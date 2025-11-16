# MCP Bridge Architecture Pivot: stdio → Streamable HTTP

**Date**: 2025-11-16
**Status**: Phase 4 implementation in progress

## Summary

We pivoted from building a **local stdio-based MCP server** to a **remote Streamable HTTP MCP server**. This is a significant architectural improvement that better leverages your existing Auth0 infrastructure and enables cloud deployment.

## What Changed

### OLD Architecture (stdio - what we pivoted away from)

```
Claude Desktop (local)
  ↓ Spawns process via stdio
  ↓ JSON-RPC over stdin/stdout
MCP Bridge (local process)
  ↓ Device Code Flow → get JWT
  ↓ Uses JWT to call API
ToolBridge REST API
  ↓
PostgreSQL
```

**Characteristics:**
- **Local only**: Process runs on user's machine
- **One user per process**: Claude spawns new process for each session
- **Device Code Flow**: MCP server shows URL + code, user authenticates
- **Manual installation**: User must download binary and configure path
- **stdio transport**: JSON-RPC over stdin/stdout

### NEW Architecture (Streamable HTTP - what we're building)

```
Claude Desktop/Web (anywhere)
  ↓ User adds URL: https://api.toolbridge.com/mcp
  ↓ OAuth in browser → get JWT
  ↓ HTTP requests with JWT
MCP Server (remote - port 8082)
  POST /mcp        ← JSON-RPC requests
  GET /mcp         ← SSE stream
  DELETE /mcp      ← Close session
  /.well-known/... ← OAuth discovery
  ↓ Validates JWT (RS256)
  ↓ Reuses JWT to call API
ToolBridge REST API (port 8081)
  ↓
PostgreSQL
```

**Characteristics:**
- **Remote access**: Deploy anywhere (Fly.io, Railway, etc.)
- **Multi-client**: Single server handles many users concurrently
- **Browser OAuth**: Users authenticate via standard OAuth flow
- **Zero installation**: Just add URL in Claude settings
- **Streamable HTTP transport**: POST for requests, GET for SSE streaming
- **Cloud-native**: Stateless, horizontally scalable

## Key Benefits

### 1. Better User Experience
- **No installation**: Users just add a URL
- **Browser authentication**: Familiar OAuth flow (no copy-paste codes)
- **Works anywhere**: Desktop, web, mobile (eventually)
- **Always updated**: Server updates apply to all users immediately

### 2. Better Operations
- **Cloud deployment**: Deploy to Fly.io, Railway, Heroku, etc.
- **Multi-tenancy**: One server, many users
- **Monitoring**: Standard HTTP metrics, logs, traces
- **Scaling**: Horizontal scaling with load balancer

### 3. Better Architecture
- **Leverages existing Auth0**: Same OAuth provider as your app
- **Reuses Phase 3 REST client**: No duplicate code
- **Standard OAuth**: Industry-standard browser-based flow
- **Standard transport**: HTTP/SSE instead of stdio

## What Stayed the Same

### Phase 3 REST Client ✅ **No changes needed!**

Your Phase 3 implementation is **perfectly designed** for this pivot:

- **HTTPClient**: Still calls ToolBridge REST API
- **SessionManager**: Still manages REST API sessions
- **EntityClient**: Still does CRUD operations
- **TokenProvider abstraction**: Key design that enables flexibility

The only difference is the `TokenProvider` implementation:
- **OLD**: `DeviceCodeFlowProvider` - does Device Code Flow
- **NEW**: `PassthroughTokenProvider` - uses JWT from incoming request

```go
// PassthroughTokenProvider provides JWT from incoming HTTP request
type PassthroughTokenProvider struct {
    jwt string
}

func (p *PassthroughTokenProvider) GetToken(...) (*auth.TokenResult, error) {
    return &auth.TokenResult{
        AccessToken: p.jwt,
        ExpiresAt:   time.Now().Add(1 * time.Hour),
        TokenType:   "Bearer",
    }, nil
}
```

That's it! Phase 3 code works unchanged.

## What's New in Phase 4

### Components to Build

1. **HTTP Server** (`server.go`)
   - Routes: `POST /mcp`, `GET /mcp`, `DELETE /mcp`, `GET /.well-known/...`
   - Port 8082 (separate from REST API on 8081)

2. **JWT Validator** (`jwt.go`)
   - Fetch Auth0 public keys from JWKS endpoint
   - Validate RS256 signature
   - Check issuer, audience, expiration
   - Cache public keys (refresh hourly)

3. **MCP Session Manager** (`session.go`)
   - Different from REST API sessions!
   - Tracks MCP client connections
   - Generates `Mcp-Session-Id` (UUID)
   - 24-hour TTL, in-memory storage

