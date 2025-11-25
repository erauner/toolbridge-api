//go:build ignore

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// This test demonstrates the complete tenant resolution flow that Flutter clients should follow:
// 1. OIDC authentication with PKCE (standard flow, not User Management API)
// 2. Obtain ID token from token exchange
// 3. Call backend /v1/auth/tenant endpoint with ID token
// 4. Backend validates token and calls WorkOS API to resolve tenant
// 5. Return tenant_id to client for use in subsequent requests
//
// This serves as the reference implementation for Flutter/Dart client code.

const (
	// OIDC Configuration (WorkOS AuthKit)
	issuerURL   = "https://svelte-monolith-27-staging.authkit.app"
	clientID    = "client_01KAPCBQNQBWMZE9WNSEWY2J3Z"
	redirectURI = "http://localhost:3000/callback"

	// B2C Mode: No organization_id required - backend falls back to default tenant
// 	organizationID = "org_01KABXHNF45RMV9KBWF3SBGPP0"
)

// Configuration from environment
var (
	backendURL = getEnv("BACKEND_URL", "http://localhost:8080")
)

// OIDCDiscovery represents the .well-known/openid-configuration response
type OIDCDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSEndpoint         string `json:"jwks_uri"`
}

// TokenResponse represents the OAuth 2.0 token response
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// TenantResolveResponse represents the backend's tenant resolution response
type TenantResolveResponse struct {
	TenantID          string `json:"tenant_id"`
	OrganizationName  string `json:"organization_name,omitempty"`
	RequiresSelection bool   `json:"requires_selection"`
}

