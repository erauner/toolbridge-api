package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
	"github.com/rs/zerolog/log"
)

// DeviceDelegate implements the Delegate interface using OAuth2 Device Code Flow
type DeviceDelegate struct {
	config     config.Auth0Config
	httpClient *http.Client
	mu         sync.RWMutex

	// Cached refresh token (will use keyring in enhancement)
	refreshToken string
	idToken      string
}

// NewDeviceDelegate creates a new device code flow delegate
func NewDeviceDelegate() *DeviceDelegate {
	return &DeviceDelegate{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Configure initializes the delegate with Auth0 configuration
func (d *DeviceDelegate) Configure(cfg config.Auth0Config) error {
	if err := cfg.Validate(); err != nil {
		return ErrInvalidConfig{Field: "auth0", Reason: err.Error()}
	}

	d.mu.Lock()
	d.config = cfg
	d.mu.Unlock()

	log.Debug().
		Str("domain", cfg.Domain).
		Msg("device delegate configured")

	return nil
}

// EnsureSession establishes or validates an Auth0 session
func (d *DeviceDelegate) EnsureSession(ctx context.Context, interactive bool, defaultScopes []string) (bool, error) {
	d.mu.RLock()
	hasRefreshToken := d.refreshToken != ""
	d.mu.RUnlock()

	if hasRefreshToken {
		log.Debug().Msg("session exists (have refresh token)")
		return true, nil
	}

	if !interactive {
		log.Debug().Msg("no session and non-interactive mode")
		return false, nil
	}

	// Start device code flow to establish session
	log.Info().Msg("no existing session, starting device code flow")

	client := d.getClient()
	if client == nil {
		return false, ErrInvalidConfig{Field: "clients", Reason: "no client configured"}
	}

	scopes := strings.Join(defaultScopes, " ")
	audience := ""
	if d.config.SyncAPI != nil {
		audience = d.config.SyncAPI.Audience
	}

	token, err := d.performDeviceCodeFlow(ctx, client.ClientID, scopes, audience)
	if err != nil {
		return false, fmt.Errorf("device code flow failed: %w", err)
	}

	d.mu.Lock()
	d.refreshToken = token.RefreshToken
	d.idToken = token.AccessToken // Store ID token temporarily
	d.mu.Unlock()

	// Store refresh token in keyring
	if token.RefreshToken != "" {
		if err := StoreRefreshToken(d.config.Domain, client.ClientID, token.RefreshToken); err != nil {
			log.Warn().Err(err).Msg("failed to store refresh token in keyring (will use in-memory)")
		}
	}

	log.Info().Msg("session established successfully")
	return true, nil
}

// TryGetToken attempts to acquire an access token
func (d *DeviceDelegate) TryGetToken(ctx context.Context, audience string, scopes []string, interactive bool) (*TokenResult, error) {
	d.mu.RLock()
	refreshToken := d.refreshToken
	d.mu.RUnlock()

	client := d.getClient()
	if client == nil {
		return nil, ErrInvalidConfig{Field: "clients", Reason: "no client configured"}
	}

	// Try to load refresh token from keyring if not in memory
	if refreshToken == "" {
		stored, err := GetRefreshToken(d.config.Domain, client.ClientID)
		if err != nil {
			log.Debug().Err(err).Msg("failed to get refresh token from keyring")
		} else if stored != "" {
			refreshToken = stored
			d.mu.Lock()
			d.refreshToken = stored
			d.mu.Unlock()
			log.Debug().Msg("loaded refresh token from keyring")
		}
	}

	// If no refresh token and not interactive, cannot proceed
	if refreshToken == "" && !interactive {
		log.Debug().Msg("no refresh token and non-interactive mode")
		return nil, nil
	}

	// If no refresh token, start device code flow
	if refreshToken == "" {
		log.Info().Msg("no refresh token, starting device code flow")
		scopeStr := strings.Join(scopes, " ")
		token, err := d.performDeviceCodeFlow(ctx, client.ClientID, scopeStr, audience)
		if err != nil {
			return nil, fmt.Errorf("device code flow failed: %w", err)
		}

		d.mu.Lock()
		d.refreshToken = token.RefreshToken
		d.mu.Unlock()

		// Store refresh token in keyring
		if token.RefreshToken != "" {
			if err := StoreRefreshToken(d.config.Domain, client.ClientID, token.RefreshToken); err != nil {
				log.Warn().Err(err).Msg("failed to store refresh token in keyring")
			}
		}

		return token, nil
	}

	// Use refresh token to get new access token
	log.Debug().Msg("using refresh token to get access token")
	token, err := d.refreshAccessToken(ctx, client.ClientID, refreshToken, audience, scopes)
	if err != nil {
		log.Warn().Err(err).Msg("refresh token failed, clearing cached token")

		// Clear invalid refresh token
		d.mu.Lock()
		d.refreshToken = ""
		d.mu.Unlock()
		_ = DeleteRefreshToken(d.config.Domain, client.ClientID)

		// Try device code flow if interactive
		if interactive {
			log.Info().Msg("refresh failed, starting new device code flow")
			scopeStr := strings.Join(scopes, " ")
			return d.performDeviceCodeFlow(ctx, client.ClientID, scopeStr, audience)
		}

		return nil, fmt.Errorf("refresh token invalid and non-interactive mode: %w", err)
	}

	// Update refresh token if rotated
	if token.RefreshToken != "" && token.RefreshToken != refreshToken {
		d.mu.Lock()
		d.refreshToken = token.RefreshToken
		d.mu.Unlock()

		if err := StoreRefreshToken(d.config.Domain, client.ClientID, token.RefreshToken); err != nil {
			log.Warn().Err(err).Msg("failed to update refresh token in keyring")
		}
	}

	return token, nil
}

// TryGetIDToken attempts to get the ID token for user info
func (d *DeviceDelegate) TryGetIDToken(ctx context.Context, defaultScopes []string) (string, error) {
	d.mu.RLock()
	idToken := d.idToken
	d.mu.RUnlock()

	if idToken != "" {
		return idToken, nil
	}

	// Get a new token with openid scope
	scopes := append([]string{"openid"}, defaultScopes...)
	token, err := d.TryGetToken(ctx, "", scopes, false)
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", fmt.Errorf("no token available")
	}

	// For simplicity, return the access token (in production, you'd decode the ID token)
	return token.AccessToken, nil
}

// LogoutAll clears all sessions and tokens
func (d *DeviceDelegate) LogoutAll(ctx context.Context) error {
	d.mu.Lock()
	d.refreshToken = ""
	d.idToken = ""
	d.mu.Unlock()

	client := d.getClient()
	if client != nil {
		if err := DeleteRefreshToken(d.config.Domain, client.ClientID); err != nil {
			log.Warn().Err(err).Msg("failed to delete refresh token from keyring")
		}
	}

	log.Info().Msg("logged out successfully")
	return nil
}

// performDeviceCodeFlow executes the OAuth2 device code flow
func (d *DeviceDelegate) performDeviceCodeFlow(ctx context.Context, clientID, scopes, audience string) (*TokenResult, error) {
	// Step 1: Request device code
	deviceCode, err := d.requestDeviceCode(ctx, clientID, scopes, audience)
	if err != nil {
		return nil, fmt.Errorf("failed to request device code: %w", err)
	}

	// Step 2: Display instructions to user
	log.Info().
		Str("verification_uri", deviceCode.VerificationURI).
		Str("user_code", deviceCode.UserCode).
		Msgf("\n\n"+
			"═══════════════════════════════════════════\n"+
			"  Auth0 Device Authorization Required\n"+
			"═══════════════════════════════════════════\n"+
			"\n"+
			"Visit: %s\n"+
			"Enter code: %s\n"+
			"\n"+
			"Waiting for authorization...\n"+
			"═══════════════════════════════════════════\n",
			deviceCode.VerificationURI,
			deviceCode.UserCode)

	// Step 3: Poll for token
	token, err := d.pollForToken(ctx, clientID, deviceCode)
	if err != nil {
		return nil, fmt.Errorf("failed to poll for token: %w", err)
	}

	log.Info().Msg("device authorization successful")
	return token, nil
}

// requestDeviceCode requests a device code from Auth0
func (d *DeviceDelegate) requestDeviceCode(ctx context.Context, clientID, scopes, audience string) (*deviceCodeResponse, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth/device/code", d.config.Domain)

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("scope", scopes)
	if audience != "" {
		data.Set("audience", audience)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	log.Debug().
		Str("url", tokenURL).
		Str("client_id", clientID).
		Str("scopes", scopes).
		Msg("requesting device code")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var deviceCode deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return nil, fmt.Errorf("failed to decode device code response: %w", err)
	}

	return &deviceCode, nil
}

// pollForToken polls Auth0 for the access token after user authorization
func (d *DeviceDelegate) pollForToken(ctx context.Context, clientID string, deviceCode *deviceCodeResponse) (*TokenResult, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth/token", d.config.Domain)
	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}

	timeout := time.Duration(deviceCode.ExpiresIn) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, ErrTimeout{Duration: timeout.String()}
			}

			token, err := d.attemptTokenExchange(ctx, tokenURL, clientID, deviceCode.DeviceCode)
			if err != nil {
				// Check for specific error codes
				if strings.Contains(err.Error(), "authorization_pending") {
					log.Debug().Msg("authorization pending, continuing to poll")
					continue
				}
				if strings.Contains(err.Error(), "slow_down") {
					log.Debug().Msg("polling too fast, slowing down")
					ticker.Reset(interval + 5*time.Second)
					continue
				}
				if strings.Contains(err.Error(), "access_denied") {
					return nil, ErrUserDenied{Description: "user denied authorization"}
				}
				if strings.Contains(err.Error(), "expired_token") {
					return nil, ErrTimeout{Duration: timeout.String()}
				}

				return nil, err
			}

			return token, nil
		}
	}
}

