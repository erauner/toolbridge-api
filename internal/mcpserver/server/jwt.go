package server

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

// JWTValidator validates Auth0 JWT tokens
type JWTValidator struct {
	mu            sync.RWMutex
	jwksURL       string
	audience      string
	issuer        string
	publicKeys    map[string]*rsa.PublicKey
	lastFetch     time.Time
	httpClient    *http.Client
	ready         bool // true once JWKS has been fetched at least once
	stopRetry     chan struct{}
	retryDone     chan struct{}
	retryRunning  bool // true if background retry goroutine is running
	introspector  *TokenIntrospector // Optional fallback for opaque tokens
}

// NewJWTValidator creates a new JWT validator
// If introspectionConfig is provided, enables fallback to token introspection for opaque tokens
func NewJWTValidator(domain, audience string, introspectionConfig ...*IntrospectionConfig) *JWTValidator {
	v := &JWTValidator{
		jwksURL:    fmt.Sprintf("https://%s/.well-known/jwks.json", domain),
		audience:   audience,
		issuer:     fmt.Sprintf("https://%s/", domain),
		publicKeys: make(map[string]*rsa.PublicKey),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		stopRetry:  make(chan struct{}),
		retryDone:  make(chan struct{}),
	}

	// Add introspector if config provided
	if len(introspectionConfig) > 0 && introspectionConfig[0] != nil {
		cfg := introspectionConfig[0]
		// Use configured audience or fallback to validator's audience
		introspectionAudience := cfg.Audience
		if introspectionAudience == "" {
			introspectionAudience = audience
		}
		v.introspector = NewTokenIntrospector(
			domain,
			cfg.ClientID,
			cfg.ClientSecret,
			introspectionAudience,
		)
		log.Info().
			Str("endpoint", v.introspector.endpoint).
			Bool("introspectionEnabled", true).
			Msg("Token introspection fallback enabled")
	}

	return v
}

// IntrospectionConfig holds introspection client credentials
// Imported from config package to avoid circular dependency
type IntrospectionConfig struct {
	ClientID     string
	ClientSecret string
	Audience     string
}

// Claims represents JWT claims
type Claims struct {
	jwt.RegisteredClaims
	Scope string `json:"scope,omitempty"`
}

// ValidateToken validates a JWT token and returns claims
// Falls back to token introspection if JWT parsing fails and introspector is configured
func (v *JWTValidator) ValidateToken(tokenString string) (*Claims, bool, error) {
	// Attempt JWT validation first (happy path for RS256 tokens)
	claims, jwtErr := v.validateJWT(tokenString)
	if jwtErr == nil {
		return claims, false, nil // Success via JWT path
	}

	// If introspector is not configured, return JWT error
	if v.introspector == nil {
		return nil, false, jwtErr
	}

	// Log JWT failure at debug level before trying introspection
	log.Debug().
		Err(jwtErr).
		Msg("JWT validation failed, attempting token introspection fallback")

	// Attempt introspection fallback
	ctx := context.Background()
	claims, introspectErr := v.introspector.Introspect(ctx, tokenString)
	if introspectErr != nil {
		// Both paths failed - log warning with both errors
		log.Warn().
			Err(jwtErr).
			AnErr("introspectionError", introspectErr).
			Msg("Both JWT validation and introspection failed")
		return nil, false, fmt.Errorf("token validation failed: jwt_error=%w, introspection_error=%v", jwtErr, introspectErr)
	}

	// Introspection succeeded
	log.Info().
		Str("sub", claims.Subject).
		Msg("Token validated successfully via introspection fallback")
	return claims, true, nil // Success via introspection path
}

// validateJWT performs standard JWT validation (internal helper)
func (v *JWTValidator) validateJWT(tokenString string) (*Claims, error) {
	// Parse token without validation first to get key ID
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Get key ID from header
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("missing kid in token header")
	}

	// Get public key for kid
	publicKey, err := v.getPublicKey(kid)
	if err != nil {
		return nil, err
	}

	// Validate token with public key
	var claims Claims
	parsedToken, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if !parsedToken.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Validate issuer
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("invalid issuer: %s", claims.Issuer)
	}

	// Validate audience (can be string or array)
	validAudience := false
	audiences, err := claims.GetAudience()
	if err != nil {
		return nil, fmt.Errorf("invalid audience format: %w", err)
	}

	for _, aud := range audiences {
		if aud == v.audience {
			validAudience = true
			break
		}
	}

	if !validAudience {
		return nil, fmt.Errorf("invalid audience: expected %s, got %v", v.audience, audiences)
	}

	return &claims, nil
}

// getPublicKey fetches or returns cached public key
func (v *JWTValidator) getPublicKey(kid string) (*rsa.PublicKey, error) {
	// Check cache first
	v.mu.RLock()
	key, exists := v.publicKeys[kid]
	lastFetch := v.lastFetch
	v.mu.RUnlock()

	// Return cached key if fresh (< 1 hour old)
	if exists && time.Since(lastFetch) < 1*time.Hour {
		return key, nil
	}

	// Fetch JWKS
	return v.fetchPublicKey(kid)
}

