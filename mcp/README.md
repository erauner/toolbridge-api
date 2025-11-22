# ToolBridge FastMCP Integration

This directory contains the Python FastMCP server that provides MCP (Model Context Protocol) tools for interacting with the ToolBridge API.

## Architecture

```
┌─────────────────────────────────────────┐
│  MCP Client (LLM)                       │
│  (Claude Desktop, VS Code, etc.)        │
└──────────────┬──────────────────────────┘
               │ MCP Protocol (Streamable HTTP)
               │ Authorization: Bearer {jwt}
               ▼
┌─────────────────────────────────────────┐
│  Python FastMCP Service (Uvicorn)       │
│  Port 8001                              │
│  ┌────────────────────────────────────┐ │
│  │  ASGI App (mcp.http_app())         │ │
│  │  - Graceful SIGTERM/SIGINT handler │ │
│  │  - TenantDirectTransport           │ │
│  │  - Extracts JWT from request       │ │
│  │  - Generates signed tenant headers │ │
│  │  - X-TB-Tenant-ID                  │ │
│  │  - X-TB-Timestamp                  │ │
│  │  - X-TB-Signature                  │ │
│  └────────────────────────────────────┘ │
└──────────────┬──────────────────────────┘
               │ HTTP (localhost)
               │ Authorization + Tenant Headers
               ▼
┌─────────────────────────────────────────┐
│  Go REST API                            │
│  Port 8080                              │
│  ┌────────────────────────────────────┐ │
│  │  Middleware Chain:                 │ │
│  │  1. JWT Validation                 │ │
│  │  2. Tenant Header Validation       │ │
│  │  3. Service Layer                  │ │
│  └────────────────────────────────────┘ │
└──────────────┬──────────────────────────┘
               │
               ▼
          PostgreSQL
```

## Components

### Python MCP Service (`toolbridge_mcp/`)

- **`server.py`**: ASGI app creation via `mcp.http_app()` + uvicorn runner with graceful shutdown
- **`mcp_instance.py`**: FastMCP instance creation with WorkOS AuthKit provider
- **`config.py`**: Settings management (environment variables + shutdown timeouts)
- **`async_client.py`**: HTTP client factory with tenant transport
- **`transports/tenant_direct.py`**: Custom httpx transport that adds signed tenant headers
- **`utils/headers.py`**: HMAC-SHA256 signing utilities
- **`utils/requests.py`**: HTTP request helpers (call_get, call_post, etc.)
- **`tools/notes.py`**: MCP tools for note management (list, create, update, delete, etc.)

### Security

**Dual Authentication:**

1. **JWT (User Identity)**: Bearer token from MCP client validates user
2. **Signed Headers (Tenant Isolation)**: HMAC-signed headers prevent cross-tenant access

**Header Format:**
```
X-TB-Tenant-ID: {tenant_id}
X-TB-Timestamp: {unix_timestamp_ms}
X-TB-Signature: {hmac_sha256_hex}
```

**Signature Computation:**
```python
message = f"{tenant_id}:{timestamp_ms}"
signature = hmac.new(
    key=secret.encode('utf-8'),
    msg=message.encode('utf-8'),
    digestmod=hashlib.sha256
).hexdigest()
```

## Local Development

### Prerequisites

- Python 3.12+
- Go 1.24+
- PostgreSQL 16
- Docker (optional)

### Setup

1. **Install Python dependencies:**
   ```bash
   cd mcp
   pip install -e .
   # or with uv
   uv sync
   ```

2. **Configure environment:**
   ```bash
   cp .env.example .env
   # Edit .env with your settings
   ```

3. **Start Go API:**
   ```bash
   # Terminal 1: Start PostgreSQL
   docker-compose up -d postgres
   
   # Terminal 2: Start Go API
   cd ..
   make dev
   # or
   go run ./cmd/server
   ```

4. **Start MCP service:**
   ```bash
   # Terminal 3: Start MCP service
   cd mcp
   python -m toolbridge_mcp.server
   
   # OR for development with auto-reload:
   uvicorn toolbridge_mcp.server:app --reload --host 0.0.0.0 --port 8001
   ```

### Environment Variables

Create `mcp/.env`:

```bash
# Tenant configuration
TOOLBRIDGE_TENANT_ID=test-tenant-123
TOOLBRIDGE_TENANT_HEADER_SECRET=dev-secret-change-in-production

# Go API connection
TOOLBRIDGE_GO_API_BASE_URL=http://localhost:8080

# Logging
TOOLBRIDGE_LOG_LEVEL=DEBUG

# Graceful shutdown (optional - defaults shown)
TOOLBRIDGE_SHUTDOWN_TIMEOUT_SECONDS=7  # Must be < Fly kill_timeout
TOOLBRIDGE_UVICORN_ACCESS_LOG=False
```

### Testing with MCP Inspector

```bash
# Install MCP inspector
npm install -g @modelcontextprotocol/inspector

# Test the MCP service
npx @modelcontextprotocol/inspector \
  --url http://localhost:8001 \
  --headers "Authorization: Bearer YOUR_JWT_TOKEN"
```

### Testing with Claude Desktop

Update `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "toolbridge": {
      "command": "python",
      "args": ["-m", "toolbridge_mcp.server"],
      "env": {
        "TOOLBRIDGE_TENANT_ID": "test-tenant-123",
        "TOOLBRIDGE_TENANT_HEADER_SECRET": "dev-secret",
        "TOOLBRIDGE_GO_API_BASE_URL": "http://localhost:8080"
      }
    }
  }
}
```

## Available MCP Tools

