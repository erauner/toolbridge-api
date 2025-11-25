# ToolBridge MCP Quick Start Guide

Get the ToolBridge FastMCP integration running locally in under 10 minutes.

## Prerequisites

- Go 1.24+
- Python 3.12+
- PostgreSQL 16
- Docker (optional, for PostgreSQL)

## Step 1: Start PostgreSQL

```bash
# Option A: Using Docker Compose (recommended)
docker-compose up -d postgres

# Option B: Local PostgreSQL
# Make sure PostgreSQL is running on localhost:5432
```

## Step 2: Set Up Go API

```bash
# 1. Install Go dependencies
go mod download

# 2. Run migrations
make migrate
# or manually:
./scripts/migrate.sh

# 3. Configure environment
cp .env.example .env

# Edit .env:
# - DATABASE_URL=postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable
# - JWT_HS256_SECRET=dev-secret-change-in-production
# - ENV=dev

# 4. Start Go API
make dev
# or
go run ./cmd/server

# Expected output:
# {"level":"info","service":"toolbridge-api","addr":":8080","message":"starting HTTP server"}
```

Verify Go API is running:
```bash
curl http://localhost:8080/healthz
# Expected: ok
```

## Step 3: Set Up Python MCP Service

```bash
# 1. Navigate to MCP directory
cd mcp

# 2. Install dependencies
pip install -e .
# or with uv (faster):
uv sync

# 3. Configure environment
cp .env.example .env

# Edit mcp/.env:
# - TOOLBRIDGE_GO_API_BASE_URL=http://localhost:8080
# - TOOLBRIDGE_LOG_LEVEL=DEBUG

# 4. Start MCP service
uvicorn toolbridge_mcp.server:mcp --reload --host 0.0.0.0 --port 8001

# Expected output:
# INFO:     Uvicorn running on http://0.0.0.0:8001
```

Verify MCP service is running:
```bash
curl http://localhost:8001/health_check \
  -H "Authorization: Bearer fake-jwt-for-testing"
# Expected: {"status":"healthy",...}
```

## Step 4: Test the Integration

### Option A: Manual Testing with curl

```bash
# 1. Create a session
SESSION_RESPONSE=$(curl -s -X POST http://localhost:8081/v1/sync/sessions \
  -H 'X-Debug-Sub: test-user')
SESSION_ID=$(echo $SESSION_RESPONSE | jq -r '.id')
EPOCH=$(echo $SESSION_RESPONSE | jq -r '.epoch')

echo "Session ID: $SESSION_ID"
echo "Epoch: $EPOCH"

# 2. Test authenticated request with tenant header
curl -X GET "http://localhost:8080/v1/notes?limit=10" \
  -H "X-Debug-Sub: test-user" \
  -H "X-Sync-Session: $SESSION_ID" \
  -H "X-Sync-Epoch: $EPOCH" \
  -H "X-TB-Tenant-ID: tenant_thinkpen_b2c"

# Expected: {"items":[],"nextCursor":null}
```

### Option B: Test with MCP Inspector

```bash
# Install MCP Inspector
npm install -g @modelcontextprotocol/inspector

# Run inspector
npx @modelcontextprotocol/inspector \
  --url http://localhost:8001 \
  --headers "Authorization: Bearer fake-jwt-token"

# In the inspector UI:
# 1. Click "Tools" tab
# 2. Find "list_notes" tool
# 3. Click "Execute"
# 4. See results
```

### Option C: Test with Claude Desktop

Update `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "toolbridge-local": {
      "command": "uvicorn",
      "args": [
        "toolbridge_mcp.server:mcp",
        "--host", "0.0.0.0",
        "--port", "8001"
      ],
      "cwd": "/path/to/toolbridge-api/mcp",
      "env": {
        "TOOLBRIDGE_GO_API_BASE_URL": "http://localhost:8080",
        "TOOLBRIDGE_LOG_LEVEL": "DEBUG"
      }
    }
  }
}
```

Restart Claude Desktop and try:
```
Create a note titled "Test Note" with content "This is a test from Claude Desktop"
```

## Step 5: Verify End-to-End Flow

Create a note via MCP and verify it in the database:

