# MCP Bridge Phase 4 - Streamable HTTP Server Implementation

**Objective**: Implement a remote MCP server using Streamable HTTP transport that allows Claude Desktop/Web to connect via URL with OAuth authentication.

## Architecture Overview

```
Claude Desktop/Web
    ↓ User adds: https://api.toolbridge.com/mcp
    ↓ OAuth flow (Auth0) - user authenticates in browser
    ↓ HTTP requests with JWT Bearer token
    ↓
Remote MCP Server (Go - Phase 4)
    POST /mcp        ← JSON-RPC messages from Claude
    GET /mcp         ← SSE stream for server→client messages
    DELETE /mcp      ← Close session
    /.well-known/... ← OAuth metadata discovery
    ↓ Uses REST client from Phase 3
    ↓
ToolBridge REST API (existing - port 8081)
    ↓
PostgreSQL
```

## Key Differences from stdio MCP

### OLD (stdio - what we pivoted away from):
- Claude Desktop spawns local process via stdio
- MCP server uses Device Code Flow to get JWT
- Process per connection
- Local only

### NEW (Streamable HTTP - what we're building):
- Claude connects to remote URL
- User authenticates via OAuth in browser
- MCP server validates JWT from Claude
- Single server handles multiple clients
- Cloud-native, can be deployed anywhere

## Transport Protocol: Streamable HTTP

Based on MCP specification and TypeScript SDK implementation.

### Endpoints

**POST /mcp**
- Receives JSON-RPC requests from Claude
- Required headers:
  - `Authorization: Bearer <jwt>` - Auth0 JWT token
  - `Mcp-Session-Id: <uuid>` - Session identifier (after initialization)
  - `Mcp-Protocol-Version: 2025-03-26` - Protocol version
  - `Accept: application/json, text/event-stream` - Must accept both
  - `Content-Type: application/json`
- Request body: JSON-RPC 2.0 message
  ```json
  {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "toolbridge__notes_create",
      "arguments": {"title": "Test"}
    }
  }
  ```
- Response: JSON-RPC 2.0 result
  ```json
  {
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
      "content": [{"type": "text", "text": "Note created"}]
    }
  }
  ```

**GET /mcp**
- Establishes SSE stream for server-to-client messages
- Required headers:
  - `Authorization: Bearer <jwt>`
  - `Mcp-Session-Id: <uuid>` - Session identifier
  - `Mcp-Protocol-Version: 2025-03-26`
  - `Accept: text/event-stream`
  - `Last-Event-ID: <id>` - Optional, for resumption
- Response headers:
  - `Content-Type: text/event-stream`
  - `Cache-Control: no-cache, no-transform`
- Response body: SSE stream
  ```
  event: message
  id: 1
  data: {"jsonrpc":"2.0","method":"notifications/message","params":{...}}

  event: message
  id: 2
  data: {"jsonrpc":"2.0","id":5,"result":{...}}
  ```

**DELETE /mcp**
- Closes a session and cleans up resources
- Required headers:
  - `Authorization: Bearer <jwt>`
  - `Mcp-Session-Id: <uuid>`
- Response: 200 OK or 204 No Content

### Session Management

**Session Lifecycle:**
1. Client sends `initialize` request via POST
2. Server generates `Mcp-Session-Id` (UUID)
3. Server includes `Mcp-Session-Id` in response headers
4. Client includes `Mcp-Session-Id` in all subsequent requests
5. Session remains active until DELETE or timeout

**Session Validation:**
- Server MUST validate `Mcp-Session-Id` on all requests (except initial `initialize`)
- Invalid session → 404 Not Found
- Missing session ID after initialization → 400 Bad Request

**Concurrent Sessions:**
- Multiple users can connect simultaneously
- Each user has their own session(s)
- Session ID ties requests together

### Protocol Version

Server MUST validate `Mcp-Protocol-Version` header:
- Supported versions: `2025-03-26` (and potentially `2024-11-05` for compatibility)
- Unsupported version → 400 Bad Request with error details