4. **JSON-RPC Parser** (`jsonrpc.go`)
   - Parse JSON-RPC 2.0 messages
   - Handle requests, responses, notifications
   - Error codes per MCP spec

5. **SSE Streamer** (`sse.go`)
   - Server-Sent Events for server→client messages
   - Event format: `event: message`, `id: N`, `data: {...}`

6. **OAuth Metadata** (`oauth_metadata.go`)
   - Return Auth0 configuration
   - Claude uses this to discover OAuth endpoints

7. **PassthroughTokenProvider** (`passthrough_token.go`)
   - Implements `TokenProvider` from Phase 3
   - Returns JWT from incoming request

### Integration Points

```go
// main.go - Phase 4
func run(ctx context.Context, cfg *config.Config) error {
    // Create MCP server
    mcpServer := server.NewMCPServer(cfg)

    // Start HTTP server (port 8082)
    go mcpServer.Start(":8082")

    // Wait for shutdown
    <-ctx.Done()
    return mcpServer.Shutdown(ctx)
}

// server.go - Handling requests
func (s *MCPServer) handleMCPPost(w http.ResponseWriter, r *http.Request) {
    // 1. Validate JWT
    jwt := extractBearer(r)
    claims, err := s.jwtValidator.ValidateToken(jwt)
    userID := claims.Subject

    // 2. Handle initialize (creates session)
    if req.Method == "initialize" {
        session := s.sessionMgr.CreateSession(userID)
        w.Header().Set("Mcp-Session-Id", session.ID)
        // ... return capabilities
    }

    // 3. Validate session for other requests
    sessionID := r.Header.Get("Mcp-Session-Id")
    session, err := s.sessionMgr.GetSession(sessionID)

    // 4. Create token provider for this request
    tokenProvider := NewPassthroughTokenProvider(jwt, claims.ExpiresAt)

    // 5. Create REST client (Phase 3) - works unchanged!
    restClient := client.NewHTTPClient(
        cfg.APIBaseURL,
        tokenProvider,
        client.NewSessionManager(cfg.APIBaseURL, tokenProvider, audience),
        audience,
    )

    // 6. Route to tool handler (Phase 5)
    s.handleToolCall(restClient, req)
}
```

## Authentication Flow Comparison

### OLD (Device Code Flow - stdio)

1. User runs `mcpbridge` locally
2. MCP server initiates Device Code Flow
3. Server prints: "Visit https://auth0.com/activate and enter: ABCD-1234"
4. User opens browser manually, enters code
5. Server polls Auth0 for token
6. Server receives JWT
7. Server uses JWT to call ToolBridge API

**Problems:**
- Clunky UX (copy-paste code)
- Local process required
- One user at a time

### NEW (Browser OAuth - Streamable HTTP)

1. User opens Claude Desktop
2. User adds server: `https://api.toolbridge.com/mcp`
3. Claude fetches `/.well-known/oauth-authorization-server`
4. Claude redirects user to Auth0 (standard OAuth)
5. User authenticates in browser (no codes!)
6. Auth0 redirects back to Claude with JWT
7. Claude sends JWT to MCP server in `Authorization` header
8. MCP server validates JWT, reuses it to call ToolBridge API

**Benefits:**
- Standard OAuth UX
- Browser-based (familiar)
- No local process needed
- Multi-user capable

## Session Management: Two Types!

This is important to understand:

### 1. MCP Session (Phase 4 - NEW)
- **Purpose**: Track Claude connection
- **Managed by**: `MCPSessionManager` in `server/session.go`
- **Identifier**: `Mcp-Session-Id` header (UUID)
- **Lifetime**: 24 hours
- **Storage**: In-memory map
- **Scope**: One per Claude connection

### 2. REST API Session (Phase 3 - EXISTING)
- **Purpose**: Track sync epoch with ToolBridge API
- **Managed by**: `SessionManager` in `client/session_manager.go`
- **Identifier**: `X-Sync-Session` header (UUID from API)
- **Lifetime**: 24 hours (23h cache for safety)
- **Storage**: Cached per user
- **Scope**: One per user per MCP server instance

Both are needed! They serve different purposes.

## Configuration Changes

**Good news: No changes to Phase 1 config!**

Your existing `config.Config` works perfectly:

```go
type Config struct {
    Auth0      Auth0Config     // Used for JWT validation + OAuth metadata
    APIBaseURL string          // ToolBridge API URL (Phase 3 client uses this)
    DevMode    bool            // Still useful for testing
    // ...
}
```

