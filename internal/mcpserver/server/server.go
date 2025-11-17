package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/client"
	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
	"github.com/erauner12/toolbridge-api/internal/mcpserver/tools"
	"github.com/rs/zerolog/log"
)

// MCPServer is the main Streamable HTTP MCP server
type MCPServer struct {
	config       *config.Config
	httpServer   *http.Server
	jwtValidator *JWTValidator
	sessionMgr   *SessionManager
	toolRegistry *tools.Registry
}

// NewMCPServer creates a new MCP server
func NewMCPServer(cfg *config.Config) *MCPServer {
	var jwtValidator *JWTValidator

	// Only create JWT validator if not in dev mode and Auth0 is configured
	if !cfg.DevMode && cfg.Auth0.SyncAPI != nil {
		// Convert introspection config if present
		var introspectionCfg *IntrospectionConfig
		if cfg.Auth0.Introspection != nil {
			introspectionCfg = &IntrospectionConfig{
				ClientID:     cfg.Auth0.Introspection.ClientID,
				ClientSecret: cfg.Auth0.Introspection.ClientSecret,
				Audience:     cfg.Auth0.Introspection.Audience,
			}
		}
		jwtValidator = NewJWTValidator(cfg.Auth0.Domain, cfg.Auth0.SyncAPI.Audience, introspectionCfg)
	}

	sessionMgr := NewSessionManager(24 * time.Hour)

	// Create and register all tools
	toolRegistry := tools.NewRegistry()
	tools.RegisterAllTools(toolRegistry)

	return &MCPServer{
		config:       cfg,
		jwtValidator: jwtValidator,
		sessionMgr:   sessionMgr,
		toolRegistry: toolRegistry,
	}
}

// WarmUp pre-fetches JWKS to make the server ready faster
// This is optional but recommended during startup.
// If the initial warmup fails, a background retry goroutine is started
// to ensure the validator eventually becomes ready even if Auth0 is
// temporarily unreachable during startup.
func (s *MCPServer) WarmUp() error {
	if s.jwtValidator != nil {
		err := s.jwtValidator.WarmUp()
		if err != nil {
			// Start background retry to recover from transient failures
			log.Info().Msg("Starting background retry for JWT validator warmup")
			s.jwtValidator.StartBackgroundRetry()
			return err
		}
		return nil
	}
	// No warmup needed in dev mode
	return nil
}