### OAuth Discovery

**/.well-known/oauth-authorization-server**

Claude uses this endpoint to discover OAuth configuration.

```json
{
  "issuer": "https://your-tenant.us.auth0.com",
  "authorization_endpoint": "https://your-tenant.us.auth0.com/authorize",
  "token_endpoint": "https://your-tenant.us.auth0.com/oauth/token",
  "jwks_uri": "https://your-tenant.us.auth0.com/.well-known/jwks.json",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "subject_types_supported": ["public"],
  "id_token_signing_alg_values_supported": ["RS256"],
  "scopes_supported": ["openid", "profile", "email", "offline_access"],
  "token_endpoint_auth_methods_supported": ["client_secret_basic", "client_secret_post"]
}
```

**Claude's OAuth Flow:**
1. Claude fetches `/.well-known/oauth-authorization-server`
2. Claude redirects user to `authorization_endpoint`
3. User authenticates with Auth0 in browser
4. Auth0 redirects back to Claude with authorization code
5. Claude exchanges code for JWT at `token_endpoint`
6. Claude sends JWT in `Authorization` header to MCP server

## Implementation Plan

### Package Structure

```
internal/mcpserver/
├── config/          # Phase 1 - Configuration
├── auth/            # Phase 2 - Auth0 token broker (NOT USED in Streamable HTTP)
├── client/          # Phase 3 - REST client ✅
├── server/          # Phase 4 - NEW
│   ├── server.go              # Main HTTP server
│   ├── session.go             # Session management
│   ├── jsonrpc.go             # JSON-RPC parsing
│   ├── sse.go                 # SSE streaming
│   ├── jwt.go                 # JWT validation
│   ├── oauth_metadata.go      # OAuth discovery endpoints
│   ├── passthrough_token.go   # TokenProvider for Phase 3 client
│   └── handler.go             # Request routing
├── tools/           # Phase 5-6 - Tool implementations
│   ├── registry.go
│   ├── notes.go
│   ├── tasks.go
│   └── ...
```

### Phase 4 Components

#### 1. JWT Validation (`jwt.go`)

**Purpose**: Validate Auth0 JWT tokens from incoming requests

**Key behaviors:**
- Fetch Auth0 public keys from JWKS endpoint
- Validate JWT signature (RS256)
- Validate issuer, audience, expiration
- Extract user ID from `sub` claim
- Cache JWKS keys (refresh periodically)

**Reference**: Auth0 JWT validation best practices

```go
package server

import (
    "context"
    "crypto/rsa"
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"

    "github.com/golang-jwt/jwt/v5"
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
    switch aud := claims.Audience.(type) {
    case string:
        validAudience = aud == v.audience
    case []interface{}:
        for _, a := range aud {
            if a == v.audience {
                validAudience = true
                break
            }
        }
    }

    if !validAudience {
        return nil, fmt.Errorf("invalid audience")
    }

    return &claims, nil
}

// Claims represents JWT claims
type Claims struct {
    jwt.RegisteredClaims
    Scope string `json:"scope,omitempty"`
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
            Kid string   `json:"kid"`
            Kty string   `json:"kty"`
            Use string   `json:"use"`
            N   string   `json:"n"`
            E   string   `json:"e"`
            Alg string   `json:"alg"`
        } `json:"keys"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
        return nil, fmt.Errorf("failed to decode JWKS: %w", err)
    }

    // Parse all keys and cache
    for _, key := range jwks.Keys {
        if key.Kty != "RSA" || key.Use != "sig" {
            continue
        }

        publicKey, err := parseRSAPublicKey(key.N, key.E)
        if err != nil {
            continue
        }

        v.publicKeys[key.Kid] = publicKey
    }

    v.lastFetch = time.Now()

    // Return requested key
    if key, exists := v.publicKeys[kid]; exists {
        return key, nil
    }

    return nil, fmt.Errorf("key ID %s not found in JWKS", kid)
}