**Note:** FastMCP automatically provides a `health_check` tool that requires authentication. If you need a public unauthenticated health endpoint for monitoring, add a separate endpoint outside the MCP protocol (e.g., via a custom route in the ASGI app).

### Notes

- `list_notes(limit, cursor, include_deleted)` - List notes with pagination
- `get_note(uid)` - Retrieve a single note
- `create_note(title, content, tags)` - Create a new note
- `update_note(uid, title, content)` - Replace note (full update)
- `patch_note(uid, updates)` - Partial update
- `delete_note(uid)` - Soft delete
- `archive_note(uid)` - Archive note
- `process_note(uid, action, metadata)` - Process action (pin, unpin, etc.)

### Tasks, Comments, Chats, Chat Messages

*Coming soon - follow the same pattern as notes.py*

## Deployment

### Option 1: MCP-Only Deployment to Fly.io (Recommended for Staging)

Deploy only the Python MCP proxy to Fly.io while using an external Go API (e.g., in K8s).

**Complete guide:** See `../docs/DEPLOYMENT-FLYIO.md` for detailed instructions.

**Quick start:**
```bash
# Create app
fly apps create toolbridge-mcp-staging

# Set secrets
fly secrets set -a toolbridge-mcp-staging \
  TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  TOOLBRIDGE_TENANT_ID="staging-tenant-001" \
  TOOLBRIDGE_TENANT_HEADER_SECRET="<secret-from-k8s>"

# Deploy MCP-only image
fly deploy --config ../fly.staging.toml -a toolbridge-mcp-staging
```

**Benefits:**
- ✅ No database setup needed (uses existing K8s DB)
- ✅ Smaller footprint (Python-only, ~100MB image)
- ✅ Auto-scale to zero when idle (clean graceful shutdown via SIGTERM)
- ✅ Fast deployments (<2 minutes)

**Graceful Shutdown:**
- Server runs as ASGI app via uvicorn (not `mcp.run()`)
- Handles SIGTERM/SIGINT with configurable timeout (default 7s)
- Clean exit on Fly.io auto-stop without `asyncio.CancelledError` tracebacks
- See `../docs/DEPLOYMENT-FLYIO.md` for shutdown configuration details

### Option 2: Full-Stack Deployment (Go + Python)

Deploy both Go API and Python MCP service together using supervisor.

See `../docs/SPEC-FASTMCP-INTEGRATION.md` for complete documentation.

```bash
# Build full-stack image
docker build -f ../Dockerfile.mcp -t toolbridge-mcp:latest ..

# Set secrets (includes database)
fly secrets set -a toolbridge-tenant-abc123 \
  DATABASE_URL="postgres://..." \
  JWT_HS256_SECRET="$(openssl rand -base64 32)" \
  TENANT_HEADER_SECRET="$(openssl rand -base64 32)" \
  TOOLBRIDGE_TENANT_ID="abc123"

# Deploy
fly deploy -c ../fly.mcp.toml -a toolbridge-tenant-abc123
```

## Development Workflow

### Adding New Tools

1. **Create new tool module** (e.g., `tools/tasks.py`)
2. **Define Pydantic models** matching Go API responses
3. **Implement tools** using `@mcp.tool()` decorator
4. **Import in `server.py`** to register tools
5. **Test** with MCP inspector or Claude Desktop

Example:

```python
# tools/tasks.py
from toolbridge_mcp.server import mcp
from toolbridge_mcp.async_client import get_client
from toolbridge_mcp.utils.requests import call_get

@mcp.tool()
async def list_tasks(limit: int = 100) -> dict:
    """List tasks with pagination."""
    async with get_client() as client:
        response = await call_get(client, "/v1/tasks", params={"limit": limit})
        return response.json()
```

### Testing Tenant Header Validation

```bash
# Run integration tests
cd ..
go test ./internal/auth -v -run TestTenantHeaderValidation

# Manual test with curl
export JWT_TOKEN="your-jwt-token"
export TENANT_ID="test-tenant-123"
export SECRET="dev-secret"

# Generate signature
python -c "
import hmac, hashlib, time
tenant_id = '$TENANT_ID'
timestamp = str(int(time.time() * 1000))
message = f'{tenant_id}:{timestamp}'
sig = hmac.new(b'$SECRET', message.encode(), hashlib.sha256).hexdigest()
print(f'X-TB-Tenant-ID: {tenant_id}')
print(f'X-TB-Timestamp: {timestamp}')
print(f'X-TB-Signature: {sig}')
"

# Test request
curl http://localhost:8080/v1/notes \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "X-TB-Tenant-ID: test-tenant-123" \
  -H "X-TB-Timestamp: 1234567890000" \
  -H "X-TB-Signature: ..."
```

## Troubleshooting

### "Missing Authorization header"

- MCP client must provide JWT token in Authorization header
- Check Claude Desktop config includes proper authentication

### "Invalid tenant headers"

- Verify TENANT_HEADER_SECRET matches between Python and Go
- Check timestamp is within 5-minute window
- Ensure signature computation matches exactly

### "Connection refused"

- Verify Go API is running on port 8080
- Check TOOLBRIDGE_GO_API_BASE_URL points to correct address

### Import errors

- Run `uv sync` or `pip install -e .` to install dependencies
- Ensure you're in the correct virtual environment

## References

- [FastMCP Documentation](https://github.com/jlowin/fastmcp)
- [Model Context Protocol Specification](https://modelcontextprotocol.io)
- [ToolBridge API Documentation](../README.md)
- [Deployment Guide](../docs/SPEC-FASTMCP-INTEGRATION.md)
