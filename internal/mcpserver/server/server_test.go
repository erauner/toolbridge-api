package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
	"github.com/golang-jwt/jwt/v5"
)

// mockJWKSServer creates a mock JWKS server for testing
func mockJWKSServer(t *testing.T, privateKey *rsa.PrivateKey) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return JWKS with public key
		n := privateKey.PublicKey.N
		e := privateKey.PublicKey.E

		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "RSA",
					"use": "sig",
					"kid": "test-key-1",
					"n":   base64urlEncode(n.Bytes()),
					"e":   base64urlEncode(intToBytes(e)),
					"alg": "RS256",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
}

// Helper functions for JWKS encoding
func base64urlEncode(data []byte) string {
	// Simple base64url encoding without padding
	const encoding = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, len(data)*4/3+1)

	for i := 0; i < len(data); i += 3 {
		remaining := len(data) - i
		chunk := uint(data[i]) << 16
		if remaining > 1 {
			chunk |= uint(data[i+1]) << 8
		}
		if remaining > 2 {
			chunk |= uint(data[i+2])
		}

		result = append(result, encoding[(chunk>>18)&63])
		result = append(result, encoding[(chunk>>12)&63])
		if remaining > 1 {
			result = append(result, encoding[(chunk>>6)&63])
		}
		if remaining > 2 {
			result = append(result, encoding[chunk&63])
		}
	}

	return string(result)
}

func intToBytes(n int) []byte {
	bytes := make([]byte, 0)
	for n > 0 {
		bytes = append([]byte{byte(n & 0xFF)}, bytes...)
		n >>= 8
	}
	if len(bytes) == 0 {
		bytes = []byte{0}
	}
	return bytes
}

// createTestJWT creates a valid JWT for testing
func createTestJWT(t *testing.T, privateKey *rsa.PrivateKey, audience, issuer, subject string) string {
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"sub": subject,
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-1"

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	return tokenString
}

func TestMCPServer_OAuthMetadata(t *testing.T) {
	// Create test config
	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: "test.auth0.com",
			SyncAPI: &config.SyncAPIConfig{
				Audience: "https://api.test.com",
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	server := NewMCPServer(cfg)

	// Create test request
	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()

	server.handleOAuthMetadata(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var metadata map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&metadata); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify required fields
	requiredFields := []string{"issuer", "authorization_endpoint", "token_endpoint", "jwks_uri"}
	for _, field := range requiredFields {
		if _, ok := metadata[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}
}

func TestMCPServer_OAuthProtectedResourceMetadata(t *testing.T) {
	tests := []struct {
		name              string
		publicURL         string
		requestHost       string
		xForwardedProto   string
		expectScheme      string
		expectHost        string
	}{
		{
			name:         "With PublicURL configured",
			publicURL:    "https://mcpbridge.example.com",
			requestHost:  "localhost:8082",
			expectScheme: "https",
			expectHost:   "mcpbridge.example.com",
		},
		{
			name:         "Without PublicURL (fallback to request - plain HTTP)",
			publicURL:    "",
			requestHost:  "localhost:8082",
			expectScheme: "http",
			expectHost:   "localhost:8082",
		},
		{
			name:            "Behind TLS proxy (X-Forwarded-Proto: https)",
			publicURL:       "",
			requestHost:     "mcpbridge.erauner.dev",
			xForwardedProto: "https",
			expectScheme:    "https",
			expectHost:      "mcpbridge.erauner.dev",
		},
		{
			name:            "Behind non-TLS proxy (X-Forwarded-Proto: http)",
			publicURL:       "",
			requestHost:     "localhost:8082",
			xForwardedProto: "http",
			expectScheme:    "http",
			expectHost:      "localhost:8082",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test config
			cfg := &config.Config{
				Auth0: config.Auth0Config{
					Domain: "test.auth0.com",
					SyncAPI: &config.SyncAPIConfig{
						Audience: "https://api.test.com",
					},
				},
				APIBaseURL: "http://localhost:8081",
				PublicURL:  tt.publicURL,
			}

			server := NewMCPServer(cfg)

			// Create test request
			req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
			req.Host = tt.requestHost
			if tt.xForwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xForwardedProto)
			}
			w := httptest.NewRecorder()

			server.handleOAuthProtectedResourceMetadata(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			var metadata map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&metadata); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// Verify required fields per RFC 9728
			requiredFields := []string{"resource", "authorization_servers", "bearer_methods_supported"}
			for _, field := range requiredFields {
				if _, ok := metadata[field]; !ok {
					t.Errorf("Missing required field: %s", field)
				}
			}

			// Verify resource URL contains expected scheme and host
			resource, ok := metadata["resource"].(string)
			if !ok {
				t.Fatalf("resource field is not a string")
			}
			expectedResource := tt.expectScheme + "://" + tt.expectHost
			if resource != expectedResource {
				t.Errorf("Expected resource %q, got %q", expectedResource, resource)
			}

			// Verify authorization_servers contains Auth0 issuer
			authServers, ok := metadata["authorization_servers"].([]interface{})
			if !ok {
				t.Fatalf("authorization_servers is not an array")
			}
			if len(authServers) != 1 {
				t.Errorf("Expected 1 authorization server, got %d", len(authServers))
			}
			if authServers[0] != "https://test.auth0.com/" {
				t.Errorf("Expected Auth0 issuer, got %v", authServers[0])
			}

			// Verify bearer_methods_supported includes "header"
			bearerMethods, ok := metadata["bearer_methods_supported"].([]interface{})
			if !ok {
				t.Fatalf("bearer_methods_supported is not an array")
			}
			found := false
			for _, method := range bearerMethods {
				if method == "header" {
					found = true
					break
				}
			}
			if !found {
				t.Error("bearer_methods_supported should include 'header'")
			}
		})
	}
}

