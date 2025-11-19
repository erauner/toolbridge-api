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
# - TENANT_HEADER_SECRET=dev-tenant-secret
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
# - TOOLBRIDGE_TENANT_ID=test-tenant-123
# - TOOLBRIDGE_TENANT_HEADER_SECRET=dev-tenant-secret  # Must match Go API!
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
# Expected: {"status":"healthy","tenant_id":"test-tenant-123",...}
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

# 2. Generate tenant headers (Python helper)
python3 << 'EOF'
import hmac, hashlib, time, sys

tenant_id = "test-tenant-123"
secret = "dev-tenant-secret"
timestamp_ms = int(time.time() * 1000)

message = f"{tenant_id}:{timestamp_ms}"
signature = hmac.new(
    secret.encode('utf-8'),
    message.encode('utf-8'),
    hashlib.sha256
).hexdigest()

print(f"X-TB-Tenant-ID: {tenant_id}")
print(f"X-TB-Timestamp: {timestamp_ms}")
print(f"X-TB-Signature: {signature}")
EOF

# 3. Test authenticated request with tenant headers
# Copy the headers from step 2 and use them:
curl -X GET "http://localhost:8080/v1/notes?limit=10" \
  -H "X-Debug-Sub: test-user" \
  -H "X-Sync-Session: $SESSION_ID" \
  -H "X-Sync-Epoch: $EPOCH" \
  -H "X-TB-Tenant-ID: test-tenant-123" \
  -H "X-TB-Timestamp: REPLACE_WITH_TIMESTAMP" \
  -H "X-TB-Signature: REPLACE_WITH_SIGNATURE"

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
        "TOOLBRIDGE_TENANT_ID": "test-tenant-123",
        "TOOLBRIDGE_TENANT_HEADER_SECRET": "dev-tenant-secret",
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
  -H "Content-Type: application/json" \
  -d '{"title":"Direct API Note","content":"Created via Go REST API"}'

# 2. List notes to see the result
curl "http://localhost:8080/v1/notes?limit=10" \
  -H "X-Debug-Sub: test-user" \
  -H "X-Sync-Session: $SESSION_ID" \
  -H "X-Sync-Epoch: $EPOCH"

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

For production, use real JWT from Auth0 or generate HS256 token.

### "Invalid tenant headers"

**Problem:** Signature mismatch or timestamp skew.

**Solutions:**
1. Ensure `TOOLBRIDGE_TENANT_HEADER_SECRET` matches `TENANT_HEADER_SECRET` in Go API
2. Generate fresh timestamp (within 5 minutes)
3. Check Python script computes signature correctly

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

### "Tenant header validation disabled"

**Problem:** Go API is running without `TENANT_HEADER_SECRET` set.

**Solution:**
```bash
# Set in .env or export:
export TENANT_HEADER_SECRET=dev-tenant-secret

# Restart Go API
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
3. **Deploy to Fly.io:** Follow `docs/SPEC-FASTMCP-INTEGRATION.md`
4. **Production secrets:** Generate secure secrets with `openssl rand -base64 32`

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

- **Specification:** `docs/SPEC-FASTMCP-INTEGRATION.md`
- **MCP README:** `mcp/README.md`
- **Go API README:** `README.md`
- **Deployment:** `docs/DEPLOYMENT.md`

## Getting Help

- Check logs in both Go API and Python MCP terminals
- Use `DEBUG` log level for detailed traces
- Review `docs/SPEC-FASTMCP-INTEGRATION.md` for architecture details
- Inspect PostgreSQL data to verify changes persisted
