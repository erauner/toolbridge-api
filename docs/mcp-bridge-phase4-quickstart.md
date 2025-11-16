# MCP Bridge Phase 4 - Quick Start Guide

**Goal**: Implement Streamable HTTP MCP server for remote access via Claude Desktop/Web.

## Architecture Shift

**OLD (stdio)**: Local process, Device Code Flow
**NEW (Streamable HTTP)**: Remote server, browser OAuth, JWT validation

```
Claude → https://api.toolbridge.com/mcp
  ↓ User authenticates in browser (Auth0)
  ↓ HTTP + JWT
MCP Server (Phase 4)
  ↓ Uses Phase 3 REST client
ToolBridge API
```

## Prerequisites
- Phase 1 complete (config system)
- Phase 2 complete (Auth0 broker) - NOT USED in Streamable HTTP!
- Phase 3 complete (REST client) ✅

## Quick Reference
- **MCP TypeScript SDK**: https://github.com/modelcontextprotocol/typescript-sdk/blob/main/src/server/streamableHttp.ts
- **Auth0 JWT Validation**: https://auth0.com/docs/secure/tokens/json-web-tokens/validate-json-web-tokens
- **JSON-RPC 2.0**: https://www.jsonrpc.org/specification

## Implementation Checklist

### 1. Package Structure
```bash
mkdir -p internal/mcpserver/server
```

Create files:
- [ ] `internal/mcpserver/server/server.go` - Main HTTP server
- [ ] `internal/mcpserver/server/jwt.go` - JWT validation
- [ ] `internal/mcpserver/server/session.go` - MCP session management
- [ ] `internal/mcpserver/server/jsonrpc.go` - JSON-RPC parsing
- [ ] `internal/mcpserver/server/sse.go` - SSE streaming
- [ ] `internal/mcpserver/server/passthrough_token.go` - TokenProvider impl
- [ ] `internal/mcpserver/server/oauth_metadata.go` - OAuth discovery
- [ ] `internal/mcpserver/server/handler.go` - Request routing

### 2. Endpoints to Implement

**POST /mcp** - JSON-RPC requests
- Extract JWT from `Authorization: Bearer <token>`
- Validate JWT (RS256, check issuer/audience/expiry)
- Handle `initialize` → create session, return `Mcp-Session-Id` header
- Handle other methods → validate session, route to tool handlers

**GET /mcp** - SSE stream
- Validate JWT and session
- Set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`
- Stream JSON-RPC messages as `event: message` with incremental IDs

**DELETE /mcp** - Close session
- Validate session
- Clean up resources
- Return 204 No Content

**GET /.well-known/oauth-authorization-server** - OAuth metadata
- Return Auth0 configuration (issuer, endpoints, JWKS URI)

### 3. JWT Validation (`jwt.go`)

**Key behaviors**:
- Fetch JWKS from `https://<auth0-domain>/.well-known/jwks.json`
- Cache public keys (refresh every 1 hour)
- Validate RS256 signature
- Check issuer: `https://<auth0-domain>/`
- Check audience: sync API audience from config
- Extract user ID from `sub` claim

**Dependencies**:
```bash
go get github.com/golang-jwt/jwt/v5
```

**Test cases**:
- Valid token passes
- Expired token fails
- Wrong audience fails
- Invalid signature fails
- JWKS caching works

### 4. Session Management (`session.go`)

**Key behaviors**:
- Generate UUID for session ID
- Store session → user mapping
- Track last seen time
- Cleanup expired sessions (24h TTL)
- Thread-safe with `sync.RWMutex`

**Sessions vs REST API sessions**:
- MCP session: Tracks MCP connection (this phase)
- REST API session: Used by Phase 3 client (existing)
- Different purposes, both needed!

### 5. PassthroughTokenProvider (`passthrough_token.go`)

**Purpose**: Implement `TokenProvider` interface from Phase 3

```go
type PassthroughTokenProvider struct {
    jwt       string
    expiresAt time.Time
}

func (p *PassthroughTokenProvider) GetToken(ctx, audience, scope string, interactive bool) (*auth.TokenResult, error) {
    return &auth.TokenResult{
        AccessToken: p.jwt,
        ExpiresAt:   p.expiresAt,
        TokenType:   "Bearer",
    }, nil
}
```

**Why needed**: Phase 3 REST client expects `TokenProvider`. We provide JWT from incoming request.

### 6. JSON-RPC Parsing (`jsonrpc.go`)

**Core types**:
```go
type JSONRPCRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}
```

**Error codes** (from spec):
- `-32700`: Parse error
- `-32600`: Invalid request
- `-32601`: Method not found
- `-32602`: Invalid params
- `-32603`: Internal error

### 7. SSE Streaming (`sse.go`)

**Key behaviors**:
```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache, no-transform")
w.Header().Set("X-Accel-Buffering", "no") // Nginx

fmt.Fprintf(w, "event: message\n")
fmt.Fprintf(w, "id: %d\n", eventID)
fmt.Fprintf(w, "data: %s\n\n", jsonData)
w.(http.Flusher).Flush()
```

**Use case**: Server-to-client notifications (not heavily used in initial implementation)

### 8. Main Server (`server.go`)