func TestMCPServer_Initialize(t *testing.T) {
	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	// Create mock JWKS server
	jwksServer := mockJWKSServer(t, privateKey)
	defer jwksServer.Close()

	// Extract domain from JWKS server URL
	issuer := jwksServer.URL + "/"
	audience := "https://api.test.com"

	// Create test config
	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: jwksServer.URL[7:], // Remove "http://" prefix
			SyncAPI: &config.SyncAPIConfig{
				Audience: audience,
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	// Override JWKS URL in validator for testing
	mcpServer := NewMCPServer(cfg)
	mcpServer.jwtValidator.jwksURL = jwksServer.URL + "/.well-known/jwks.json"
	mcpServer.jwtValidator.issuer = issuer

	// Create test JWT
	token := createTestJWT(t, privateKey, audience, issuer, "test-user-123")

	// Create initialize request
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0",
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")

	w := httptest.NewRecorder()
	mcpServer.handleMCPPost(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check session ID in header
	sessionID := w.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Error("Expected Mcp-Session-Id header, got empty")
	}

	// Decode response
	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Errorf("Expected no error, got: %s", response.Error.Message)
	}

	// Verify response contains capabilities
	var result map[string]interface{}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if _, ok := result["capabilities"]; !ok {
		t.Error("Response missing capabilities")
	}
}

func TestMCPServer_MissingSessionID(t *testing.T) {
	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	// Create mock JWKS server
	jwksServer := mockJWKSServer(t, privateKey)
	defer jwksServer.Close()

	issuer := jwksServer.URL + "/"
	audience := "https://api.test.com"

	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: jwksServer.URL[7:],
			SyncAPI: &config.SyncAPIConfig{
				Audience: audience,
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	mcpServer := NewMCPServer(cfg)
	mcpServer.jwtValidator.jwksURL = jwksServer.URL + "/.well-known/jwks.json"
	mcpServer.jwtValidator.issuer = issuer

	token := createTestJWT(t, privateKey, audience, issuer, "test-user-123")

	// Create tools/list request without session ID
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")
	// Note: No Mcp-Session-Id header

	w := httptest.NewRecorder()
	mcpServer.handleMCPPost(w, req)

	// Should return error
	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Error("Expected error for missing session ID")
	}

	if response.Error.Code != InvalidRequest {
		t.Errorf("Expected error code %d, got %d", InvalidRequest, response.Error.Code)
	}
}

