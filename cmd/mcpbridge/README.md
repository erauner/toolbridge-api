# MCP Bridge Server

A Model Context Protocol (MCP) bridge that translates Claude tool calls into ToolBridge REST API operations.

## Overview

The MCP Bridge provides a **remote MCP server** using **Streamable HTTP transport** that:
- Validates Auth0 JWT tokens from Claude (user authenticates in browser)
- Manages MCP sessions and REST API sessions
- Translates MCP tool calls to REST CRUD operations
- Maintains full compatibility with the Dart MCP tool schemas
- Can be accessed remotely from Claude Desktop, Claude Web, or any MCP client

### Architecture

```
Claude Desktop/Web
    ↓ User adds server URL: https://api.toolbridge.com/mcp
    ↓ OAuth flow (Auth0) - user authenticates in browser
    ↓ HTTP requests with JWT Bearer token
    ↓
Remote MCP Server (Streamable HTTP)
    POST /mcp        ← JSON-RPC requests
    GET /mcp         ← SSE stream for server→client messages
    DELETE /mcp      ← Close session
    /.well-known/... ← OAuth metadata discovery
    ↓ Uses REST client from Phase 3
    ↓
ToolBridge REST API (port 8081)
    ↓
PostgreSQL
```

**Key Benefits:**
- **Remote access**: Deploy anywhere, connect from any Claude instance
- **OAuth in browser**: Users authenticate via standard OAuth flow (no Device Code Flow in server)
- **Multi-client**: Multiple users can connect simultaneously
- **Cloud-native**: Deploy on Fly.io, Railway, Heroku, etc.
- **No client installation**: Just add URL in Claude settings

## Quick Start

### Development Mode (No Auth0)

For local development and testing, you can run in dev mode which bypasses Auth0:

```bash
# Using make
make run-mcp

# Or directly with go run
MCP_DEV_MODE=true MCP_API_BASE_URL=http://localhost:8081 go run ./cmd/mcpbridge --dev
```

Dev mode uses the `X-Debug-Sub` header to authenticate with the REST API.

**Verify your setup**:
```bash
# Run automated smoke tests to verify --dev and --debug flags work correctly
make test-mcp-smoke
```

### Production Mode (With Auth0)

1. Create a configuration file (see `config/mcpbridge_config.example.json`)
2. Configure Auth0 clients and sync API audience
3. Run with config file:

```bash
./bin/mcpbridge --config /path/to/config.json
```

Or use environment variables:

```bash
export AUTH0_DOMAIN="your-tenant.us.auth0.com"
export AUTH0_CLIENT_ID_NATIVE="your-client-id"
export AUTH0_SYNC_API_AUDIENCE="https://api.toolbridge.example.com"
export MCP_API_BASE_URL="https://api.toolbridge.example.com"

./bin/mcpbridge
```

## Configuration

### Config File (JSON)

Create a config file based on `config/mcpbridge_config.example.json`:

```json
{
  "apiBaseUrl": "http://localhost:8081",
  "debug": false,
  "devMode": false,
  "logLevel": "info",
  "auth0": {
    "domain": "your-tenant.us.auth0.com",
    "clients": {
      "native": {
        "clientId": "YOUR_CLIENT_ID",
        "scopes": ["openid", "profile", "email", "offline_access"]
      }
    },
    "syncApi": {
      "audience": "https://api.toolbridge.example.com"
    }
  }
}
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_API_BASE_URL` | ToolBridge REST API base URL | `http://localhost:8081` |
| `MCP_DEV_MODE` | Enable dev mode (bypass Auth0) | `false` |
| `MCP_DEBUG` | Enable debug logging | `false` |
| `MCP_LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |
| `AUTH0_DOMAIN` | Auth0 tenant domain | - |
| `AUTH0_CLIENT_ID_NATIVE` | Auth0 native client ID | - |
| `AUTH0_CLIENT_ID_WEB` | Auth0 web client ID | - |
| `AUTH0_CLIENT_ID_NATIVE_MACOS` | Auth0 macOS client ID | - |
| `AUTH0_SYNC_API_AUDIENCE` | Sync API audience | - |
| `AUTH0_SYNC_API_SCOPE` | Additional scopes for sync API | - |

### CLI Flags

```bash
./bin/mcpbridge [flags]