// parseRSAPublicKey parses RSA public key from JWKS n and e values
func parseRSAPublicKey(n, e string) (*rsa.PublicKey, error) {
    // Implementation: decode base64url n and e, construct rsa.PublicKey
    // See: github.com/golang-jwt/jwt examples
    // This is standard JWT key parsing logic
    return nil, fmt.Errorf("not implemented - use jwt library helpers")
}
```

#### 2. Session Manager (`session.go`)

**Purpose**: Track MCP sessions (different from REST API sessions!)

**Key behaviors:**
- Generate session IDs on initialization
- Store session → user mapping
- Validate session IDs on subsequent requests
- Clean up expired sessions
- Thread-safe concurrent access

```go
package server

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/google/uuid"
)

// MCPSession represents an active MCP client connection
type MCPSession struct {
    ID        string
    UserID    string // From JWT sub claim
    CreatedAt time.Time
    LastSeen  time.Time
    // SSE connection tracking would go here
}

// SessionManager manages MCP sessions
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*MCPSession // sessionID -> session
    ttl      time.Duration
}

// NewSessionManager creates a new session manager
func NewSessionManager(ttl time.Duration) *SessionManager {
    mgr := &SessionManager{
        sessions: make(map[string]*MCPSession),
        ttl:      ttl,
    }

    // Start cleanup goroutine
    go mgr.cleanupExpired()

    return mgr
}

// CreateSession creates a new MCP session for a user
func (sm *SessionManager) CreateSession(userID string) *MCPSession {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    session := &MCPSession{
        ID:        uuid.New().String(),
        UserID:    userID,
        CreatedAt: time.Now(),
        LastSeen:  time.Now(),
    }

    sm.sessions[session.ID] = session
    return session
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) (*MCPSession, error) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    session, exists := sm.sessions[sessionID]
    if !exists {
        return nil, fmt.Errorf("session not found")
    }

    return session, nil
}

// UpdateLastSeen updates the last seen time for a session
func (sm *SessionManager) UpdateLastSeen(sessionID string) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    if session, exists := sm.sessions[sessionID]; exists {
        session.LastSeen = time.Now()
    }
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(sessionID string) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    delete(sm.sessions, sessionID)
}

// cleanupExpired removes expired sessions
func (sm *SessionManager) cleanupExpired() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        sm.mu.Lock()
        now := time.Now()
        for id, session := range sm.sessions {
            if now.Sub(session.LastSeen) > sm.ttl {
                delete(sm.sessions, id)
            }
        }
        sm.mu.Unlock()
    }
}
```

#### 3. JSON-RPC Parser (`jsonrpc.go`)

**Purpose**: Parse and validate JSON-RPC 2.0 messages

**Reference**: JSON-RPC 2.0 specification

```go
package server

import "encoding/json"

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}

// MCP-specific error codes (from MCP specification)
const (
    ParseError     = -32700
    InvalidRequest = -32600
    MethodNotFound = -32601
    InvalidParams  = -32602
    InternalError  = -32603
)

// IsNotification returns true if this is a notification (no id)
func (r *JSONRPCRequest) IsNotification() bool {
    return len(r.ID) == 0
}
```

#### 4. SSE Streaming (`sse.go`)

**Purpose**: Handle Server-Sent Events for server→client messages

```go
package server

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
)

// SSEStream manages an SSE connection for a session
type SSEStream struct {
    mu          sync.Mutex
    w           http.ResponseWriter
    flusher     http.Flusher
    eventID     int
    sessionID   string
    ctx         context.Context
    cancel      context.CancelFunc
}

// NewSSEStream creates a new SSE stream
func NewSSEStream(w http.ResponseWriter, sessionID string) (*SSEStream, error) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        return nil, fmt.Errorf("streaming not supported")
    }

    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache, no-transform")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

    ctx, cancel := context.WithCancel(context.Background())

    return &SSEStream{
        w:         w,
        flusher:   flusher,
        sessionID: sessionID,
        ctx:       ctx,
        cancel:    cancel,
    }, nil
}