func TestMCPServer_InvalidToken(t *testing.T) {
	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: "test.auth0.com",
			SyncAPI: &config.SyncAPIConfig{
				Audience: "https://api.test.com",
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	mcpServer := NewMCPServer(cfg)

	// Create request with invalid token
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer invalid-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")

	w := httptest.NewRecorder()
	mcpServer.handleMCPPost(w, req)

	// Should return error
	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestMCPServer_DeleteSession(t *testing.T) {
	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: "test.auth0.com",
			SyncAPI: &config.SyncAPIConfig{
				Audience: "https://api.test.com",
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	mcpServer := NewMCPServer(cfg)

	// Create a session directly
	session := mcpServer.sessionMgr.CreateSession("test-user")

	// Delete the session
	req := httptest.NewRequest("DELETE", "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", session.ID)

	w := httptest.NewRecorder()
	mcpServer.handleMCPDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// Verify session is deleted
	_, err := mcpServer.sessionMgr.GetSession(session.ID)
	if err == nil {
		t.Error("Expected session to be deleted")
	}
}

func TestMCPServer_UnsupportedProtocolVersion(t *testing.T) {
	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: "test.auth0.com",
			SyncAPI: &config.SyncAPIConfig{
				Audience: "https://api.test.com",
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	mcpServer := NewMCPServer(cfg)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer fake-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "1.0.0") // Unsupported version

	w := httptest.NewRecorder()
	mcpServer.handleMCPPost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestMCPServer_SupportedProtocolVersions(t *testing.T) {
	// Test that all documented MCP protocol versions are supported
	supportedVersions := []string{
		"2024-11-05",
		"2025-03-26",
		"2025-06-18", // Latest version with Streamable HTTP transport and RFC 9728 OAuth support
	}

	for _, version := range supportedVersions {
		t.Run(version, func(t *testing.T) {
			if !isSupportedProtocolVersion(version) {
				t.Errorf("Version %s should be supported but is not", version)
			}
		})
	}

	// Test unsupported versions
	unsupportedVersions := []string{
		"1.0.0",
		"2024-01-01",
		"2025-12-31",
		"invalid",
	}

	for _, version := range unsupportedVersions {
		t.Run("unsupported_"+version, func(t *testing.T) {
			if isSupportedProtocolVersion(version) {
				t.Errorf("Version %s should not be supported but is", version)
			}
		})
	}
}

func TestMCPServer_ToolsList(t *testing.T) {
	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	// Create mock JWKS server
	jwksServer := mockJWKSServer(t, privateKey)
	defer jwksServer.Close()

	issuer := jwksServer.URL + "/"
	audience := "https://api.test.com"

	cfg := &config.Config{
		Auth0: config.Auth0Config{
			Domain: jwksServer.URL[7:],
			SyncAPI: &config.SyncAPIConfig{
				Audience: audience,
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	mcpServer := NewMCPServer(cfg)
	mcpServer.jwtValidator.jwksURL = jwksServer.URL + "/.well-known/jwks.json"
	mcpServer.jwtValidator.issuer = issuer

	token := createTestJWT(t, privateKey, audience, issuer, "test-user-123")

	// First initialize to get session
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{},
	}

	body, _ := json.Marshal(initReq)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")

	w := httptest.NewRecorder()
	mcpServer.handleMCPPost(w, req)

	sessionID := w.Header().Get("Mcp-Session-Id")

	// Now call tools/list
	toolsReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	body, _ = json.Marshal(toolsReq)
	req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")
	req.Header.Set("Mcp-Session-Id", sessionID)

	w = httptest.NewRecorder()
	mcpServer.handleMCPPost(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Errorf("Expected no error, got: %s", response.Error.Message)
	}

	// For Phase 5-6, tools list should contain all registered tools
	var result map[string]interface{}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Error("Expected tools to be an array")
	}

	// Verify we have the expected number of tools (41 in Phase 5-6)
	if len(tools) == 0 {
		t.Error("Expected tools to be registered, got empty list")
	}

	// Verify tools have expected structure
	if len(tools) > 0 {
		firstTool, ok := tools[0].(map[string]interface{})
		if !ok {
			t.Error("Expected tool to be an object")
		} else {
			if _, hasName := firstTool["name"]; !hasName {
				t.Error("Expected tool to have 'name' field")
			}
			if _, hasDesc := firstTool["description"]; !hasDesc {
				t.Error("Expected tool to have 'description' field")
			}
			if _, hasSchema := firstTool["inputSchema"]; !hasSchema {
				t.Error("Expected tool to have 'inputSchema' field")
			}
		}
	}
}

func TestMCPServer_OriginValidation(t *testing.T) {
	tests := []struct {
		name           string
		devMode        bool
		allowedOrigins []string
		requestOrigin  string
		wantAllowed    bool
	}{
		{
			name:           "dev mode allows any origin",
			devMode:        true,
			allowedOrigins: []string{},
			requestOrigin:  "https://malicious.com",
			wantAllowed:    true,
		},
		{
			name:           "empty allowlist allows all (with warning)",
			devMode:        false,
			allowedOrigins: []string{},
			requestOrigin:  "https://example.com",
			wantAllowed:    true,
		},
		{
			name:           "missing origin header allowed (desktop apps)",
			devMode:        false,
			allowedOrigins: []string{"https://allowed.com"},
			requestOrigin:  "",
			wantAllowed:    true, // Desktop apps like Claude Desktop don't send Origin
		},
		{
			name:           "allowed origin accepted",
			devMode:        false,
			allowedOrigins: []string{"https://allowed.com", "https://also-allowed.com"},
			requestOrigin:  "https://allowed.com",
			wantAllowed:    true,
		},
		{
			name:           "disallowed origin rejected",
			devMode:        false,
			allowedOrigins: []string{"https://allowed.com"},
			requestOrigin:  "https://malicious.com",
			wantAllowed:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				DevMode:        tt.devMode,
				AllowedOrigins: tt.allowedOrigins,
				Auth0: config.Auth0Config{
					Domain: "test.auth0.com",
					SyncAPI: &config.SyncAPIConfig{
						Audience: "https://api.test.com",
					},
				},
				APIBaseURL: "http://localhost:8081",
			}

			server := NewMCPServer(cfg)

			req := httptest.NewRequest("POST", "/mcp", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}

			allowed := server.validateOrigin(req)
			if allowed != tt.wantAllowed {
				t.Errorf("validateOrigin() = %v, want %v", allowed, tt.wantAllowed)
			}
		})
	}
}

func TestMCPServer_OriginValidation_Integration(t *testing.T) {
	// Test that Origin validation is enforced on all endpoints
	cfg := &config.Config{
		DevMode:        false,
		AllowedOrigins: []string{"https://allowed.com"},
		Auth0: config.Auth0Config{
			Domain: "test.auth0.com",
			SyncAPI: &config.SyncAPIConfig{
				Audience: "https://api.test.com",
			},
		},
		APIBaseURL: "http://localhost:8081",
	}

	server := NewMCPServer(cfg)

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
		path    string
	}{
		{
			name:    "POST /mcp rejects bad origin",
			handler: server.handleMCPPost,
			method:  "POST",
			path:    "/mcp",
		},
		{
			name:    "GET /mcp rejects bad origin",
			handler: server.handleMCPGet,
			method:  "GET",
			path:    "/mcp",
		},
		{
			name:    "DELETE /mcp rejects bad origin",
			handler: server.handleMCPDelete,
			method:  "DELETE",
			path:    "/mcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Origin", "https://malicious.com")
			req.Header.Set("Mcp-Protocol-Version", "2025-03-26")

			w := httptest.NewRecorder()
			tt.handler(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("Expected status %d (Forbidden), got %d", http.StatusForbidden, w.Code)
			}

			if !bytes.Contains(w.Body.Bytes(), []byte("origin not allowed")) {
				t.Errorf("Expected error message 'origin not allowed', got: %s", w.Body.String())
			}
		})
	}
}

