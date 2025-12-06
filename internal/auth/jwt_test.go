package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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

// =============================================================================
// RS256 Backend Signer Tests
// =============================================================================

// TestInitBackendSigner_PKCS8 tests initialization with PKCS#8 format private key
func TestInitBackendSigner_PKCS8(t *testing.T) {
	// Reset global state
	backendSigner = nil

	// Generate a test key and encode as PKCS#8
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	pkcs8Bytes, err := marshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to marshal PKCS#8: %v", err)
	}

	pemBlock := pemEncode(pkcs8Bytes, "PRIVATE KEY")

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: pemBlock,
		BackendKeyID:            "test-backend-key-1",
	}

	err = InitBackendSigner(cfg)
	if err != nil {
		t.Fatalf("InitBackendSigner failed: %v", err)
	}

	if backendSigner == nil {
		t.Fatal("Expected backendSigner to be initialized")
	}
	if backendSigner.KeyID != "test-backend-key-1" {
		t.Errorf("Expected KeyID=%s, got %s", "test-backend-key-1", backendSigner.KeyID)
	}
	if backendSigner.PrivateKey == nil {
		t.Error("Expected PrivateKey to be set")
	}
	if backendSigner.PublicKey == nil {
		t.Error("Expected PublicKey to be set")
	}

	// Cleanup
	backendSigner = nil
}

// TestInitBackendSigner_PKCS1 tests initialization with PKCS#1 format private key
func TestInitBackendSigner_PKCS1(t *testing.T) {
	// Reset global state
	backendSigner = nil

	// Generate a test key and encode as PKCS#1
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	pkcs1Bytes := marshalPKCS1PrivateKey(privateKey)
	pemBlock := pemEncode(pkcs1Bytes, "RSA PRIVATE KEY")

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: pemBlock,
		BackendKeyID:            "test-backend-key-2",
	}

	err = InitBackendSigner(cfg)
	if err != nil {
		t.Fatalf("InitBackendSigner failed: %v", err)
	}

	if backendSigner == nil {
		t.Fatal("Expected backendSigner to be initialized")
	}
	if backendSigner.KeyID != "test-backend-key-2" {
		t.Errorf("Expected KeyID=%s, got %s", "test-backend-key-2", backendSigner.KeyID)
	}

	// Cleanup
	backendSigner = nil
}

// TestInitBackendSigner_MissingKeyID tests that missing key ID returns error
func TestInitBackendSigner_MissingKeyID(t *testing.T) {
	backendSigner = nil

	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	pkcs1Bytes := marshalPKCS1PrivateKey(privateKey)
	pemBlock := pemEncode(pkcs1Bytes, "RSA PRIVATE KEY")

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: pemBlock,
		BackendKeyID:            "", // Missing!
	}

	err := InitBackendSigner(cfg)
	if err == nil {
		t.Fatal("Expected error when BackendKeyID is missing")
	}
	if !contains(err.Error(), "BackendKeyID must be set") {
		t.Errorf("Expected 'BackendKeyID must be set' error, got: %v", err)
	}

	backendSigner = nil
}

// TestInitBackendSigner_InvalidPEM tests that invalid PEM returns error
func TestInitBackendSigner_InvalidPEM(t *testing.T) {
	backendSigner = nil

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "not-valid-pem-data",
		BackendKeyID:            "test-key",
	}

	err := InitBackendSigner(cfg)
	if err == nil {
		t.Fatal("Expected error for invalid PEM")
	}
	if !contains(err.Error(), "failed to decode PEM") {
		t.Errorf("Expected 'failed to decode PEM' error, got: %v", err)
	}

	backendSigner = nil
}

// TestInitBackendSigner_EmptyConfig tests that empty config is a no-op
func TestInitBackendSigner_EmptyConfig(t *testing.T) {
	backendSigner = nil

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "",
		BackendKeyID:            "",
	}

	err := InitBackendSigner(cfg)
	if err != nil {
		t.Fatalf("Expected no error for empty config, got: %v", err)
	}
	if backendSigner != nil {
		t.Error("Expected backendSigner to remain nil for empty config")
	}
}

// TestSignBackendToken_RS256 tests signing with RS256 when configured
func TestSignBackendToken_RS256(t *testing.T) {
	backendSigner = nil

	// Setup backend signer
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	backendSigner = &BackendSigner{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
		KeyID:      "rs256-test-key",
	}

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "dummy-pem", // Just needs to be non-empty
		BackendKeyID:            "rs256-test-key",
		HS256Secret:             "fallback-secret",
	}

	claims := jwt.MapClaims{
		"sub":        "user_123",
		"iss":        "toolbridge-api",
		"token_type": "backend",
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
	}

	tokenString, err := SignBackendToken(claims, cfg)
	if err != nil {
		t.Fatalf("SignBackendToken failed: %v", err)
	}

	// Parse token to verify it's RS256 with correct kid
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return backendSigner.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("Failed to parse token: %v", err)
	}
	if !token.Valid {
		t.Error("Token should be valid")
	}

	// Check kid header
	kid, ok := token.Header["kid"].(string)
	if !ok || kid != "rs256-test-key" {
		t.Errorf("Expected kid=%s, got %v", "rs256-test-key", token.Header["kid"])
	}

	// Check algorithm
	if token.Header["alg"] != "RS256" {
		t.Errorf("Expected alg=RS256, got %v", token.Header["alg"])
	}

	backendSigner = nil
}