**Initialize**:
```go
func NewMCPServer(cfg *config.Config) *MCPServer {
    return &MCPServer{
        jwtValidator: NewJWTValidator(cfg.Auth0.Domain, cfg.Auth0.SyncAPI.Audience),
        sessionMgr:   NewSessionManager(24 * time.Hour),
        toolRegistry: NewToolRegistry(), // Phase 5
    }
}
```

**Handle POST /mcp**:
1. Extract JWT from `Authorization` header
2. Validate JWT → get user ID
3. Parse JSON-RPC request
4. If `method == "initialize"`:
   - Create session
   - Return session ID in `Mcp-Session-Id` header
   - Return capabilities
5. Else:
   - Validate `Mcp-Session-Id` header
   - Create `PassthroughTokenProvider` with JWT
   - Create REST client (Phase 3)
   - Route to tool handler (Phase 5)

### 9. Integration with main.go

```go
func run(ctx context.Context, cfg *config.Config) error {
    // Create MCP server
    mcpServer := server.NewMCPServer(cfg)

    // Start HTTP server
    go func() {
        addr := ":8082" // Different from REST API (8081)
        if err := mcpServer.Start(addr); err != nil {
            log.Error().Err(err).Msg("MCP server failed")
        }
    }()

    // Wait for shutdown
    <-ctx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    return mcpServer.Shutdown(shutdownCtx)
}
```

### 10. Configuration

**No changes needed!** Use existing Auth0 config from Phase 1.

**Key config values**:
- `cfg.Auth0.Domain` - Used for JWKS URL and OAuth metadata
- `cfg.Auth0.SyncAPI.Audience` - Validated in JWT
- `cfg.APIBaseURL` - Passed to Phase 3 REST client

### 11. OAuth Metadata Endpoint

```go
func (s *MCPServer) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
    domain := s.config.Auth0.Domain
    metadata := map[string]interface{}{
        "issuer":                fmt.Sprintf("https://%s/", domain),
        "authorization_endpoint": fmt.Sprintf("https://%s/authorize", domain),
        "token_endpoint":        fmt.Sprintf("https://%s/oauth/token", domain),
        "jwks_uri":              fmt.Sprintf("https://%s/.well-known/jwks.json", domain),
        // ... (see detailed spec for full list)
    }
    json.NewEncoder(w).Encode(metadata)
}
```

**Why needed**: Claude discovers OAuth endpoints from this URL.

## Testing Checklist

### Unit Tests
- [ ] JWT validation (valid, expired, wrong audience, invalid signature)
- [ ] Session management (create, get, cleanup, thread safety)
- [ ] JSON-RPC parsing (valid, invalid, notifications)
- [ ] SSE formatting

### Integration Tests
- [ ] Initialize creates session
- [ ] Session ID returned in header
- [ ] Subsequent requests require session
- [ ] Invalid session returns 404
- [ ] OAuth metadata endpoint works

### Manual Testing
```bash
# 1. Get real JWT from Auth0
# Use Auth0 dashboard or curl to get a token for your audience

# 2. Test initialize
curl -X POST http://localhost:8082/mcp \
  -H "Authorization: Bearer <jwt>" \
  -H "Mcp-Protocol-Version: 2025-03-26" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}'

# 3. Check Mcp-Session-Id in response headers

# 4. Test with session
curl -X POST http://localhost:8082/mcp \
  -H "Authorization: Bearer <jwt>" \
  -H "Mcp-Session-Id: <session-id>" \
  -H "Mcp-Protocol-Version: 2025-03-26" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

## Success Criteria

- [ ] Server starts on port 8082
- [ ] OAuth metadata endpoint returns valid JSON
- [ ] JWT validation works (valid token accepted, invalid rejected)
- [ ] Initialize creates session and returns session ID
- [ ] Subsequent requests validate session
- [ ] PassthroughTokenProvider integrates with Phase 3 REST client
- [ ] SSE endpoint establishes connection
- [ ] DELETE endpoint closes session
- [ ] No race conditions (run with `-race`)

## Common Issues

**"streaming not supported"**: Handler doesn't implement `http.Flusher`
- Solution: Use standard `http.ResponseWriter` (it implements Flusher)

**"session not found"**: Session ID not being passed correctly
- Solution: Check `Mcp-Session-Id` header in request

**JWT validation fails**: Wrong audience or issuer
- Solution: Verify `cfg.Auth0.SyncAPI.Audience` matches JWT `aud` claim

**JWKS fetch fails**: Network or domain issue
- Solution: Verify `cfg.Auth0.Domain` is correct and accessible

## Time Estimate
6-8 hours

## Dependencies
```bash
go get github.com/golang-jwt/jwt/v5
go get github.com/google/uuid  # Already from Phase 3
```

## Next Steps

**Phase 5-6**: Implement MCP tools
- Tool registry
- Notes tools (create, list, get, update, delete, pin)
- Tasks tools (create, complete, reopen)
- Comments, chats, chat messages tools
- Context attachment tools

Tools will use the REST client from Phase 3 via the PassthroughTokenProvider.

## Key Architectural Points

1. **No Device Code Flow**: User authenticates in browser, not in MCP server
2. **JWT passthrough**: MCP server validates JWT, then passes it to REST API
3. **Two session types**:
   - MCP session (this phase) - tracks MCP connection
   - REST API session (Phase 3) - used by REST client
4. **Phase 3 reuse**: REST client works unchanged, just different TokenProvider
5. **Remote access**: Deploy anywhere, users connect via URL
