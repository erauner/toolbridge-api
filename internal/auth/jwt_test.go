package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Mock JWKS server for testing RS256 validation
type mockJWKSServer struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	kid        string
}

func newMockJWKSServer() (*mockJWKSServer, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	return &mockJWKSServer{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
		kid:        "test-key-id",
	}, nil
}

func (m *mockJWKSServer) issueToken(claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = m.kid
	return token.SignedString(m.privateKey)
}

// TestValidateToken_WorkOS_DCR_SkipsAudienceValidation tests the core DCR fix:
// When MCP_OAUTH_AUDIENCE is empty but issuer is configured, audience validation
// should be skipped for WorkOS AuthKit DCR tokens.
func TestValidateToken_WorkOS_DCR_SkipsAudienceValidation(t *testing.T) {
	server, err := newMockJWKSServer()
	if err != nil {
		t.Fatalf("Failed to create mock JWKS server: %v", err)
	}

	// Simulate WorkOS AuthKit DCR configuration:
	// - Issuer is set (WorkOS AuthKit domain)
	// - No AcceptedAudiences (MCP_OAUTH_AUDIENCE is empty)
	// - Token has client ID as audience (unpredictable due to DCR)
	cfg := JWTCfg{
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		AcceptedAudiences: []string{}, // Empty = skip audience validation for DCR
	}

	// Initialize global cache with mock server's public key
	globalJWKSCache = &jwksCache{
		keys: map[string]*rsa.PublicKey{
			server.kid: server.publicKey,
		},
		lastFetch: time.Now(),
		cacheTTL:  1 * time.Hour,
	}

	// WorkOS DCR token with dynamically-created client ID as audience
	claims := jwt.MapClaims{
		"sub": "user_01KAHS4J1W6TT5390SR3918ZPF",
		"iss": "https://svelte-monolith-27-staging.authkit.app",
		"aud": "client_01KABXHNQ09QGWEX4APPYG2AH5", // Client ID, not resource URL
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := server.issueToken(claims)
	if err != nil {
		t.Fatalf("Failed to issue token: %v", err)
	}

	// CRITICAL: This should PASS even though audience is client ID
	// because AcceptedAudiences is empty (DCR mode)
	sub, _, err := ValidateToken(tokenString, cfg)
	if err != nil {
		t.Fatalf("Expected token to be accepted in DCR mode, got error: %v", err)
	}

	if sub != "user_01KAHS4J1W6TT5390SR3918ZPF" {
		t.Errorf("Expected sub=%s, got %s", "user_01KAHS4J1W6TT5390SR3918ZPF", sub)
	}
}

// TestValidateToken_WorkOS_DCR_StillValidatesIssuer ensures that even when
// audience validation is skipped, issuer validation still occurs.
func TestValidateToken_WorkOS_DCR_StillValidatesIssuer(t *testing.T) {
	server, err := newMockJWKSServer()
	if err != nil {
		t.Fatalf("Failed to create mock JWKS server: %v", err)
	}

	cfg := JWTCfg{
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		AcceptedAudiences: []string{}, // DCR mode
	}

	globalJWKSCache = &jwksCache{
		keys: map[string]*rsa.PublicKey{
			server.kid: server.publicKey,
		},
		lastFetch: time.Now(),
		cacheTTL:  1 * time.Hour,
	}

	// Token with WRONG issuer
	claims := jwt.MapClaims{
		"sub": "user_123",
		"iss": "https://evil-attacker.com", // Wrong issuer
		"aud": "client_01KABXHNQ09QGWEX4APPYG2AH5",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := server.issueToken(claims)
	if err != nil {
		t.Fatalf("Failed to issue token: %v", err)
	}

	// Should FAIL due to issuer mismatch
	_, _, err = ValidateToken(tokenString, cfg)
	if err == nil {
		t.Fatal("Expected token to be rejected due to invalid issuer")
	}
	if !contains(err.Error(), "invalid issuer") {
		t.Errorf("Expected 'invalid issuer' error, got: %v", err)
	}
}

// TestValidateToken_JWTAudienceSet_StillValidates is a regression test for a security issue.
// When JWT_AUDIENCE is set (for direct API tokens) but MCP_OAUTH_AUDIENCE is empty,
// we must still validate audience. Only skip validation when BOTH are empty (pure DCR mode).
func TestValidateToken_JWTAudienceSet_StillValidates(t *testing.T) {
	server, err := newMockJWKSServer()
	if err != nil {
		t.Fatalf("Failed to create mock JWKS server: %v", err)
	}

	// Configuration with JWT_AUDIENCE set but AcceptedAudiences empty
	// This is the common case for deployments with direct API access + MCP
	cfg := JWTCfg{
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		Audience:          "https://toolbridgeapi.erauner.dev", // Set for direct API
		AcceptedAudiences: []string{},                          // Empty (no MCP audience)
	}

	globalJWKSCache = &jwksCache{
		keys: map[string]*rsa.PublicKey{
			server.kid: server.publicKey,
		},
		lastFetch: time.Now(),
		cacheTTL:  1 * time.Hour,
	}

	// Token with WRONG audience (not the configured JWT_AUDIENCE)
	claims := jwt.MapClaims{
		"sub": "user_123",
		"iss": "https://svelte-monolith-27-staging.authkit.app",
		"aud": "https://attacker.com", // Wrong audience
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := server.issueToken(claims)
	if err != nil {
		t.Fatalf("Failed to issue token: %v", err)
	}

	// CRITICAL: Should REJECT even though AcceptedAudiences is empty
	// because cfg.Audience is set
	_, _, err = ValidateToken(tokenString, cfg)
	if err == nil {
		t.Fatal("SECURITY ISSUE: Token with wrong audience accepted when JWT_AUDIENCE is set!")
	}
	if !contains(err.Error(), "invalid audience") {
		t.Errorf("Expected 'invalid audience' error, got: %v", err)
	}

	// Now test with CORRECT audience - should pass
	claims["aud"] = "https://toolbridgeapi.erauner.dev"
	tokenString, err = server.issueToken(claims)
	if err != nil {
		t.Fatalf("Failed to issue token: %v", err)
	}

	sub, _, err := ValidateToken(tokenString, cfg)
	if err != nil {
		t.Fatalf("Token with correct audience should be accepted: %v", err)
	}
	if sub != "user_123" {
		t.Errorf("Expected sub=user_123, got %s", sub)
	}
}

// TestValidateToken_RegularTokens_StillValidateAudience ensures that when
// AcceptedAudiences IS configured, audience validation still happens.
func TestValidateToken_RegularTokens_StillValidateAudience(t *testing.T) {
	server, err := newMockJWKSServer()
	if err != nil {
		t.Fatalf("Failed to create mock JWKS server: %v", err)
	}

	// Regular (non-DCR) configuration with audience validation
	cfg := JWTCfg{
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		Audience:          "https://toolbridgeapi.erauner.dev",
		AcceptedAudiences: []string{"https://toolbridge-mcp-staging.fly.dev/mcp"},
	}

	globalJWKSCache = &jwksCache{
		keys: map[string]*rsa.PublicKey{
			server.kid: server.publicKey,
		},
		lastFetch: time.Now(),
		cacheTTL:  1 * time.Hour,
	}

	tests := []struct {
		name        string
		audience    interface{}
		shouldPass  bool
		description string
	}{
		{
			name:        "valid primary audience",
			audience:    "https://toolbridgeapi.erauner.dev",
			shouldPass:  true,
			description: "Token with primary audience should pass",
		},
		{
			name:        "valid additional audience",
			audience:    "https://toolbridge-mcp-staging.fly.dev/mcp",
			shouldPass:  true,
			description: "Token with additional accepted audience should pass",
		},
		{
			name:        "invalid audience",
			audience:    "https://attacker.com",
			shouldPass:  false,
			description: "Token with non-accepted audience should fail",
		},
		{
			name:        "multiple audiences including valid",
			audience:    []interface{}{"https://toolbridgeapi.erauner.dev", "https://other.com"},
			shouldPass:  true,
			description: "Token with array of audiences including valid one should pass",
		},
		{
			name:        "multiple audiences all invalid",
			audience:    []interface{}{"https://attacker.com", "https://evil.com"},
			shouldPass:  false,
			description: "Token with only invalid audiences should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := jwt.MapClaims{
				"sub": "user_123",
				"iss": "https://svelte-monolith-27-staging.authkit.app",
				"aud": tt.audience,
				"exp": time.Now().Add(1 * time.Hour).Unix(),
				"iat": time.Now().Unix(),
			}

			tokenString, err := server.issueToken(claims)
			if err != nil {
				t.Fatalf("Failed to issue token: %v", err)
			}

			sub, _, err := ValidateToken(tokenString, cfg)

			if tt.shouldPass {
				if err != nil {
					t.Errorf("%s: Expected token to pass, got error: %v", tt.description, err)
				}
				if sub != "user_123" {
					t.Errorf("Expected sub=user_123, got %s", sub)
				}
			} else {
				if err == nil {
					t.Errorf("%s: Expected token to fail, but it passed", tt.description)
				}
				if !contains(err.Error(), "invalid audience") {
					t.Errorf("Expected 'invalid audience' error, got: %v", err)
				}
			}
		})
	}
}

// TestValidateToken_BackendToken_SkipsIssuerAndAudience tests that backend
// tokens with token_type="backend" skip external IdP validation.
func TestValidateToken_BackendToken_SkipsIssuerAndAudience(t *testing.T) {
	// Backend tokens use HS256, not RS256
	secret := "test-hmac-secret"

	cfg := JWTCfg{
		HS256Secret:       secret,
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		Audience:          "https://toolbridgeapi.erauner.dev",
		AcceptedAudiences: []string{},
	}

	// Backend token with token_type="backend"
	claims := jwt.MapClaims{
		"sub":        "user_123",
		"iss":        "toolbridge-api", // Backend issuer, not IdP
		"aud":        "internal",        // Internal audience, not IdP audience
		"token_type": "backend",
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	// Should PASS even though issuer and audience don't match IdP config
	sub, _, err := ValidateToken(tokenString, cfg)
	if err != nil {
		t.Fatalf("Expected backend token to pass, got error: %v", err)
	}

	if sub != "user_123" {
		t.Errorf("Expected sub=user_123, got %s", sub)
	}
}

// TestValidateToken_LegacyBackendToken tests backward compatibility with
// old backend tokens that have iss="toolbridge-api" but no token_type claim.
func TestValidateToken_LegacyBackendToken(t *testing.T) {
	secret := "test-hmac-secret"

	cfg := JWTCfg{
		HS256Secret:       secret,
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		Audience:          "https://toolbridgeapi.erauner.dev",
		AcceptedAudiences: []string{},
	}

	// Legacy backend token (no token_type, but iss="toolbridge-api")
	claims := jwt.MapClaims{
		"sub": "user_123",
		"iss": "toolbridge-api", // Legacy backend issuer
		"aud": "internal",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		// No token_type claim
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	// Should PASS for backward compatibility
	sub, _, err := ValidateToken(tokenString, cfg)
	if err != nil {
		t.Fatalf("Expected legacy backend token to pass, got error: %v", err)
	}

	if sub != "user_123" {
		t.Errorf("Expected sub=user_123, got %s", sub)
	}
}

// TestValidateToken_ExpiredToken ensures expired tokens are rejected.
func TestValidateToken_ExpiredToken(t *testing.T) {
	server, err := newMockJWKSServer()
	if err != nil {
		t.Fatalf("Failed to create mock JWKS server: %v", err)
	}

	cfg := JWTCfg{
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		AcceptedAudiences: []string{},
	}

	globalJWKSCache = &jwksCache{
		keys: map[string]*rsa.PublicKey{
			server.kid: server.publicKey,
		},
		lastFetch: time.Now(),
		cacheTTL:  1 * time.Hour,
	}

	// Expired token
	claims := jwt.MapClaims{
		"sub": "user_123",
		"iss": "https://svelte-monolith-27-staging.authkit.app",
		"aud": "client_01KABXHNQ09QGWEX4APPYG2AH5",
		"exp": time.Now().Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	}

	tokenString, err := server.issueToken(claims)
	if err != nil {
		t.Fatalf("Failed to issue token: %v", err)
	}

	// Should FAIL due to expiration
	_, _, err = ValidateToken(tokenString, cfg)
	if err == nil {
		t.Fatal("Expected expired token to be rejected")
	}
}

// TestValidateToken_MissingSubClaim ensures tokens without sub claim are rejected.
func TestValidateToken_MissingSubClaim(t *testing.T) {
	server, err := newMockJWKSServer()
	if err != nil {
		t.Fatalf("Failed to create mock JWKS server: %v", err)
	}

	cfg := JWTCfg{
		Issuer:            "https://svelte-monolith-27-staging.authkit.app",
		AcceptedAudiences: []string{},
	}

	globalJWKSCache = &jwksCache{
		keys: map[string]*rsa.PublicKey{
			server.kid: server.publicKey,
		},
		lastFetch: time.Now(),
		cacheTTL:  1 * time.Hour,
	}

	// Token without sub claim
	claims := jwt.MapClaims{
		// No "sub" claim
		"iss": "https://svelte-monolith-27-staging.authkit.app",
		"aud": "client_01KABXHNQ09QGWEX4APPYG2AH5",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := server.issueToken(claims)
	if err != nil {
		t.Fatalf("Failed to issue token: %v", err)
	}

	// Should FAIL due to missing sub claim
	_, _, err = ValidateToken(tokenString, cfg)
	if err == nil {
		t.Fatal("Expected token without sub claim to be rejected")
	}
	// Error message is "missing or invalid sub claim" - just check it's not nil
	if err.Error() == "" {
		t.Errorf("Expected non-empty error message, got: %v", err)
	}
}

// Note: contains() helper function is defined in tenant_headers_test.go