Flags:
  --config string      Path to configuration file (JSON)
  --dev                Enable development mode (bypasses Auth0 validation, uses X-Debug-Sub header)
  --debug              Enable debug logging with pretty console output (implies --log-level debug)
  --log-level string   Log level (debug, info, warn, error) (default "info")
  --version            Show version information
```

**Note**: The `--debug` flag automatically enables debug-level logging and switches to
console output format with colors. You don't need to also specify `--log-level debug`.

## Claude Desktop Integration

### Remote Server (Production - Streamable HTTP)

Add your remote MCP server URL in Claude Desktop settings:

**Via Claude Desktop UI:**
1. Open Claude Desktop settings
2. Go to "Connectors" or "MCP Servers"
3. Click "Add Server"
4. Enter your server URL: `https://api.toolbridge.com/mcp`
5. Complete OAuth authentication in browser
6. Done! Claude can now access your ToolBridge data

**Note**: Direct `claude_desktop_config.json` configuration won't work for remote servers. You must use the UI to add remote servers.

### Local Development (stdio mode - DEPRECATED)

For local testing only, you can still run as stdio process:

```json
{
  "mcpServers": {
    "toolbridge-local": {
      "command": "/path/to/mcpbridge",
      "args": ["--dev", "--stdio"],
      "env": {
        "MCP_API_BASE_URL": "http://localhost:8081",
        "MCP_DEV_MODE": "true"
      }
    }
  }
}
```

**Important**: stdio mode is for development only. Production deployments should use Streamable HTTP (remote server).

## Building

```bash
# Build binary
make build-mcp

# Binary will be at: bin/mcpbridge
```

## Testing

### Quick Verification (Smoke Tests)

The smoke test suite verifies that the bridge works correctly without config:

```bash
make test-mcp-smoke
```

**What it tests**:
- ✅ `--dev` flag starts without Auth0 config
- ✅ `--debug` flag enables debug logging
- ✅ `--version` flag displays version info
- ✅ No Auth0 validation errors in dev mode

This is the **recommended first step** after building to verify your setup.

### Unit Tests

```bash
# Run unit tests (config loader, validation, etc.)
make test-mcp

# Run with verbose output
go test -v ./internal/mcpserver/...
```

**Coverage**:
- Config file loading and env var precedence
- CLI flag override ordering
- Template substitution in redirect URIs
- Default scope computation

## Architecture Details

### Transport Layer

**Streamable HTTP** (MCP specification 2025-03-26):
- `POST /mcp` - JSON-RPC requests from Claude
- `GET /mcp` - SSE stream for server→client messages
- `DELETE /mcp` - Close MCP session
- `GET /.well-known/oauth-authorization-server` - OAuth metadata discovery

### Request Flow

```
1. User opens Claude Desktop/Web
   ↓
2. User adds MCP server: https://api.toolbridge.com/mcp
   ↓
3. Claude fetches /.well-known/oauth-authorization-server
   ↓
4. Claude redirects user to Auth0 for authentication
   ↓
5. User authenticates in browser
   ↓
6. Auth0 issues JWT token to Claude
   ↓
7. Claude sends initialize request to POST /mcp with JWT
   ↓
8. MCP Bridge validates JWT (RS256 signature, audience, issuer)
   ↓
9. MCP Bridge creates session, returns Mcp-Session-Id header
   ↓
10. Claude sends tool calls with JWT + Mcp-Session-Id
    ↓
11. MCP Bridge validates session and JWT
    ↓
12. MCP Bridge creates PassthroughTokenProvider (reuses JWT)
    ↓
13. MCP Bridge uses Phase 3 REST client to call ToolBridge API
    ↓
14. REST client auto-manages API sessions and epochs
    ↓
15. Results returned to Claude
```

### Session Management

**Two types of sessions:**

1. **MCP Session** (Phase 4 - this server)
   - Tracks Claude connection
   - Managed by MCP Bridge
   - Stored in memory
   - 24-hour TTL

2. **REST API Session** (Phase 3 - REST client)
   - Tracks sync epoch with ToolBridge API
   - Managed by SessionManager from Phase 3
   - Created automatically by REST client
   - 23-hour cache TTL (server TTL is 24h)

### Authentication Flow

**No Device Code Flow needed!** User authenticates in browser via standard OAuth:

1. Claude discovers OAuth endpoints from `/.well-known/...`
2. User authenticates with Auth0 in browser (standard OAuth flow)
3. Claude receives JWT
4. Claude sends JWT in `Authorization: Bearer <token>` header
5. MCP Bridge validates JWT (RS256 public key from Auth0 JWKS)
6. MCP Bridge reuses same JWT to call ToolBridge API (passthrough)

