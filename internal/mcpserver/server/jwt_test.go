package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestJWTValidator_IntrospectionFallback tests that introspection is used when JWT validation fails
func TestJWTValidator_IntrospectionFallback(t *testing.T) {
	audience := "https://api.test.com"
	issuer := "https://test.auth0.com/"
	sub := "test-user-123"

	// Create mock introspection endpoint that returns valid response
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST request
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify Basic Auth credentials
		username, password, ok := r.BasicAuth()
		if !ok || username != "test-client-id" || password != "test-client-secret" {
			t.Error("Invalid or missing Basic Auth credentials")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			t.Errorf("Failed to parse form: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		token := r.FormValue("token")
		if token != "opaque-token-abc123" {
			t.Errorf("Expected token 'opaque-token-abc123', got '%s'", token)
		}

		// Return successful introspection response
		response := IntrospectionResponse{
			Active: true,
			Sub:    sub,
			Exp:    time.Now().Add(1 * time.Hour).Unix(),
			Iat:    time.Now().Unix(),
			Aud:    audience, // Single string audience
			Scope:  "read:data write:data",
			Iss:    issuer,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer introspectionServer.Close()

	// Create introspector with mock endpoint
	introspector := &TokenIntrospector{
		endpoint:     introspectionServer.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		audience:     audience,
		issuer:       issuer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Create JWT validator with introspector
	validator := &JWTValidator{
		audience:     audience,
		issuer:       issuer,
		introspector: introspector,
	}

	// Test with opaque token (JWT validation will fail, introspection should succeed)
	claims, usedIntrospection, err := validator.ValidateToken("opaque-token-abc123")

	// Verify introspection was used
	if err != nil {
		t.Fatalf("Expected validation to succeed via introspection, got error: %v", err)
	}

	if !usedIntrospection {
		t.Error("Expected introspection to be used, but it wasn't")
	}

	if claims.Subject != sub {
		t.Errorf("Expected subject '%s', got '%s'", sub, claims.Subject)
	}

	if claims.Issuer != issuer {
		t.Errorf("Expected issuer '%s', got '%s'", issuer, claims.Issuer)
	}

	if len(claims.Audience) != 1 || claims.Audience[0] != audience {
		t.Errorf("Expected audience ['%s'], got %v", audience, claims.Audience)
	}
}

// TestJWTValidator_IntrospectionRejectsWrongIssuer tests that introspection rejects tokens with wrong issuer
func TestJWTValidator_IntrospectionRejectsWrongIssuer(t *testing.T) {
	expectedIssuer := "https://test.auth0.com/"
	wrongIssuer := "https://malicious.com/"
	audience := "https://api.test.com"

	// Create mock introspection endpoint that returns wrong issuer
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := IntrospectionResponse{
			Active: true,
			Sub:    "test-user",
			Exp:    time.Now().Add(1 * time.Hour).Unix(),
			Aud:    audience,
			Iss:    wrongIssuer, // Wrong issuer!
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer introspectionServer.Close()

	introspector := &TokenIntrospector{
		endpoint:     introspectionServer.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		audience:     audience,
		issuer:       expectedIssuer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Introspect should reject the token
	_, err := introspector.Introspect(context.Background(), "some-token")
	if err == nil {
		t.Fatal("Expected introspection to fail due to wrong issuer, but it succeeded")
	}

	// Error message should mention issuer mismatch
	if err.Error() != "invalid issuer: expected https://test.auth0.com/, got https://malicious.com/" {
		t.Errorf("Expected issuer mismatch error, got: %v", err)
	}
}

// TestJWTValidator_IntrospectionRejectsWrongAudience tests that introspection rejects tokens with wrong audience
func TestJWTValidator_IntrospectionRejectsWrongAudience(t *testing.T) {
	issuer := "https://test.auth0.com/"
	expectedAudience := "https://api.test.com"
	wrongAudience := "https://other-api.com"

	// Create mock introspection endpoint that returns wrong audience
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := IntrospectionResponse{
			Active: true,
			Sub:    "test-user",
			Exp:    time.Now().Add(1 * time.Hour).Unix(),
			Aud:    wrongAudience, // Wrong audience!
			Iss:    issuer,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer introspectionServer.Close()

	introspector := &TokenIntrospector{
		endpoint:     introspectionServer.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		audience:     expectedAudience,
		issuer:       issuer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Introspect should reject the token
	_, err := introspector.Introspect(context.Background(), "some-token")
	if err == nil {
		t.Fatal("Expected introspection to fail due to wrong audience, but it succeeded")
	}

	// Error message should mention audience mismatch
	if err.Error() != "invalid audience: expected https://api.test.com, got [https://other-api.com]" {
		t.Errorf("Expected audience mismatch error, got: %v", err)
	}
}

// TestJWTValidator_IntrospectionArrayAudience tests audience validation with array format
func TestJWTValidator_IntrospectionArrayAudience(t *testing.T) {
	issuer := "https://test.auth0.com/"
	expectedAudience := "https://api.test.com"
	sub := "test-user-123"

	// Create mock introspection endpoint with array audience
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := IntrospectionResponse{
			Active: true,
			Sub:    sub,
			Exp:    time.Now().Add(1 * time.Hour).Unix(),
			// Array audience with expected audience in the list
			Aud: []interface{}{"https://other-api.com", expectedAudience},
			Iss: issuer,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer introspectionServer.Close()

	introspector := &TokenIntrospector{
		endpoint:     introspectionServer.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		audience:     expectedAudience,
		issuer:       issuer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Introspect should succeed (finds expected audience in array)
	claims, err := introspector.Introspect(context.Background(), "some-token")
	if err != nil {
		t.Fatalf("Expected introspection to succeed with array audience, got error: %v", err)
	}

	if claims.Subject != sub {
		t.Errorf("Expected subject '%s', got '%s'", sub, claims.Subject)
	}

	// Verify audience was extracted correctly
	if len(claims.Audience) != 1 || claims.Audience[0] != expectedAudience {
		t.Errorf("Expected audience ['%s'], got %v", expectedAudience, claims.Audience)
	}
}

// TestJWTValidator_IntrospectionInactiveToken tests that inactive tokens are rejected
func TestJWTValidator_IntrospectionInactiveToken(t *testing.T) {
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	// Create mock introspection endpoint that returns inactive token
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := IntrospectionResponse{
			Active: false, // Token is not active
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer introspectionServer.Close()

	introspector := &TokenIntrospector{
		endpoint:     introspectionServer.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		audience:     audience,
		issuer:       issuer,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Introspect should reject inactive token
	_, err := introspector.Introspect(context.Background(), "inactive-token")
	if err == nil {
		t.Fatal("Expected introspection to fail for inactive token, but it succeeded")
	}

	if err.Error() != "token is not active" {
		t.Errorf("Expected 'token is not active' error, got: %v", err)
	}
}

// TestJWTValidator_NoIntrospectorConfigured tests fallback when no introspector is configured
func TestJWTValidator_NoIntrospectorConfigured(t *testing.T) {
	// Create validator without introspector
	validator := &JWTValidator{
		audience:     "https://api.test.com",
		issuer:       "https://test.auth0.com/",
		introspector: nil, // No introspector
	}

	// Attempt to validate opaque token (will fail JWT parsing, no introspection fallback)
	_, usedIntrospection, err := validator.ValidateToken("opaque-token")

	// Should fail with JWT error
	if err == nil {
		t.Fatal("Expected validation to fail without introspector, but it succeeded")
	}

	if usedIntrospection {
		t.Error("Expected introspection NOT to be used (none configured)")
	}
}