// SendMessage sends a JSON-RPC message via SSE
func (s *SSEStream) SendMessage(msg interface{}) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.eventID++

    // Marshal message to JSON
    data, err := json.Marshal(msg)
    if err != nil {
        return err
    }

    // Write SSE event
    fmt.Fprintf(s.w, "event: message\n")
    fmt.Fprintf(s.w, "id: %d\n", s.eventID)
    fmt.Fprintf(s.w, "data: %s\n\n", data)

    s.flusher.Flush()
    return nil
}

// Close closes the SSE stream
func (s *SSEStream) Close() {
    s.cancel()
}
```

#### 5. PassthroughTokenProvider (`passthrough_token.go`)

**Purpose**: Implement TokenProvider interface using JWT from request

```go
package server

import (
    "context"
    "time"

    "github.com/erauner12/toolbridge-api/internal/mcpserver/auth"
)

// PassthroughTokenProvider provides the JWT from the incoming HTTP request
// This is used in Streamable HTTP mode where Claude provides the JWT
type PassthroughTokenProvider struct {
    jwt       string
    expiresAt time.Time
}

// NewPassthroughTokenProvider creates a token provider from a JWT
func NewPassthroughTokenProvider(jwt string, expiresAt time.Time) *PassthroughTokenProvider {
    return &PassthroughTokenProvider{
        jwt:       jwt,
        expiresAt: expiresAt,
    }
}

// GetToken returns the JWT from the request
func (p *PassthroughTokenProvider) GetToken(ctx context.Context, audience, scope string, interactive bool) (*auth.TokenResult, error) {
    return &auth.TokenResult{
        AccessToken: p.jwt,
        ExpiresAt:   p.expiresAt,
        TokenType:   "Bearer",
    }, nil
}

// InvalidateToken is a no-op for passthrough provider
func (p *PassthroughTokenProvider) InvalidateToken(audience, scope string) {
    // No-op: token comes from Claude, we can't invalidate it
}
```

#### 6. Main Server (`server.go`)

**Purpose**: HTTP server with routing and middleware

```go
package server

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/erauner12/toolbridge-api/internal/mcpserver/client"
    "github.com/erauner12/toolbridge-api/internal/mcpserver/config"
    "github.com/rs/zerolog/log"
)

// MCPServer is the main Streamable HTTP MCP server
type MCPServer struct {
    config         *config.Config
    httpServer     *http.Server
    jwtValidator   *JWTValidator
    sessionMgr     *SessionManager
    toolRegistry   *ToolRegistry
}

// NewMCPServer creates a new MCP server
func NewMCPServer(cfg *config.Config) *MCPServer {
    jwtValidator := NewJWTValidator(cfg.Auth0.Domain, cfg.Auth0.SyncAPI.Audience)
    sessionMgr := NewSessionManager(24 * time.Hour)

    return &MCPServer{
        config:       cfg,
        jwtValidator: jwtValidator,
        sessionMgr:   sessionMgr,
        toolRegistry: NewToolRegistry(),
    }
}

// Start starts the HTTP server
func (s *MCPServer) Start(addr string) error {
    mux := http.NewServeMux()

    // MCP endpoints
    mux.HandleFunc("POST /mcp", s.handleMCPPost)
    mux.HandleFunc("GET /mcp", s.handleMCPGet)
    mux.HandleFunc("DELETE /mcp", s.handleMCPDelete)

    // OAuth discovery
    mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleOAuthMetadata)

    s.httpServer = &http.Server{
        Addr:         addr,
        Handler:      mux,
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 30 * time.Second,
    }

    log.Info().Str("addr", addr).Msg("Starting MCP server")
    return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *MCPServer) Shutdown(ctx context.Context) error {
    return s.httpServer.Shutdown(ctx)
}