**JWT Validation:**
- Fetch public keys from `https://<domain>/.well-known/jwks.json`
- Validate RS256 signature
- Check issuer: `https://<domain>/`
- Check audience: matches sync API audience
- Extract user ID from `sub` claim

## Tool Categories

The MCP bridge implements the following tool categories:

- **Notes**: List, get, create, update, delete, toggle pin
- **Tasks**: List, get, create, update, delete, complete, reopen
- **Comments**: List, get, create, update, delete
- **Chats**: Create, append, get, list, delete
- **Chat Messages**: List, get, create, update, delete
- **Chat Context**: Attach, detach, list, clear context items
- **OAuth Discovery**: Discover OAuth configuration for servers
- **Server Info**: Get API capabilities and limits

## Logging

Logs are written to stderr in JSON format (or console format in debug mode):

```json
{
  "level": "info",
  "time": "2025-01-15T10:30:45Z",
  "message": "MCP server initialized",
  "version": "0.1.0"
}
```

## Testing Auth0 Integration

### Prerequisites

1. **Create Auth0 Application**:
   - Go to your Auth0 Dashboard
   - Create a new "Native" application
   - Enable "Device Code" grant type in Application Settings → Advanced Settings → Grant Types
   - Note your Domain and Client ID

2. **Configure Sync API**:
   - Create an API in Auth0 Dashboard (if not already created)
   - Note the API Identifier (audience)
   - Configure permissions/scopes as needed

3. **Create Configuration File**:
   ```json
   {
     "apiBaseUrl": "http://localhost:8081",
     "debug": true,
     "devMode": false,
     "auth0": {
       "domain": "your-tenant.us.auth0.com",
       "clients": {
         "native": {
           "clientId": "YOUR_CLIENT_ID",
           "scopes": ["openid", "profile", "email", "offline_access"]
         }
       },
       "syncApi": {
         "audience": "https://api.toolbridge.example.com"
       }
     }
   }
   ```

### Manual Testing Steps

#### 1. Initial Token Acquisition (Interactive)

Start the bridge with your Auth0 configuration:

```bash
./bin/mcpbridge --config config/auth0_prod.json --debug
```

**Expected behavior**:
- Bridge displays device code instructions:
  ```
  ═══════════════════════════════════════════
    Auth0 Device Authorization Required
  ═══════════════════════════════════════════

  Visit: https://your-tenant.us.auth0.com/activate
  Enter code: XXXX-XXXX

  Waiting for authorization...
  ═══════════════════════════════════════════
  ```
- Open the URL in a browser and enter the code
- After authorization, you should see: `"message": "device authorization successful"`
- Bridge should log: `"message": "Auth0 token broker initialized"`

#### 2. Token Caching

Restart the bridge immediately (within token expiry time):

```bash
./bin/mcpbridge --config config/auth0_prod.json --debug
```

**Expected behavior**:
- No device code prompt (uses cached refresh token from keyring)
- Should log: `"message": "loaded refresh token from keyring"`
- Token acquisition should be silent and fast

#### 3. Token Refresh (Automatic)

The broker automatically refreshes tokens when they're within 5 minutes of expiry.

**To test manually**:
1. Modify a cached token's expiry time (requires code change for testing)
2. Or wait until token approaches expiry
3. Make a request - token should refresh silently

**Expected logs**:
```json
{"level":"debug","message":"token is expiring soon"}
{"level":"debug","message":"refreshing access token"}
{"level":"debug","message":"access token refreshed successfully"}
```

#### 4. Token Invalidation

Test token cache invalidation (useful when receiving 401 errors):

```go
// In your REST client (Phase 3)
if resp.StatusCode == 401 {
    broker.InvalidateToken(audience, scope)
    // Retry with fresh token
}
```

**Expected behavior**:
- Token removed from cache
- Next request triggers fresh token acquisition

#### 5. Logout

Currently, logout happens automatically on graceful shutdown (Ctrl+C):

```bash
./bin/mcpbridge --config config/auth0_prod.json
# Press Ctrl+C
```

**Expected logs**:
```json
{"level":"info","signal":"interrupt","message":"Received shutdown signal"}
{"level":"info","message":"Shutting down MCP server..."}
{"level":"info","message":"logged out successfully"}
```

