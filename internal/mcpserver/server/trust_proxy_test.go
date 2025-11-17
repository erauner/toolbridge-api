package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
)

// createUnsignedTestJWT creates a simple test JWT without real signature
// (for trust-proxy tests where signature validation is skipped)
func createUnsignedTestJWT(sub string, exp time.Time) string {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	payload := map[string]interface{}{
		"sub": sub,
		"exp": exp.Unix(),
		"aud": "test-audience",
		"iss": "https://test.auth0.com/",
	}
	payloadJSON, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	// Fake signature (trust-proxy mode doesn't validate it)
	signature := "fake_signature"

	return headerB64 + "." + payloadB64 + "." + signature
}

func TestTrustProxyAuth_ParseJWTClaimsWithoutValidation(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectSub   string
		expectError bool
	}{
		{
			name:        "valid JWT",
			token:       createUnsignedTestJWT("user123", time.Now().Add(1*time.Hour)),
			expectSub:   "user123",
			expectError: false,
		},
		{
			name:        "expired JWT (should still parse)",
			token:       createUnsignedTestJWT("user456", time.Now().Add(-1*time.Hour)),
			expectSub:   "user456",
			expectError: false, // No validation, just parsing
		},
		{
			name:        "invalid format - missing parts",
			token:       "invalid.token",
			expectError: true,
		},
		{
			name:        "invalid format - not base64",
			token:       "not.base64.data!!!",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, _, err := parseJWTClaimsWithoutValidation(tt.token)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			sub, ok := claims["sub"].(string)
			if !ok {
				t.Errorf("Expected sub claim to be string")
				return
			}

			if sub != tt.expectSub {
				t.Errorf("Expected sub=%s, got %s", tt.expectSub, sub)
			}
		})
	}
}

func TestTrustProxyAuth_HandleMCPPost(t *testing.T) {
	cfg := &config.Config{
		TrustToolhiveAuth: true,
		APIBaseURL:        "http://localhost:8081",
		Auth0: config.Auth0Config{
			SyncAPI: &config.SyncAPIConfig{
				Audience: "test-audience",
			},
		},
	}

	server := NewMCPServer(cfg)

	tests := []struct {
		name          string
		authHeader    string
		expectedError string // JSON-RPC error message (all return HTTP 200)
	}{
		{
			name:          "valid JWT in trust-proxy mode",
			authHeader:    "Bearer " + createUnsignedTestJWT("user123", time.Now().Add(1*time.Hour)),
			expectedError: "", // No error expected
		},
		{
			name:          "missing Authorization header",
			authHeader:    "",
			expectedError: "missing authorization header from proxy",
		},
		{
			name:          "invalid Authorization header format",
			authHeader:    "NotBearer token",
			expectedError: "missing authorization header from proxy",
		},
		{
			name:          "malformed JWT",
			authHeader:    "Bearer invalid.jwt",
			expectedError: "invalid jwt format",
		},
		{
			name:          "JWT without sub claim",
			authHeader:    "Bearer " + createTestJWTWithoutSub(),
			expectedError: "missing sub claim in jwt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
			req := httptest.NewRequest("POST", "/mcp", body)
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			server.handleMCPPost(w, req)

			// JSON-RPC always returns HTTP 200, check response body for errors
			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d (JSON-RPC), got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
			}

			bodyStr := w.Body.String()
			if tt.expectedError != "" {
				// Should contain JSON-RPC error
				if !strings.Contains(bodyStr, tt.expectedError) {
					t.Errorf("Expected error containing %q, got body: %s", tt.expectedError, bodyStr)
				}
			} else {
				// Should NOT contain error field
				if strings.Contains(bodyStr, `"error"`) {
					t.Errorf("Expected no error, got body: %s", bodyStr)
				}
			}
		})
	}
}

func TestTrustProxyAuth_HandleMCPDelete(t *testing.T) {
	cfg := &config.Config{
		TrustToolhiveAuth: true,
		APIBaseURL:        "http://localhost:8081",
		Auth0: config.Auth0Config{
			SyncAPI: &config.SyncAPIConfig{
				Audience: "test-audience",
			},
		},
	}

	tests := []struct {
		name           string
		setupSession   bool   // Whether to create a session before the test
		sessionID      string
		sessionUserID  string // User ID for the session (if created)
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "valid delete with matching user",
			setupSession:   true,
			sessionID:      "test-session-123",
			sessionUserID:  "user123",
			authHeader:     "Bearer " + createUnsignedTestJWT("user123", time.Now().Add(1*time.Hour)),
			expectedStatus: http.StatusNoContent,
		},
		{
			name:           "missing Authorization header",
			setupSession:   true,
			sessionID:      "test-session-456",
			sessionUserID:  "user123",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "session user mismatch",
			setupSession:   true,
			sessionID:      "test-session-789",
			sessionUserID:  "user123",
			authHeader:     "Bearer " + createUnsignedTestJWT("different-user", time.Now().Add(1*time.Hour)),
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "session not found",
			setupSession:   false, // Don't create session
			sessionID:      "nonexistent-session",
			authHeader:     "Bearer " + createUnsignedTestJWT("user123", time.Now().Add(1*time.Hour)),
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh server for each test
			server := NewMCPServer(cfg)

			// Setup session if needed
			if tt.setupSession {
				session := &MCPSession{
					ID:        tt.sessionID,
					UserID:    tt.sessionUserID,
					CreatedAt: time.Now(),
					LastSeen:  time.Now(),
				}
				server.sessionMgr.sessions[tt.sessionID] = session
			}

			req := httptest.NewRequest("DELETE", "/mcp", nil)
			req.Header.Set("Mcp-Session-Id", tt.sessionID)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			server.handleMCPDelete(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestTrustProxyAuth_OAuthMetadataEndpoints(t *testing.T) {
	cfg := &config.Config{
		TrustToolhiveAuth: true,
		APIBaseURL:        "http://localhost:8081",
		Auth0: config.Auth0Config{
			SyncAPI: &config.SyncAPIConfig{
				Audience: "test-audience",
			},
		},
	}

	server := NewMCPServer(cfg)

	tests := []struct {
		name           string
		endpoint       string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "OAuth authorization server metadata returns 501",
			endpoint:       "/.well-known/oauth-authorization-server",
			expectedStatus: http.StatusNotImplemented,
			expectedBody:   "OAuth metadata not available - authentication handled by proxy",
		},
		{
			name:           "OAuth protected resource metadata returns 501",
			endpoint:       "/.well-known/oauth-protected-resource",
			expectedStatus: http.StatusNotImplemented,
			expectedBody:   "OAuth metadata not available - authentication handled by proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.endpoint, nil)
			w := httptest.NewRecorder()

			if tt.endpoint == "/.well-known/oauth-authorization-server" {
				server.handleOAuthMetadata(w, req)
			} else {
				server.handleOAuthProtectedResourceMetadata(w, req)
			}

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedBody != "" && !strings.Contains(w.Body.String(), tt.expectedBody) {
				t.Errorf("Expected body to contain %q, got: %s", tt.expectedBody, w.Body.String())
			}
		})
	}
}

// Helper function to create a JWT without sub claim
func createTestJWTWithoutSub() string {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	payload := map[string]interface{}{
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"aud": "test-audience",
		"iss": "https://test.auth0.com/",
		// No "sub" claim
	}
	payloadJSON, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signature := "fake_signature"

	return headerB64 + "." + payloadB64 + "." + signature
}