```bash
# 1. Create note via Go API directly (baseline)
curl -X POST http://localhost:8080/v1/notes \
  -H "X-Debug-Sub: test-user" \
  -H "X-Sync-Session: $SESSION_ID" \
  -H "X-Sync-Epoch: $EPOCH" \
  -H "X-TB-Tenant-ID: tenant_thinkpen_b2c" \
  -H "Content-Type: application/json" \
  -d '{"title":"Direct API Note","content":"Created via Go REST API"}'

# 2. List notes to see the result
curl "http://localhost:8080/v1/notes?limit=10" \
  -H "X-Debug-Sub: test-user" \
  -H "X-Sync-Session: $SESSION_ID" \
  -H "X-Sync-Epoch: $EPOCH" \
  -H "X-TB-Tenant-ID: tenant_thinkpen_b2c"

# Expected: Note appears in list

# 3. Check PostgreSQL directly
psql -U toolbridge -d toolbridge -c "SELECT uid, payload_json->>'title' as title FROM note;"
```

## Common Issues & Solutions

### "Missing Authorization header"

**Problem:** MCP tools require JWT token from client.

**Solution:** When testing locally, pass any non-empty Authorization header:
```bash
curl -H "Authorization: Bearer fake-token-for-testing" ...
```

For production, use real JWT from OIDC provider.

### "Not authorized for requested tenant"

**Problem:** User not authorized for the tenant ID in X-TB-Tenant-ID header.

**Solutions:**
1. Use `X-Debug-Sub` header in dev mode to bypass JWT validation
2. Ensure the tenant ID matches what the user is authorized for
3. For B2C testing, use the default tenant: `X-TB-Tenant-ID: tenant_thinkpen_b2c`

### "Connection refused to localhost:8080"

**Problem:** Go API not running.

**Solution:**
```bash
# Check if Go API is running
lsof -i :8080

# If not running, start it:
cd /path/to/toolbridge-api
make dev
```

### "Uvicorn won't start"

**Problem:** Python dependencies not installed or wrong version.

**Solution:**
```bash
cd mcp
pip uninstall toolbridge-mcp
pip install -e .
# or
uv sync --reinstall
```

## Next Steps

1. **Add more tools:** Copy `tools/notes.py` pattern to add tasks, comments, chats
2. **Write tests:** See `../internal/auth/tenant_headers_test.go` for examples
3. **Deploy to Fly.io:** Follow `docs/DEPLOYMENT-FLYIO.md` for MCP-only deployment
4. **Full-stack deployment:** See `docs/SPEC-FASTMCP-INTEGRATION.md` for Go + Python
5. **Production secrets:** See `docs/SECRETS-REFERENCE.md`

## Development Tips

### Hot Reloading

Both services support hot reloading:

```bash
# Go API (restart on changes)
make dev  # Uses air for auto-restart

# Python MCP (--reload flag)
uvicorn toolbridge_mcp.server:mcp --reload --host 0.0.0.0 --port 8001
```

### Debugging

**Go API logs:**
```bash
# Pretty logs in dev mode
export ENV=dev
make dev
```

**Python MCP logs:**
```bash
# Verbose logging
export TOOLBRIDGE_LOG_LEVEL=DEBUG
uvicorn toolbridge_mcp.server:mcp --reload --log-level debug
```

**Database queries:**
```bash
# Watch database activity
psql -U toolbridge -d toolbridge

# In psql:
SELECT uid, payload_json->>'title', updated_at_ms FROM note ORDER BY updated_at_ms DESC LIMIT 10;
```

### Testing Workflow

```bash
# 1. Run Go tests
go test ./internal/auth -v

# 2. Run Python tests (when added)
cd mcp
pytest

# 3. Integration test via curl
./scripts/smoke-test.sh
```

## Reference

- **Local Setup Guide:** This document (quickstart)
- **Fly.io Deployment:** `docs/DEPLOYMENT-FLYIO.md` (MCP-only to staging)
- **Full-Stack Deployment:** `docs/SPEC-FASTMCP-INTEGRATION.md` (Go + Python)
- **Secrets Management:** `docs/SECRETS-REFERENCE.md`
- **MCP Service README:** `mcp/README.md`
- **Go API README:** `README.md`
- **K8s Deployment:** `../homelab-k8s/apps/toolbridge-api/README.md`

## Getting Help

- Check logs in both Go API and Python MCP terminals
- Use `DEBUG` log level for detailed traces
- Review `docs/SPEC-FASTMCP-INTEGRATION.md` for architecture details
- Inspect PostgreSQL data to verify changes persisted