### Unit Tests

Run broker tests to verify caching logic:

```bash
# Run all auth tests
go test -v ./internal/mcpserver/auth/...

# Run with race detector (important for concurrent code)
go test -race ./internal/mcpserver/auth/...

# Run with coverage
go test -cover ./internal/mcpserver/auth/...
```

**Tests cover**:
- ✅ Scope merging (default + user scopes, deduplication, sorting)
- ✅ Cache key generation
- ✅ Expiry detection (5-minute buffer)
- ✅ Token caching behavior
- ✅ Cache invalidation
- ✅ Thread safety (concurrent access)

## Troubleshooting

### General Debugging Steps

**First, verify basic functionality**:
```bash
# Run smoke tests to ensure --dev and --debug flags work
make test-mcp-smoke
```

If smoke tests fail, check:
1. Binary is built correctly: `make build-mcp`
2. No conflicting environment variables are set
3. File permissions on the binary

**Enable debug logging** for detailed diagnostics:
```bash
./bin/mcpbridge --dev --debug
```

### Auth0 Token Acquisition Fails

**Problem**: `Failed to acquire Auth0 token`

**Solution**:
1. Verify Auth0 domain and client ID are correct
2. Ensure client has device code flow enabled
3. Check network connectivity to Auth0 tenant
4. Try running with `--debug` to see detailed logs

### Epoch Mismatch Errors

**Problem**: `epoch_mismatch` errors on every request

**Solution**:
1. Delete local session cache (if implemented)
2. Ensure API server is running and accessible
3. Check that `X-Sync-Epoch` header is being sent
4. Verify database migrations have run (owner_state table exists)

### Rate Limit Exceeded

**Problem**: HTTP 429 errors

**Solution**:
1. Reduce request frequency
2. Check rate limit configuration in API server
3. Use batch operations where available
4. Respect `Retry-After` header (handled automatically)

### Dev Mode Not Working

**Problem**: Authentication fails in dev mode

**Solution**:
1. Ensure API server has `JWT_HS256_SECRET` configured
2. Verify `X-Debug-Sub` header is being sent
3. Check API server logs for debug header validation
4. Confirm dev mode is enabled (`MCP_DEV_MODE=true`)

## Development Roadmap

**Phase 1: Foundation** ✅
- [x] Configuration loading (JSON + environment)
- [x] CLI flags and logging setup
- [x] Graceful shutdown handling

**Phase 2: Auth0 Integration** ✅
- [x] OAuth2 Device Code Flow (for stdio mode - deprecated)
- [x] Token caching and refresh
- [x] Secure token storage (keyring integration)
- Note: Device Code Flow is NOT used in Streamable HTTP mode!

**Phase 3: REST Client** ✅
- [x] HTTP client with auto-auth and retry logic
- [x] Session management (REST API sessions)
- [x] Epoch coordination
- [x] Entity CRUD operations
- [x] TokenProvider abstraction (enables Streamable HTTP reuse)

**Phase 4: Streamable HTTP Server** (In Progress)
- [ ] HTTP server with POST/GET/DELETE `/mcp` endpoints
- [ ] JWT validation (Auth0 RS256)
- [ ] MCP session management
- [ ] JSON-RPC message parsing
- [ ] SSE streaming for server→client messages
- [ ] OAuth metadata discovery (`/.well-known/...`)
- [ ] PassthroughTokenProvider for Phase 3 integration

**Phase 5-6: Tool Implementations** (Planned)
- [ ] Tool registry and dispatcher
- [ ] Notes tools (create, list, get, update, delete, pin)
- [ ] Tasks tools (create, complete, reopen, list)
- [ ] Comments tools (create, list, update, delete)
- [ ] Chats tools (create, append, get, list, delete)
- [ ] Chat messages tools (list, create, update)
- [ ] Context attachment tools (attach, detach, list, clear)

**Phase 7: Production Deployment** (Planned)
- [ ] Dockerfile for Streamable HTTP server
- [ ] Kubernetes manifests
- [ ] Fly.io / Railway deployment guides
- [ ] Production OAuth configuration
- [ ] Rate limiting and monitoring

**Phase 8: Testing & Polish** (Planned)
- [ ] Integration tests (full MCP flow)
- [ ] Load testing (concurrent sessions)
- [ ] Error handling improvements
- [ ] Documentation updates

## License

MIT