// handleMCPPost handles POST /mcp (JSON-RPC requests)
func (s *MCPServer) handleMCPPost(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Extract and validate JWT
    authHeader := r.Header.Get("Authorization")
    if authHeader == "" {
        s.sendError(w, nil, InvalidRequest, "missing authorization header")
        return
    }

    tokenString := authHeader[len("Bearer "):]
    claims, err := s.jwtValidator.ValidateToken(tokenString)
    if err != nil {
        log.Warn().Err(err).Msg("JWT validation failed")
        s.sendError(w, nil, InvalidRequest, "invalid token")
        return
    }

    userID := claims.Subject

    // Parse JSON-RPC request
    var req JSONRPCRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        s.sendError(w, nil, ParseError, "invalid JSON")
        return
    }

    // Handle initialize specially (creates session)
    if req.Method == "initialize" {
        s.handleInitialize(w, r, &req, userID, tokenString, claims.ExpiresAt.Time)
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
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }

    // Update last seen
    s.sessionMgr.UpdateLastSeen(sessionID)

    // Create token provider for this request
    tokenProvider := NewPassthroughTokenProvider(tokenString, claims.ExpiresAt.Time)

    // Create REST client for this user
    httpClient := s.createRESTClient(tokenProvider)

    // Route request to handler
    s.handleJSONRPC(w, &req, session, httpClient)
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

// handleMCPGet handles GET /mcp (SSE stream)
func (s *MCPServer) handleMCPGet(w http.ResponseWriter, r *http.Request) {
    // Similar to POST: validate JWT, get session
    // Create SSE stream
    // Keep connection open
    // Stream messages as they arrive
}

// handleMCPDelete handles DELETE /mcp (close session)
func (s *MCPServer) handleMCPDelete(w http.ResponseWriter, r *http.Request) {
    sessionID := r.Header.Get("Mcp-Session-Id")
    if sessionID == "" {
        http.Error(w, "missing session ID", http.StatusBadRequest)
        return
    }

    s.sessionMgr.DeleteSession(sessionID)
    w.WriteHeader(http.StatusNoContent)
}

// createRESTClient creates a REST client for the ToolBridge API
func (s *MCPServer) createRESTClient(tokenProvider *PassthroughTokenProvider) *client.HTTPClient {
    audience := s.config.Auth0.SyncAPI.Audience
    sessionMgr := client.NewSessionManager(s.config.APIBaseURL, tokenProvider, audience)
    return client.NewHTTPClient(s.config.APIBaseURL, tokenProvider, sessionMgr, audience)
}

// Helper functions
func (s *MCPServer) sendError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK) // JSON-RPC errors are still HTTP 200

    response := JSONRPCResponse{
        JSONRPC: "2.0",
        ID:      id,
        Error: &JSONRPCError{
            Code:    code,
            Message: message,
        },
    }

    json.NewEncoder(w).Encode(response)
}

func mustMarshal(v interface{}) json.RawMessage {
    data, _ := json.Marshal(v)
    return data
}
```

#### 7. OAuth Metadata (`oauth_metadata.go`)

**Purpose**: Serve OAuth discovery endpoints for Claude

```go
package server

import (
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/erauner12/toolbridge-api/internal/mcpserver/config"
)