// fetchPublicKey fetches public keys from Auth0 JWKS endpoint
func (v *JWTValidator) fetchPublicKey(kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check (another goroutine may have fetched)
	if key, exists := v.publicKeys[kid]; exists && time.Since(v.lastFetch) < 1*time.Minute {
		return key, nil
	}

	log.Debug().Str("jwksURL", v.jwksURL).Msg("Fetching JWKS")

	// Fetch JWKS
	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS request failed with status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	log.Debug().Int("keyCount", len(jwks.Keys)).Msg("Received JWKS")

	// Parse all keys and cache
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" || key.Use != "sig" {
			continue
		}

		publicKey, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			log.Warn().
				Err(err).
				Str("kid", key.Kid).
				Msg("Failed to parse RSA public key")
			continue
		}

		v.publicKeys[key.Kid] = publicKey
		log.Debug().
			Str("kid", key.Kid).
			Msg("Cached RSA public key")
	}

	v.lastFetch = time.Now()
	v.ready = true // Mark as ready after first successful fetch

	// Return requested key
	if key, exists := v.publicKeys[kid]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("key ID %s not found in JWKS", kid)
}

// Ready returns true if the validator has successfully fetched JWKS at least once
func (v *JWTValidator) Ready() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.ready
}

// StartBackgroundRetry starts a background goroutine that retries JWKS fetches
// until successful. This ensures readiness eventually becomes true even if
// Auth0 is temporarily unreachable during startup.
// This method is idempotent - calling it multiple times will not start multiple goroutines,
// and it's safe to call again after a previous retry cycle has completed.
func (v *JWTValidator) StartBackgroundRetry() {
	v.mu.Lock()
	if v.retryRunning {
		// Already running, don't start another goroutine
		v.mu.Unlock()
		log.Debug().Msg("Background retry already running, skipping duplicate start")
		return
	}

	// Recreate channels for this retry cycle (safe even if previous cycle closed them)
	v.stopRetry = make(chan struct{})
	v.retryDone = make(chan struct{})
	v.retryRunning = true
	v.mu.Unlock()

	log.Info().Msg("Starting background JWKS retry (will retry every 5-60s until successful)")

	go func() {
		defer func() {
			v.mu.Lock()
			v.retryRunning = false
			v.mu.Unlock()
			close(v.retryDone)
			log.Info().Msg("Background JWKS retry stopped")
		}()

		retryInterval := 5 * time.Second
		maxRetryInterval := 60 * time.Second

		for {
			// Check if already ready
			if v.Ready() {
				log.Info().Msg("JWT validator ready, stopping background retry")
				return
			}

			// Try to fetch JWKS
			log.Debug().Msg("Background retry: attempting to fetch JWKS")
			err := v.WarmUp()
			if err == nil {
				log.Info().Msg("Background retry succeeded, JWT validator now ready")
				return
			}

			log.Warn().Err(err).Dur("retryIn", retryInterval).Msg("Background retry failed, will retry")

			// Wait before retry or stop
			select {
			case <-time.After(retryInterval):
				// Exponential backoff (max 60s)
				retryInterval *= 2
				if retryInterval > maxRetryInterval {
					retryInterval = maxRetryInterval
				}
			case <-v.stopRetry:
				log.Info().Msg("Background retry received stop signal")
				return
			}
		}
	}()
}

// StopBackgroundRetry stops the background retry goroutine
// Only waits if the retry goroutine is actually running to avoid deadlock
func (v *JWTValidator) StopBackgroundRetry() {
	v.mu.RLock()
	isRunning := v.retryRunning
	v.mu.RUnlock()

	if !isRunning {
		// Retry was never started (warmup succeeded on first try)
		return
	}

	close(v.stopRetry)
	<-v.retryDone // Wait for goroutine to finish
}

// WarmUp pre-fetches JWKS to make the validator ready
// This is optional but recommended during startup to avoid readiness delays
func (v *JWTValidator) WarmUp() error {
	log.Debug().Msg("Warming up JWT validator (fetching JWKS)")

	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS during warmup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS warmup request failed with status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("failed to decode JWKS during warmup: %w", err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Parse all keys and cache
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" || key.Use != "sig" {
			continue
		}

		publicKey, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			log.Warn().
				Err(err).
				Str("kid", key.Kid).
				Msg("Failed to parse RSA public key during warmup")
			continue
		}

		v.publicKeys[key.Kid] = publicKey
		log.Debug().
			Str("kid", key.Kid).
			Msg("Cached RSA public key during warmup")
	}

	v.lastFetch = time.Now()
	v.ready = true

	log.Info().Int("keyCount", len(v.publicKeys)).Msg("JWT validator warmed up successfully")
	return nil
}

// parseRSAPublicKey parses RSA public key from JWKS n and e values
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	// Decode base64url-encoded n (modulus)
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode n: %w", err)
	}

	// Decode base64url-encoded e (exponent)
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode e: %w", err)
	}

	// Convert bytes to big.Int
	n := new(big.Int).SetBytes(nBytes)

	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}
