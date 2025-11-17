package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

// TokenIntrospector performs OAuth 2.0 token introspection per RFC 7662
// Used as fallback when JWT parsing fails (e.g., for opaque Auth0 tokens)
type TokenIntrospector struct {
	endpoint     string // https://<domain>/oauth/token/introspect
	clientID     string
	clientSecret string
	audience     string // Expected audience for validation
	issuer       string // Expected issuer for validation
	httpClient   *http.Client
}

// IntrospectionResponse represents the OAuth 2.0 introspection response per RFC 7662
type IntrospectionResponse struct {
	Active bool   `json:"active"` // REQUIRED - whether token is active
	Sub    string `json:"sub,omitempty"`
	Exp    int64  `json:"exp,omitempty"` // Unix timestamp
	Iat    int64  `json:"iat,omitempty"`
	Aud    any    `json:"aud,omitempty"` // Can be string or array of strings
	Scope  string `json:"scope,omitempty"`
	Iss    string `json:"iss,omitempty"`
}

// NewTokenIntrospector creates a new token introspector
func NewTokenIntrospector(domain, clientID, clientSecret, audience, issuer string) *TokenIntrospector {
	endpoint := fmt.Sprintf("https://%s/oauth/token/introspect", domain)

	log.Info().
		Str("endpoint", endpoint).
		Str("clientId", clientID).
		Str("audience", audience).
		Str("authMethod", "client_secret_post").
		Msg("Token introspector initialized (credentials will be sent in form body)")

	return &TokenIntrospector{
		endpoint:     endpoint,
		clientID:     clientID,
		clientSecret: clientSecret,
		audience:     audience,
		issuer:       issuer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Introspect performs token introspection and returns claims
// Returns Claims struct compatible with JWT validation path
func (ti *TokenIntrospector) Introspect(ctx context.Context, token string) (*Claims, error) {
	// Prepare form data per RFC 7662
	formData := url.Values{}
	formData.Set("token", token)
	formData.Set("token_type_hint", "access_token")
	if ti.audience != "" {
		formData.Set("audience", ti.audience)
	}

	// Auth0 requires client_secret_post authentication method
	// (credentials in form body, not Basic Auth header)
	// This matches the client's configured authentication_method in Auth0
	formData.Set("client_id", ti.clientID)
	formData.Set("client_secret", ti.clientSecret)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", ti.endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create introspection request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	log.Debug().
		Str("endpoint", ti.endpoint).
		Str("audience", ti.audience).
		Msg("Performing token introspection")

	// Perform request
	resp, err := ti.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("introspection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspection request failed with status %d", resp.StatusCode)
	}

	// Parse introspection response
	var introspection IntrospectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&introspection); err != nil {
		return nil, fmt.Errorf("failed to decode introspection response: %w", err)
	}

	// Check if token is active
	if !introspection.Active {
		return nil, fmt.Errorf("token is not active")
	}

	// Validate required claims
	if introspection.Sub == "" {
		return nil, fmt.Errorf("introspection response missing 'sub' claim")
	}

	// Convert to Claims struct
	claims := &Claims{}
	claims.Subject = introspection.Sub
	claims.Scope = introspection.Scope

	// Set expiration
	if introspection.Exp > 0 {
		claims.ExpiresAt = &jwt.NumericDate{Time: time.Unix(introspection.Exp, 0)}
	} else {
		// If no expiry provided, treat as short-lived (5 minutes from now)
		log.Warn().Msg("Introspection response missing 'exp' claim, treating as short-lived token")
		claims.ExpiresAt = &jwt.NumericDate{Time: time.Now().Add(5 * time.Minute)}
	}

	// Set issued at
	if introspection.Iat > 0 {
		claims.IssuedAt = &jwt.NumericDate{Time: time.Unix(introspection.Iat, 0)}
	}

	// Set issuer
	if introspection.Iss != "" {
		claims.Issuer = introspection.Iss
	}

	// Normalize audience (can be string or array)
	audienceStr, err := normalizeAudience(introspection.Aud, ti.audience)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize audience: %w", err)
	}
	if audienceStr != "" {
		claims.Audience = []string{audienceStr}
	}

	// SECURITY: Validate issuer matches expected value
	if claims.Issuer != ti.issuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", ti.issuer, claims.Issuer)
	}

	// SECURITY: Validate audience matches expected value
	validAudience := false
	for _, aud := range claims.Audience {
		if aud == ti.audience {
			validAudience = true
			break
		}
	}
	if !validAudience {
		return nil, fmt.Errorf("invalid audience: expected %s, got %v", ti.audience, claims.Audience)
	}

	log.Debug().
		Str("sub", claims.Subject).
		Str("audience", audienceStr).
		Time("exp", claims.ExpiresAt.Time).
		Msg("Token introspection successful")

	return claims, nil
}

// normalizeAudience converts audience (string or array) to a single string
// Prefers the configured audience if present in array, otherwise returns first element
func normalizeAudience(aud any, preferredAudience string) (string, error) {
	if aud == nil {
		return "", nil
	}

	switch v := aud.(type) {
	case string:
		return v, nil
	case []interface{}:
		if len(v) == 0 {
			return "", nil
		}
		// If preferred audience is set and found in array, use it
		if preferredAudience != "" {
			for _, a := range v {
				if str, ok := a.(string); ok && str == preferredAudience {
					return str, nil
				}
			}
		}
		// Otherwise return first element
		if str, ok := v[0].(string); ok {
			return str, nil
		}
		return "", fmt.Errorf("audience array contains non-string element")
	default:
		return "", fmt.Errorf("unexpected audience type: %T", aud)
	}
}