func main() {
	fmt.Println("=== Complete Tenant Resolution Flow Test ===")
	fmt.Println("This demonstrates the full flow that Flutter clients should implement:")
	fmt.Println("1. OIDC Authentication (PKCE)")
	fmt.Println("2. Token Exchange")
	fmt.Println("3. Tenant Resolution via Backend API")
	fmt.Println()

	// Step 1: OIDC Discovery
	fmt.Println("Step 1: Discovering OIDC endpoints...")
	discovery, err := discoverOIDC()
	if err != nil {
		fmt.Printf("❌ Error discovering OIDC: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Authorization: %s\n", discovery.AuthorizationEndpoint)
	fmt.Printf("   ✓ Token: %s\n", discovery.TokenEndpoint)
	fmt.Println()

	// Step 2: Generate PKCE parameters
	fmt.Println("Step 2: Generating PKCE parameters...")
	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)
	state := generateState()
	fmt.Printf("   ✓ Code verifier: %s...\n", codeVerifier[:20])
	fmt.Printf("   ✓ Code challenge: %s...\n", codeChallenge[:20])
	fmt.Println()

	// Step 3: Build authorization URL
	fmt.Println("Step 3: Building authorization URL...")
	authURL := buildAuthorizationURL(discovery.AuthorizationEndpoint, codeChallenge, state)
	fmt.Printf("   ✓ Authorization URL ready\n")
	fmt.Println()

	// Step 4: Perform authorization (opens browser)
	fmt.Println("Step 4: Performing authorization (browser will open)...")
	code, err := captureAuthorizationCode(authURL, state)
	if err != nil {
		fmt.Printf("❌ Error during authorization: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Authorization code: %s...\n", code[:20])
	fmt.Println()

	// Step 5: Exchange code for tokens
	fmt.Println("Step 5: Exchanging authorization code for tokens...")
	tokenResp, err := exchangeCodeForTokens(discovery.TokenEndpoint, code, codeVerifier)
	if err != nil {
		fmt.Printf("❌ Error exchanging code: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   ✓ ID token obtained: %s...\n", tokenResp.IDToken[:50])
	fmt.Printf("   ✓ Access token obtained: %s...\n", tokenResp.AccessToken[:50])
	fmt.Println()

	// Step 6: Call backend tenant resolution endpoint
	fmt.Println("Step 6: Calling backend /v1/auth/tenant endpoint...")
	fmt.Printf("   Backend URL: %s\n", backendURL)
	tenantResp, err := resolveTenant(backendURL, tokenResp.IDToken)
	if err != nil {
		fmt.Printf("❌ Error resolving tenant: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Tenant resolved successfully!\n")
	fmt.Println()

	// Step 7: Display results
	fmt.Println("=== Tenant Resolution Result ===")
	fmt.Printf("Tenant ID: %s\n", tenantResp.TenantID)
	fmt.Printf("Organization Name: %s\n", tenantResp.OrganizationName)
	fmt.Printf("Requires Selection: %v\n", tenantResp.RequiresSelection)
	fmt.Println()

	// Step 8: Summary
	fmt.Println("=== Flow Complete ===")
	fmt.Println("✅ SUCCESS - Full tenant resolution flow completed")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Println("1. ✓ OIDC authentication with PKCE completed")
	fmt.Println("2. ✓ ID token obtained from token exchange")
	fmt.Println("3. ✓ Backend validated token and queried WorkOS API")
	fmt.Printf("4. ✓ Tenant ID resolved: %s\n", tenantResp.TenantID)
	fmt.Println()
	fmt.Println("Flutter Implementation Notes:")
	fmt.Println("- Use flutter_appauth package for OIDC/PKCE flow")
	fmt.Println("- Store ID token securely (flutter_secure_storage)")
	fmt.Println("- Call GET /v1/auth/tenant with 'Bearer <id_token>'")
	fmt.Println("- Cache tenant_id for subsequent authenticated requests")
	fmt.Println("- Use tenant_id in X-Tenant-ID header for API calls")
}

// resolveTenant calls the backend tenant resolution endpoint with an ID token
func resolveTenant(baseURL, idToken string) (*TenantResolveResponse, error) {
	url := baseURL + "/v1/auth/tenant"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+idToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tenant resolution failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result TenantResolveResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// discoverOIDC fetches OIDC configuration from .well-known endpoint
func discoverOIDC() (*OIDCDiscovery, error) {
	discoveryURL := issuerURL + "/.well-known/openid-configuration"
	resp, err := http.Get(discoveryURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discovery failed with status %d", resp.StatusCode)
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, err
	}

	return &discovery, nil
}

// generateCodeVerifier generates a random code verifier for PKCE
func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// generateCodeChallenge generates the code challenge from verifier (S256)
func generateCodeChallenge(verifier string) string {
	h := sha256.New()
	h.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// generateState generates a random state parameter
func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// buildAuthorizationURL constructs the OAuth 2.0 authorization URL
func buildAuthorizationURL(authEndpoint, codeChallenge, state string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", "openid profile email")
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	// params.Set("organization_id", organizationID) // WorkOS-specific parameter

	return authEndpoint + "?" + params.Encode()
}

// exchangeCodeForTokens exchanges authorization code for tokens
func exchangeCodeForTokens(tokenEndpoint, code, codeVerifier string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequest("POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

// captureAuthorizationCode starts a local server and captures the authorization code
func captureAuthorizationCode(authURL, expectedState string) (string, error) {
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start local HTTP server to receive callback
	srv := &http.Server{Addr: ":3000"}
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if state != expectedState {
			errChan <- fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		if code == "" {
			errChan <- fmt.Errorf("no code in callback")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<html>
			<head><title>Authorization Complete</title></head>
			<body>
				<h1>✓ Authorization Complete</h1>
				<p>You can close this window and return to the terminal.</p>
			</body>
			</html>
		`))

		codeChan <- code
	})

	// Start server in background
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Open browser
	if err := openBrowser(authURL); err != nil {
		return "", fmt.Errorf("failed to open browser: %w", err)
	}

	// Wait for code or timeout
	select {
	case code := <-codeChan:
		srv.Shutdown(context.Background())
		return code, nil
	case err := <-errChan:
		srv.Shutdown(context.Background())
		return "", err
	case <-time.After(5 * time.Minute):
		srv.Shutdown(context.Background())
		return "", fmt.Errorf("timeout waiting for authorization")
	}
}

// openBrowser opens the default browser to the given URL
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