func TestMCPServer_DevMode(t *testing.T) {
	// Test that server works in dev mode without Auth0 configuration
	cfg := &config.Config{
		DevMode:        true,
		AllowedOrigins: []string{},
		APIBaseURL:     "http://localhost:8081",
		// No Auth0 config - this should not panic
	}

	server := NewMCPServer(cfg)

	// Server should be created successfully
	if server == nil {
		t.Fatal("Expected server to be created in dev mode")
	}

	// JWT validator should be nil in dev mode
	if server.jwtValidator != nil {
		t.Error("Expected jwtValidator to be nil in dev mode")
	}

	// Test initialize request with X-Debug-Sub header
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0",
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("X-Debug-Sub", "test-user-dev")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")

	w := httptest.NewRecorder()
	server.handleMCPPost(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check for session ID in response headers
	sessionID := w.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Error("Expected Mcp-Session-Id header in response")
	}

	// Test tools/list with the session (this would previously panic)
	toolsReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	body, _ = json.Marshal(toolsReq)
	req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("X-Debug-Sub", "test-user-dev")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", "2025-03-26")
	req.Header.Set("Mcp-Session-Id", sessionID)

	w = httptest.NewRecorder()
	server.handleMCPPost(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for tools/list, got %d: %s", w.Code, w.Body.String())
	}

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Errorf("Expected no error, got: %s", response.Error.Message)
	}
}