**What's used where:**
- `cfg.Auth0.Domain` → JWKS URL: `https://{domain}/.well-known/jwks.json`
- `cfg.Auth0.SyncAPI.Audience` → JWT validation (must match JWT `aud` claim)
- `cfg.APIBaseURL` → Phase 3 REST client (unchanged)
- `cfg.Auth0` clients → OAuth metadata endpoint (return to Claude)

## Phase 2 Code: Not Used!

**Important**: Phase 2 (Device Code Flow) is **NOT used** in Streamable HTTP mode.

The `internal/mcpserver/auth/` package with Device Code Flow was built for stdio mode. In Streamable HTTP:
- User authenticates in browser (Claude handles OAuth)
- Claude sends JWT to MCP server
- MCP server just validates JWT (doesn't acquire it)

Phase 2 code can stay for backward compatibility with stdio mode (dev/testing), but production Streamable HTTP doesn't need it.

## Deployment Strategy

### Development (Local)
```bash
# Terminal 1: REST API
make dev  # Port 8081

# Terminal 2: MCP Server
make run-mcp  # Port 8082

# Access:
# - REST API: http://localhost:8081
# - MCP Server: http://localhost:8082/mcp
# - OAuth metadata: http://localhost:8082/.well-known/oauth-authorization-server
```

### Production (Cloud)

**Option 1: Separate Services (Recommended)**
- Deploy REST API to one service (port 8081)
- Deploy MCP Server to another service (port 8082)
- Benefits: Independent scaling, clear separation

**Option 2: Combined Service**
- Run both in one process (different ports)
- Benefits: Simpler deployment, shared resources

**Deployment targets:**
- **Fly.io**: `fly launch` (Dockerfile included)
- **Railway**: Connect GitHub repo, auto-deploy
- **Heroku**: `git push heroku main`
- **Kubernetes**: Use provided manifests

## Testing Strategy

### Phase 3 Tests ✅ Still Valid!

All your Phase 3 tests work unchanged:
- `httpclient_test.go` - Retry logic, header injection
- `session_manager_test.go` - Session caching, thread safety

### Phase 4 New Tests

**Unit tests:**
- `jwt_test.go` - Token validation, JWKS caching
- `session_test.go` - MCP session management
- `jsonrpc_test.go` - Message parsing
- `sse_test.go` - Stream formatting

**Integration tests:**
- Full MCP flow (initialize → tool call → response)
- Concurrent sessions (multiple users)
- JWT expiry handling
- Session cleanup

## Migration Path

You don't need to migrate! Just implement Phase 4:

1. **Keep Phase 3 code unchanged** ✅
2. **Implement Phase 4 server** (new package)
3. **Update main.go** to start HTTP server
4. **Deploy to cloud** (Fly.io, Railway, etc.)
5. **Configure in Claude** (add URL)

Phase 2 (Device Code Flow) stays for backward compatibility but isn't used in production.

## Next Steps

1. **Implement Phase 4** (see `docs/mcp-bridge-phase4-prompt.md`)
   - JWT validation
   - HTTP server with `/mcp` endpoints
   - MCP session management
   - SSE streaming
   - OAuth metadata endpoint

2. **Implement Phase 5-6** (tool handlers)
   - Tool registry
   - Notes, tasks, comments, chats tools
   - Uses Phase 3 REST client

3. **Deploy to production**
   - Dockerfile
   - Fly.io / Railway setup
   - OAuth configuration in Auth0

4. **Test with Claude**
   - Add server URL in Claude Desktop
   - Complete OAuth flow
   - Test tool calls

## Questions?

**Q: Do I need to change Phase 3 code?**
A: No! Phase 3 works unchanged. Just use `PassthroughTokenProvider` instead of Device Code Flow.

**Q: What happens to Phase 2 (Device Code Flow)?**
A: It stays for backward compatibility (stdio mode for dev/testing) but isn't used in production Streamable HTTP.

**Q: Can I still run locally for development?**
A: Yes! Run the MCP server on `localhost:8082` and test with curl or MCP Inspector.

**Q: How do I get a JWT for testing?**
A: Use Auth0 dashboard or curl to get a token for your audience. See Phase 4 testing section.

**Q: Can I deploy this to my own domain?**
A: Yes! Deploy anywhere that supports HTTP servers (Fly.io, Railway, Heroku, K8s, etc.).

## References

- **Phase 4 detailed spec**: `docs/mcp-bridge-phase4-prompt.md`
- **Phase 4 quickstart**: `docs/mcp-bridge-phase4-quickstart.md`
- **Updated architecture**: `cmd/mcpbridge/README.md`
- **MCP Streamable HTTP spec**: https://spec.modelcontextprotocol.io/specification/2025-03-26/basic/transports/
- **Auth0 JWT validation**: https://auth0.com/docs/secure/tokens/json-web-tokens/validate-json-web-tokens
