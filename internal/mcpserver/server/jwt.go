package server

import (
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
	mu         sync.RWMutex
	jwksURL    string
	audience   string
	issuer     string
	publicKeys map[string]*rsa.PublicKey
	lastFetch  time.Time
	httpClient *http.Client
	ready      bool // true once JWKS has been fetched at least once
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(domain, audience string) *JWTValidator {
	return &JWTValidator{
		jwksURL:    fmt.Sprintf("https://%s/.well-known/jwks.json", domain),
		audience:   audience,
		issuer:     fmt.Sprintf("https://%s/", domain),
		publicKeys: make(map[string]*rsa.PublicKey),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Claims represents JWT claims
type Claims struct {
	jwt.RegisteredClaims
	Scope string `json:"scope,omitempty"`
}

// ValidateToken validates a JWT token and returns claims
func (v *JWTValidator) ValidateToken(tokenString string) (*Claims, error) {
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