// TestSignBackendToken_HS256Fallback tests HS256 fallback when RS256 not configured
func TestSignBackendToken_HS256Fallback(t *testing.T) {
	backendSigner = nil // No RS256 signer

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "", // Not configured
		BackendKeyID:            "",
		HS256Secret:             "test-hs256-secret",
	}

	claims := jwt.MapClaims{
		"sub":        "user_456",
		"iss":        "toolbridge-api",
		"token_type": "backend",
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
	}

	tokenString, err := SignBackendToken(claims, cfg)
	if err != nil {
		t.Fatalf("SignBackendToken failed: %v", err)
	}

	// Parse token to verify it's HS256
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte("test-hs256-secret"), nil
	})
	if err != nil {
		t.Fatalf("Failed to parse token: %v", err)
	}
	if !token.Valid {
		t.Error("Token should be valid")
	}

	if token.Header["alg"] != "HS256" {
		t.Errorf("Expected alg=HS256, got %v", token.Header["alg"])
	}
}

// TestSignBackendToken_NoSigningMethod tests error when no signing method available
func TestSignBackendToken_NoSigningMethod(t *testing.T) {
	backendSigner = nil

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "",
		BackendKeyID:            "",
		HS256Secret:             "", // Neither configured!
	}

	claims := jwt.MapClaims{"sub": "user_789"}

	_, err := SignBackendToken(claims, cfg)
	if err == nil {
		t.Fatal("Expected error when no signing method available")
	}
	if !contains(err.Error(), "no signing method available") {
		t.Errorf("Expected 'no signing method available' error, got: %v", err)
	}
}

// TestValidateToken_RS256BackendToken tests full round-trip: sign with RS256, validate
func TestValidateToken_RS256BackendToken(t *testing.T) {
	backendSigner = nil
	globalJWKSCache = nil // Ensure JWKS isn't used

	// Setup backend signer
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	backendSigner = &BackendSigner{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
		KeyID:      "backend-rs256-key",
	}

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "dummy",
		BackendKeyID:            "backend-rs256-key",
		HS256Secret:             "fallback",
		Issuer:                  "https://external-idp.com", // External IdP config
		Audience:                "https://api.example.com",
	}

	// Sign a backend token
	claims := jwt.MapClaims{
		"sub":        "user_backend_rs256",
		"iss":        "toolbridge-api",
		"aud":        "internal-service",
		"token_type": "backend",
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
	}

	tokenString, err := SignBackendToken(claims, cfg)
	if err != nil {
		t.Fatalf("SignBackendToken failed: %v", err)
	}

	// Validate the token - should use backend signer's public key
	sub, returnedClaims, err := ValidateToken(tokenString, cfg)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if sub != "user_backend_rs256" {
		t.Errorf("Expected sub=%s, got %s", "user_backend_rs256", sub)
	}

	// Verify it was treated as backend token (no issuer/audience errors)
	if returnedClaims["token_type"] != "backend" {
		t.Errorf("Expected token_type=backend, got %v", returnedClaims["token_type"])
	}

	backendSigner = nil
}

// TestValidateToken_RS256BackendToken_WrongKid tests that wrong kid falls through to JWKS
func TestValidateToken_RS256BackendToken_WrongKid(t *testing.T) {
	backendSigner = nil
	globalJWKSCache = nil

	// Setup backend signer with one key ID
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	backendSigner = &BackendSigner{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
		KeyID:      "backend-key-1",
	}

	cfg := JWTCfg{
		BackendRSAPrivateKeyPEM: "dummy",
		BackendKeyID:            "backend-key-1",
	}

	// Create a token with DIFFERENT kid
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "user_123",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	token.Header["kid"] = "different-key-id" // Not matching backend signer

	tokenString, _ := token.SignedString(privateKey)

	// Should fail because kid doesn't match backend signer and no JWKS cache
	_, _, err := ValidateToken(tokenString, cfg)
	if err == nil {
		t.Fatal("Expected error for non-matching kid without JWKS")
	}
	// Error is wrapped: "jwt validation failed: token is unverifiable: ..."
	// The contains() helper only checks prefixes, so check for jwt validation failed
	if !contains(err.Error(), "jwt validation failed") {
		t.Errorf("Expected jwt validation error, got: %v", err)
	}

	backendSigner = nil
}

// Helper functions for tests
func marshalPKCS8PrivateKey(key *rsa.PrivateKey) ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(key)
}

func marshalPKCS1PrivateKey(key *rsa.PrivateKey) []byte {
	return x509.MarshalPKCS1PrivateKey(key)
}

func pemEncode(data []byte, blockType string) string {
	block := &pem.Block{
		Type:  blockType,
		Bytes: data,
	}
	return string(pem.EncodeToMemory(block))
}
