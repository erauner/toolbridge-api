# MCP Bridge Server

A Model Context Protocol (MCP) bridge that translates Claude tool calls into ToolBridge REST API operations.

## Overview

The MCP Bridge provides a stdio-based MCP server that:
- Authenticates with Auth0 to obtain access tokens
- Manages REST API sessions and epoch coordination
- Translates MCP tool calls to REST CRUD operations
- Maintains full compatibility with the Dart MCP tool schemas

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
  --dev                Enable development mode (uses X-Debug-Sub header)
  --debug              Enable debug logging
  --log-level string   Log level (debug, info, warn, error) (default "info")
  --version            Show version information
```

## Claude Desktop Integration

To use with Claude Desktop, add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "toolbridge": {
      "command": "/path/to/mcpbridge",
      "args": ["--config", "/path/to/config.json"]
    }
  }
}
```

Or for dev mode:

```json
{
  "mcpServers": {
    "toolbridge": {
      "command": "/path/to/mcpbridge",
      "args": ["--dev"],
      "env": {
        "MCP_API_BASE_URL": "http://localhost:8081",
        "MCP_DEV_MODE": "true"
      }
    }
  }
}
```

## Building

```bash
# Build binary
make build-mcp

# Binary will be at: bin/mcpbridge
```

## Testing

```bash
# Run MCP bridge tests
make test-mcp

# Run with verbose output
go test -v ./internal/mcpserver/...
```

## Architecture

```
┌─────────────┐
│   Claude    │
│   Desktop   │
└─────┬───────┘
      │ stdio (JSON-RPC)
      │
┌─────▼───────────┐
│  MCP Bridge     │
│  - Auth0 Token  │
│  - Session Mgmt │
│  - Tool Routing │
└─────┬───────────┘
      │ HTTP/REST
      │ (Bearer Token + Session Headers)
      │
┌─────▼────────────┐
│  ToolBridge API  │
│  - Auth Validate │
│  - Epoch Check   │
│  - CRUD Ops      │
└─────┬────────────┘
      │
┌─────▼────────────┐
│   PostgreSQL     │
└──────────────────┘
```

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

## Troubleshooting

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

**Phase 2: Auth0 Integration** (In Progress)
- [ ] OAuth2 Device Code Flow
- [ ] Token caching and refresh
- [ ] Secure token storage

**Phase 3: REST Client** (Planned)
- [ ] HTTP client with retry logic
- [ ] Session management
- [ ] Epoch coordination

**Phase 4: MCP Server Core** (Planned)
- [ ] JSON-RPC message handling
- [ ] Tool registry
- [ ] Context management

**Phase 5-6: Tool Implementations** (Planned)
- [ ] Notes & Tasks tools
- [ ] Comments, Chats, Chat Messages
- [ ] Context attachment tools

**Phase 7: Additional Features** (Planned)
- [ ] OAuth discovery tool
- [ ] Server info tool

**Phase 8: Testing & Polish** (Planned)
- [ ] Integration tests
- [ ] Error handling improvements
- [ ] Documentation updates

## License

MIT