// Start starts the HTTP server
func (s *MCPServer) Start(addr string) error {
	mux := http.NewServeMux()

	// Health endpoints (no auth required for k8s probes)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)

	// MCP endpoints
	mux.HandleFunc("POST /mcp", s.handleMCPPost)
	mux.HandleFunc("GET /mcp", s.handleMCPGet)
	mux.HandleFunc("DELETE /mcp", s.handleMCPDelete)

	// OAuth discovery
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleOAuthMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleOAuthProtectedResourceMetadata)

	s.httpServer = &http.Server{
		Addr:        addr,
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
		// WriteTimeout is intentionally omitted to support long-lived SSE connections
		// SSE streams can stay open indefinitely for server-to-client notifications
	}

	log.Info().Str("addr", addr).Msg("Starting MCP server")
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *MCPServer) Shutdown(ctx context.Context) error {
	// Stop background retry goroutine (safe to call even if not running)
	if s.jwtValidator != nil {
		s.jwtValidator.StopBackgroundRetry()
	}

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// handleMCPPost handles POST /mcp (JSON-RPC requests)
func (s *MCPServer) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	// Validate Origin header (DNS rebinding protection)
	if !s.validateOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	// Get protocol version (defaults to 2025-06-18 if not specified)
	protocolVersion := getProtocolVersion(r)
	if !isSupportedProtocolVersion(protocolVersion) {
		log.Warn().
			Str("version", protocolVersion).
			Msg("Unsupported MCP protocol version")
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}

	log.Debug().
		Str("version", protocolVersion).
		Msg("MCP protocol version validated")

	var userID string
	var tokenString string
	var expiresAt time.Time

	// Check for dev mode
	if s.config.DevMode {
		// In dev mode, allow X-Debug-Sub header as fallback
		debugSub := r.Header.Get("X-Debug-Sub")
		if debugSub != "" {
			userID = debugSub
			tokenString = "dev-mode-token"
			expiresAt = time.Now().Add(24 * time.Hour)
			log.Debug().Str("userId", userID).Msg("Using dev mode authentication")
		}
	}

	// If not using dev mode or no debug header, use JWT
	if userID == "" {
		// JWT validation required but validator not configured
		if s.jwtValidator == nil {
			s.sendError(w, nil, InternalError, "authentication not configured")
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.sendError(w, nil, InvalidRequest, "missing authorization header")
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			s.sendError(w, nil, InvalidRequest, "invalid authorization header format")
			return
		}

		tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		claims, usedIntrospection, err := s.jwtValidator.ValidateToken(tokenString)
		if err != nil {
			log.Warn().Err(err).Bool("introspectionFallback", usedIntrospection).Msg("Access token validation failed")
			s.sendError(w, nil, InvalidRequest, "invalid token")
			return
		}

		userID = claims.Subject
		expiresAt = claims.ExpiresAt.Time

		if usedIntrospection {
			log.Debug().Str("sub", userID).Msg("Token validated via introspection")
		}

	// Parse JSON-RPC request
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, nil, ParseError, "invalid JSON")
		return
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		s.sendError(w, req.ID, InvalidRequest, "invalid jsonrpc version")
		return
	}

	// Handle initialize specially (creates session)
	if req.Method == "initialize" {
		s.handleInitialize(w, r, &req, userID, tokenString, expiresAt)
		return
	}

	// All other requests require session
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		s.sendError(w, req.ID, InvalidRequest, "missing Mcp-Session-Id header")
		return
	}

	session, err := s.sessionMgr.GetSession(sessionID)
	if err != nil {
		s.sendError(w, req.ID, InvalidRequest, "session not found")
		return
	}

	// Verify session belongs to this user
	if session.UserID != userID {
		s.sendError(w, req.ID, InvalidRequest, "session user mismatch")
		return
	}

	// Update last seen
	s.sessionMgr.UpdateLastSeen(sessionID)

	// Create token provider for this request
	tokenProvider := NewPassthroughTokenProvider(tokenString, expiresAt)

	// Create REST client for this user (passing userID for dev mode debug subject)
	httpClient := s.createRESTClient(tokenProvider, session.UserID)

	// Route request to handler
	s.handleJSONRPC(w, r, &req, session, httpClient)
}

// handleInitialize handles the initialize request
func (s *MCPServer) handleInitialize(w http.ResponseWriter, r *http.Request, req *JSONRPCRequest, userID, jwt string, expiresAt time.Time) {
	// Create new session
	session := s.sessionMgr.CreateSession(userID)

	log.Info().
		Str("sessionId", session.ID).
		Str("userId", userID).
		Msg("Created new MCP session")

	// Return initialize response with session ID in header
	w.Header().Set("Mcp-Session-Id", session.ID)
	w.Header().Set("Content-Type", "application/json")

	// Return server capabilities
	result := map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "ToolBridge MCP Server",
			"version": "0.1.0",
		},
	}

	response := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mustMarshal(result),
	}

	json.NewEncoder(w).Encode(response)
}