// attemptTokenExchange attempts to exchange device code for access token
func (d *DeviceDelegate) attemptTokenExchange(ctx context.Context, tokenURL, clientID, deviceCode string) (*TokenResult, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("client_id", clientID)
	data.Set("device_code", deviceCode)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("%s: %s", errorResp.Error, errorResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &TokenResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		TokenType:    tokenResp.TokenType,
	}, nil
}

// refreshAccessToken uses a refresh token to get a new access token
func (d *DeviceDelegate) refreshAccessToken(ctx context.Context, clientID, refreshToken, audience string, scopes []string) (*TokenResult, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth/token", d.config.Domain)

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	data.Set("refresh_token", refreshToken)
	if audience != "" {
		data.Set("audience", audience)
	}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	log.Debug().Msg("refreshing access token")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("%s: %s", errorResp.Error, errorResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	log.Debug().
		Time("expiresAt", expiresAt).
		Msg("access token refreshed successfully")

	return &TokenResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		TokenType:    tokenResp.TokenType,
	}, nil
}

// getClient returns the first configured client (priority: native, web, macos)
func (d *DeviceDelegate) getClient() *config.ClientConfig {
	if native := d.config.GetNativeClient(); native != nil {
		return native
	}
	if web := d.config.GetWebClient(); web != nil {
		return web
	}
	if macos := d.config.GetMacOSClient(); macos != nil {
		return macos
	}
	return nil
}

// deviceCodeResponse represents Auth0's device code endpoint response
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}