// handleOAuthMetadata serves the OAuth authorization server metadata
// Reference: RFC 8414 (OAuth 2.0 Authorization Server Metadata)
func (s *MCPServer) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
    domain := s.config.Auth0.Domain
    issuer := fmt.Sprintf("https://%s/", domain)

    metadata := map[string]interface{}{
        "issuer":                issuer,
        "authorization_endpoint": fmt.Sprintf("https://%s/authorize", domain),
        "token_endpoint":        fmt.Sprintf("https://%s/oauth/token", domain),
        "jwks_uri":              fmt.Sprintf("https://%s/.well-known/jwks.json", domain),
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
```

## Integration with main.go

```go
func run(ctx context.Context, cfg *config.Config) error {
    // Phase 4: Start MCP server in Streamable HTTP mode
    mcpServer := server.NewMCPServer(cfg)

    // Start server in background
    go func() {
        addr := ":8082" // Different port from REST API
        if err := mcpServer.Start(addr); err != nil && err != http.ErrServerClosed {
            log.Error().Err(err).Msg("MCP server failed")
        }
    }()

    log.Info().Msg("MCP server started in Streamable HTTP mode")

    // Wait for shutdown signal
    <-ctx.Done()

    // Graceful shutdown
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    return mcpServer.Shutdown(shutdownCtx)
}
```

## Testing Strategy

### Unit Tests

**jwt_test.go**: JWT validation
- Valid token passes
- Expired token fails
- Wrong audience fails
- Wrong issuer fails
- Invalid signature fails
- JWKS caching works

**session_test.go**: Session management
- Create session generates UUID
- Get session retrieves correct session
- Invalid session returns error
- Session cleanup removes expired sessions
- Thread safety under concurrent access

**jsonrpc_test.go**: JSON-RPC parsing
- Valid request parses correctly
- Invalid JSON returns parse error
- Notification detection works

**sse_test.go**: SSE streaming
- Messages are formatted correctly
- Event IDs increment
- Flush is called after each message

### Integration Tests

**mcp_server_test.go**: Full flow
- Initialize creates session
- Session ID is returned in header
- Subsequent requests require session ID
- Invalid session returns 404
- JWT validation enforced
- OAuth metadata endpoint works

### Manual Testing

```bash
# 1. Start REST API
make dev

# 2. Start MCP server
go run cmd/mcpbridge/main.go --config config/auth0_prod.json

# 3. Test OAuth metadata
curl http://localhost:8082/.well-known/oauth-authorization-server

# 4. Test initialize (need real JWT from Auth0)
curl -X POST http://localhost:8082/mcp \
  -H "Authorization: Bearer <your-jwt>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-03-26" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-03-26",
      "capabilities": {},
      "clientInfo": {"name": "test-client", "version": "1.0"}
    }
  }'

# 5. Extract Mcp-Session-Id from response headers
# 6. Test tools/list (requires session)
curl -X POST http://localhost:8082/mcp \
  -H "Authorization: Bearer <your-jwt>" \
  -H "Mcp-Session-Id: <session-id>" \
  -H "Mcp-Protocol-Version: 2025-03-26" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
  }'
```

## Success Criteria

- [ ] HTTP server starts on configured port
- [ ] POST /mcp accepts JSON-RPC requests
- [ ] GET /mcp establishes SSE stream
- [ ] DELETE /mcp closes sessions
- [ ] JWT validation enforces Auth0 authentication
- [ ] Session management tracks multiple users
- [ ] OAuth metadata endpoint returns valid configuration
- [ ] PassthroughTokenProvider works with Phase 3 REST client
- [ ] Initialize creates session and returns session ID
- [ ] Subsequent requests validate session ID
- [ ] Invalid session returns 404
- [ ] Missing session ID returns 400
- [ ] Protocol version validation works
- [ ] Graceful shutdown closes all sessions
- [ ] No race conditions under concurrent load

## Next Steps (Phase 5-6)

Phase 5-6 will implement the actual MCP tools (notes, tasks, comments, etc.) using the REST client from Phase 3. The tool registry will dispatch tool calls to the appropriate handlers.

## References

- MCP Streamable HTTP spec: https://spec.modelcontextprotocol.io/specification/2025-03-26/basic/transports/
- MCP TypeScript SDK: https://github.com/modelcontextprotocol/typescript-sdk/blob/main/src/server/streamableHttp.ts
- Auth0 JWT validation: https://auth0.com/docs/secure/tokens/json-web-tokens/validate-json-web-tokens
- JSON-RPC 2.0: https://www.jsonrpc.org/specification
- SSE specification: https://html.spec.whatwg.org/multipage/server-sent-events.html
- Phase 3 REST client: internal/mcpserver/client/