// handleJSONRPC routes JSON-RPC requests to appropriate handlers
func (s *MCPServer) handleJSONRPC(w http.ResponseWriter, r *http.Request, req *JSONRPCRequest, session *MCPSession, httpClient *client.HTTPClient) {
	ctx := r.Context()
	logger := log.With().
		Str("sessionId", session.ID).
		Str("userId", session.UserID).
		Str("method", req.Method).
		Logger()

	switch req.Method {
	case "tools/list":
		// Return all registered tools
		toolDescriptors := s.toolRegistry.List()
		result := map[string]interface{}{
			"tools": toolDescriptors,
		}
		s.sendResult(w, req.ID, result)

	case "tools/call":
		// Parse tool call request
		var callReq tools.CallRequest
		if err := json.Unmarshal(req.Params, &callReq); err != nil {
			s.sendError(w, req.ID, InvalidParams, "invalid tool call parameters")
			return
		}

		// Create tool context
		toolCtx := tools.NewToolContext(&logger, session.UserID, session.ID, httpClient, s.sessionMgr)

		// Execute tool
		result, err := s.toolRegistry.Call(ctx, toolCtx, callReq)
		if err != nil {
			// Check if it's a ToolError
			if toolErr, ok := err.(*tools.ToolError); ok {
				code, message, data := toolErr.ToJSONRPCError()
				s.sendError(w, req.ID, code, message, data)
			} else {
				s.sendError(w, req.ID, InternalError, err.Error())
			}
			return
		}

		s.sendResult(w, req.ID, result)

	case "ping":
		// Simple ping response
		result := map[string]interface{}{
			"status": "ok",
		}
		s.sendResult(w, req.ID, result)

	default:
		s.sendError(w, req.ID, MethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleMCPGet handles GET /mcp (SSE stream)
func (s *MCPServer) handleMCPGet(w http.ResponseWriter, r *http.Request) {
	// Validate Origin header (DNS rebinding protection)
	if !s.validateOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	// Get protocol version (defaults to 2025-06-18 if not specified)
	protocolVersion := getProtocolVersion(r)
	if !isSupportedProtocolVersion(protocolVersion) {
		log.Warn().
			Str("version", protocolVersion).
			Msg("Unsupported MCP protocol version")
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}

	log.Debug().
		Str("version", protocolVersion).
		Msg("MCP protocol version validated")

	var userID string

	// Check for dev mode
	if s.config.DevMode {
		// In dev mode, allow X-Debug-Sub header as fallback
		debugSub := r.Header.Get("X-Debug-Sub")
		if debugSub != "" {
			userID = debugSub
			log.Debug().Str("userId", userID).Msg("Using dev mode authentication")
		}
	}

	// If not using dev mode or no debug header, use JWT
	if userID == "" {
		// JWT validation required but validator not configured
		if s.jwtValidator == nil {
			http.Error(w, "authentication not configured", http.StatusInternalServerError)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, usedIntrospection, err := s.jwtValidator.ValidateToken(tokenString)
		if err != nil {
			log.Warn().Err(err).Bool("introspectionFallback", usedIntrospection).Msg("Access token validation failed")
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		userID = claims.Subject
		if usedIntrospection {
			log.Debug().Str("sub", userID).Msg("Token validated via introspection")
		}
	}

	// Get session
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}

	session, err := s.sessionMgr.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Verify session belongs to this user
	if session.UserID != userID {
		http.Error(w, "session user mismatch", http.StatusForbidden)
		return
	}

	// Create SSE stream
	stream, err := NewSSEStream(r.Context(), w, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	log.Info().
		Str("sessionId", sessionID).
		Str("userId", userID).
		Msg("SSE stream established")

	// Keep connection alive
	// In Phase 4, we just keep the connection open without sending messages
	// Phase 5-6 will add actual server-to-client messaging
	<-stream.Done()

	log.Info().
		Str("sessionId", sessionID).
		Msg("SSE stream closed")
}

// handleMCPDelete handles DELETE /mcp (close session)
func (s *MCPServer) handleMCPDelete(w http.ResponseWriter, r *http.Request) {
	// Validate Origin header (DNS rebinding protection)
	if !s.validateOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "missing session ID", http.StatusBadRequest)
		return
	}

	// Optionally validate JWT here too
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") && s.jwtValidator != nil {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, usedIntrospection, err := s.jwtValidator.ValidateToken(tokenString)
		if err == nil {
			if usedIntrospection {
				log.Debug().Str("sub", claims.Subject).Msg("Token validated via introspection")
			}
			// Verify session belongs to this user
			session, err := s.sessionMgr.GetSession(sessionID)
			if err == nil && session.UserID != claims.Subject {
				http.Error(w, "session user mismatch", http.StatusForbidden)
				return
			}
		}
	}

	s.sessionMgr.DeleteSession(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// createRESTClient creates a REST client for the ToolBridge API
func (s *MCPServer) createRESTClient(tokenProvider *PassthroughTokenProvider, userID string) *client.HTTPClient {
	// In dev mode or when Auth0 is not configured, use fallback audience
	var audience string
	var debugSub string

	if s.config.Auth0.SyncAPI != nil {
		audience = s.config.Auth0.SyncAPI.Audience
	} else {
		// Dev mode: use API base URL as fallback audience
		audience = s.config.APIBaseURL
		log.Debug().Str("audience", audience).Msg("Using fallback audience for dev mode")
	}

	// In dev mode, pass userID as debug subject for REST API authentication
	if s.config.DevMode {
		debugSub = userID
		log.Debug().Str("debugSub", debugSub).Msg("Using debug subject for dev mode REST API calls")
	}

	sessionMgr := client.NewSessionManager(s.config.APIBaseURL, tokenProvider, audience)
	return client.NewHTTPClient(s.config.APIBaseURL, tokenProvider, sessionMgr, audience, debugSub)
}

// getProtocolVersion returns the MCP protocol version from the request header
// If the header is missing or empty, defaults to the latest version (2025-06-18)
func getProtocolVersion(r *http.Request) string {
	version := r.Header.Get("Mcp-Protocol-Version")
	if version == "" {
		return "2025-06-18" // Default to latest version
	}
	return version
}

// isSupportedProtocolVersion checks if the given MCP protocol version is supported
// Supported versions:
// - 2024-11-05: Initial MCP specification
// - 2025-03-26: Updated specification with OAuth improvements
// - 2025-06-18: Latest specification with Streamable HTTP transport and RFC 9728 OAuth support
func isSupportedProtocolVersion(version string) bool {
	supportedVersions := []string{
		"2024-11-05",
		"2025-03-26",
		"2025-06-18",
	}

	for _, supported := range supportedVersions {
		if version == supported {
			return true
		}
	}

	return false
}

// validateOrigin checks if the request Origin header is allowed
// This prevents DNS rebinding attacks as required by MCP Streamable HTTP spec
func (s *MCPServer) validateOrigin(r *http.Request) bool {
	// In dev mode, skip origin validation for local development
	if s.config.DevMode {
		return true
	}

	// If no allowed origins configured, allow all (WARNING: only safe for local dev)
	if len(s.config.AllowedOrigins) == 0 {
		log.Warn().Msg("No allowed origins configured - accepting all origins (unsafe for production)")
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		// Requests without Origin header (e.g., from desktop apps like Claude Desktop)
		// are allowed since they're authenticated via OAuth2 and behind HTTPS
		// Browser requests always include Origin header, so we can still validate those
		log.Debug().Msg("Request missing Origin header - allowing (likely desktop app)")
		return true
	}

	// Check if origin is in allowlist
	for _, allowed := range s.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}

	log.Warn().
		Str("origin", origin).
		Strs("allowedOrigins", s.config.AllowedOrigins).
		Msg("Origin not in allowlist")
	return false
}

// Helper functions
func (s *MCPServer) sendError(w http.ResponseWriter, id json.RawMessage, code int, message string, data ...json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors are still HTTP 200

	errObj := &JSONRPCError{
		Code:    code,
		Message: message,
	}

	// Include data if provided
	if len(data) > 0 && data[0] != nil {
		errObj.Data = data[0]
	}

	response := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   errObj,
	}

	json.NewEncoder(w).Encode(response)
}

func (s *MCPServer) sendResult(w http.ResponseWriter, id json.RawMessage, result interface{}) {
	w.Header().Set("Content-Type", "application/json")

	response := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  mustMarshal(result),
	}

	json.NewEncoder(w).Encode(response)
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// handleHealth handles GET /healthz (liveness probe)
// Returns 200 OK if process is alive, no dependencies checked
func (s *MCPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// handleReady handles GET /readyz (readiness probe)
// Returns 200 OK if server is ready to accept traffic
// Checks JWT validator readiness if not in dev mode
func (s *MCPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// In dev mode, always ready (no JWT validator)
	if s.config.DevMode {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ready",
			"devMode": true,
		})
		return
	}

	// Check JWT validator readiness
	if s.jwtValidator == nil || !s.jwtValidator.Ready() {
		log.Debug().Msg("Readiness check failed: JWT validator not ready")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not ready",
			"reason": "JWT validator not initialized",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ready",
		"jwtValidator": "ready",
	})
}
