package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleOAuthMetadata serves the OAuth authorization server metadata
// Reference: RFC 8414 (OAuth 2.0 Authorization Server Metadata)
func (s *MCPServer) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
	domain := s.config.Auth0.Domain
	issuer := fmt.Sprintf("https://%s/", domain)

	metadata := map[string]interface{}{
		"issuer":                issuer,
		"authorization_endpoint": fmt.Sprintf("https://%s/authorize", domain),
		"token_endpoint":         fmt.Sprintf("https://%s/oauth/token", domain),
		"jwks_uri":               fmt.Sprintf("https://%s/.well-known/jwks.json", domain),
		"response_types_supported": []string{"code"},
		"grant_types_supported":    []string{"authorization_code", "refresh_token"},
		"subject_types_supported":  []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported": []string{"openid", "profile", "email", "offline_access"},
		"token_endpoint_auth_methods_supported": []string{
			"client_secret_basic",
			"client_secret_post",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// handleOAuthProtectedResourceMetadata serves the OAuth protected resource metadata
// Reference: RFC 9728 (OAuth 2.0 Protected Resource Metadata)
// Required by MCP specification (June 2025 update) for Claude Desktop integration
func (s *MCPServer) handleOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	domain := s.config.Auth0.Domain
	issuer := fmt.Sprintf("https://%s/", domain)

	// Get the resource URL from the request or config
	// In production, this should be the public-facing MCP endpoint URL
	resourceURL := s.config.PublicURL
	if resourceURL == "" {
		// Fallback: construct from request
		// Check X-Forwarded-Proto first (for TLS-terminating proxies like Envoy)
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			// Fall back to checking direct TLS connection
			if r.TLS != nil {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}
		resourceURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	metadata := map[string]interface{}{
		"resource":               resourceURL,
		"authorization_servers":  []string{issuer},
		"bearer_methods_supported": []string{"header"},
		"resource_documentation": fmt.Sprintf("%s/mcp", resourceURL),
		// Optional: Specify supported signing algorithms (matches Auth0 JWKS)
		"resource_signing_alg_values_supported": []string{"RS256"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}
