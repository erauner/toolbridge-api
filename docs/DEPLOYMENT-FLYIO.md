# ToolBridge MCP Deployment to Fly.io

This guide covers deploying the **MCP-only** Python proxy service to Fly.io staging. The Go API and PostgreSQL continue to run in Kubernetes.

## Architecture

```
┌─────────────────────────────────────────┐
│  MCP Client (Claude Desktop, etc.)     │
└──────────────┬──────────────────────────┘
               │ HTTPS/MCP Protocol
               │ Authorization: Bearer {jwt}
               ▼
┌─────────────────────────────────────────┐
│  Fly.io: Python MCP Proxy               │
│  (toolbridge-mcp-staging.fly.dev)       │
│  - Validates JWT                        │
│  - Generates signed tenant headers      │
│  - Forwards requests to K8s Go API      │
└──────────────┬──────────────────────────┘
               │ HTTPS
               │ Authorization + X-TB-* headers
               ▼
┌─────────────────────────────────────────┐
│  K8s: Go REST API                       │
│  (toolbridgeapi.erauner.dev)            │
│  - Validates JWT                        │
│  - Validates tenant headers             │
│  - Executes business logic              │
└──────────────┬──────────────────────────┘
               │
               ▼
          PostgreSQL (CloudNativePG in K8s)
```

## 🔒 Security Checklist (Critical!)

**Before deploying**, ensure you've completed these security steps:

- [ ] **Secrets exist in K8s** - Verify `kubectl get secret toolbridge-secret -n toolbridge` returns jwt-secret + tenant-header-secret
- [ ] **Secrets match Fly.io** - Both services must use the same TENANT_HEADER_SECRET
- [ ] **Secrets are rotated** - If this is a re-deployment after exposure, generate new secrets first
- [ ] **Local .env configured** - Copy `.env.example` to `.env` and fill in secrets for testing

**📘 Detailed Reference:** See [SECRETS-REFERENCE.md](./SECRETS-REFERENCE.md) for:
- How to generate secrets securely
- K8s secret structure and encryption (SOPS)
- Fly.io secrets management
- Secret rotation procedures
- Validation and troubleshooting

## Prerequisites

1. **Fly.io CLI installed:**
   ```bash
   # macOS
   brew install flyctl

   # Linux/WSL
   curl -L https://fly.io/install.sh | sh
   ```

2. **Fly.io account:**
   ```bash
   fly auth login
   ```

3. **K8s Go API running:**
   - Ensure `https://toolbridgeapi.erauner.dev` is accessible
   - Verify health check: `curl https://toolbridgeapi.erauner.dev/healthz`

4. **Secrets prepared:**
   - `TOOLBRIDGE_TENANT_HEADER_SECRET` from K8s deployment
   - Generate if new: `openssl rand -base64 32`

## Step 1: Pre-Deployment Testing

Test the MCP-only Docker image locally before deploying:

```bash
# Build the MCP-only image
docker build -f Dockerfile.mcp-only -t toolbridge-mcp:local .

# Run locally with test configuration
docker run --rm -p 8001:8001 \
  -e TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  -e TOOLBRIDGE_TENANT_ID="local-test-tenant" \
  -e TOOLBRIDGE_TENANT_HEADER_SECRET="test-secret-123" \
  -e TOOLBRIDGE_LOG_LEVEL="DEBUG" \
  toolbridge-mcp:local

# In another terminal, test the service
curl http://localhost:8001/
# Expected: 404 or JSON response (not connection refused)

# Test with MCP inspector (if available)
npx @modelcontextprotocol/inspector http://localhost:8001
```

## Step 2: Create Fly.io App

```bash
# Create the staging app
fly apps create toolbridge-mcp-staging --org personal

# Verify app was created
fly apps list | grep toolbridge-mcp-staging
```

## Step 3: Configure Secrets

Set all required secrets on the Fly.io app:

```bash
# Required: External Go API URL (your K8s ingress)
fly secrets set \
  TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-mcp-staging

# Required: Tenant identification
fly secrets set \
  TOOLBRIDGE_TENANT_ID="staging-tenant-001" \
  -a toolbridge-mcp-staging

# Required: Tenant header secret (MUST match K8s TENANT_HEADER_SECRET)
# Get this from your K8s secret or generate a new one
fly secrets set \
  TOOLBRIDGE_TENANT_HEADER_SECRET="<same-as-k8s-tenant-header-secret>" \
  -a toolbridge-mcp-staging

# Optional: Logging level (default: INFO)
fly secrets set \
  TOOLBRIDGE_LOG_LEVEL="DEBUG" \
  -a toolbridge-mcp-staging
```

**Important:** The `TOOLBRIDGE_TENANT_HEADER_SECRET` **must** match the `TENANT_HEADER_SECRET` configured in your K8s deployment. Check the K8s secret:

```bash
# Get the secret from K8s (adjust namespace/secret name)
kubectl get secret toolbridge-secret -n toolbridge -o jsonpath='{.data.tenant-header-secret}' | base64 -d
```

If the secret doesn't exist in K8s yet, you need to:

1. Generate one: `openssl rand -base64 32`
2. Add it to K8s secret
3. Redeploy the K8s Go API
4. Then set the same value in Fly.io

## Step 4: Deploy to Fly.io

```bash
# Deploy using the staging configuration
fly deploy --config fly.staging.toml -a toolbridge-mcp-staging

# Watch the deployment
fly logs -a toolbridge-mcp-staging
```

Expected output in logs:
```
INFO:     Started server process [1]
INFO:     Waiting for application startup.
INFO:     Application startup complete.
INFO:     Uvicorn running on http://0.0.0.0:8001
```

## Step 5: Verify Deployment

### Check App Status

```bash
# Get app info
fly status -a toolbridge-mcp-staging

# Get public URL
APP_URL=$(fly info -a toolbridge-mcp-staging --json | jq -r '.Hostname')
echo "App URL: https://${APP_URL}"
```

### Test Health Check

```bash
# Test basic connectivity
curl https://${APP_URL}/

# Should return 404 or FastMCP index (not connection refused/502)
```

### Test with MCP Inspector

```bash
# Install MCP inspector (if not already installed)
npm install -g @modelcontextprotocol/inspector

# Connect to staging MCP service
npx @modelcontextprotocol/inspector https://${APP_URL}

# In the inspector UI:
# 1. Click "Tools" tab
# 2. You should see all 40 ToolBridge tools listed
# 3. Try "health_check" tool (doesn't require auth)
```

### Test Tool Execution

Create a test script to validate tool execution:

```bash
# Create test-staging.py
cat > test-staging.py << 'EOF'
import httpx
import jwt
import time
from datetime import datetime, timedelta

# Configuration
MCP_URL = "https://toolbridge-mcp-staging.fly.dev"
JWT_SECRET = "dev-secret"  # Replace with actual secret if using HS256
USER_ID = f"staging-test-{int(time.time())}"
TENANT_ID = "staging-tenant-001"

def generate_jwt_token(user_id: str, tenant_id: str) -> str:
    """Generate JWT token for testing."""
    payload = {
        "sub": user_id,
        "tenant_id": tenant_id,
        "iat": datetime.utcnow(),
        "exp": datetime.utcnow() + timedelta(hours=1)
    }
    return jwt.encode(payload, JWT_SECRET, algorithm="HS256")

async def test_health_check():
    """Test health_check tool."""
    token = generate_jwt_token(USER_ID, TENANT_ID)

    async with httpx.AsyncClient() as client:
        # Test health check endpoint
        response = await client.get(
            f"{MCP_URL}/",
            headers={"Authorization": f"Bearer {token}"}
        )
        print(f"Status: {response.status_code}")
        print(f"Response: {response.text[:200]}")

if __name__ == "__main__":
    import asyncio
    asyncio.run(test_health_check())
EOF

# Run the test
python test-staging.py
```

## Step 6: Integration Testing

Verify the MCP service can communicate with the K8s Go API:

```bash
# Check logs for any connection errors
fly logs -a toolbridge-mcp-staging | grep -i error

# Test note creation flow (requires valid JWT)
# Adapt scripts/test-mcp-integration.py to point to staging:
export MCP_SSE_URL="https://toolbridge-mcp-staging.fly.dev"
export GO_API_URL="https://toolbridgeapi.erauner.dev"
export JWT_SECRET="<your-jwt-secret>"

python scripts/test-mcp-integration.py
```

## Step 7: Load Testing (Optional)

Test concurrent request handling:

```bash
# Install k6 (load testing tool)
brew install k6  # macOS
# or download from https://k6.io/

# Create load test script
cat > load-test.js << 'EOF'
import http from 'k6/http';
import { check, sleep } from 'k6';

export let options = {
  stages: [
    { duration: '30s', target: 10 },   // Ramp up to 10 users
    { duration: '1m', target: 10 },    // Stay at 10 users
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<200'],  // 95% of requests < 200ms
  },
};

export default function () {
  const url = 'https://toolbridge-mcp-staging.fly.dev/';
  const params = {
    headers: {
      'Authorization': 'Bearer test-token',
    },
  };

  let res = http.get(url, params);
  check(res, {
    'status is 200 or 404': (r) => r.status === 200 || r.status === 404,
  });

  sleep(1);
}
EOF

# Run load test
k6 run load-test.js

# Monitor during test
fly metrics -a toolbridge-mcp-staging
```

## Monitoring

### View Real-Time Logs

```bash
# Follow all logs
fly logs -a toolbridge-mcp-staging

# Filter for errors only
fly logs -a toolbridge-mcp-staging | grep -i error

# Search for specific tenant
fly logs -a toolbridge-mcp-staging | grep "tenant_id"
```

### Check Resource Usage

```bash
# View metrics dashboard
fly dashboard metrics -a toolbridge-mcp-staging

# Check VM status
fly status -a toolbridge-mcp-staging

# View resource usage
fly metrics -a toolbridge-mcp-staging
```

### Health Monitoring

Set up automated health checks:

```bash
# Using Uptime Robot, Better Uptime, or similar service
# Monitor: https://toolbridge-mcp-staging.fly.dev/
# Expected: 200 or 404 (not 502/503)
```

## Scaling

### Vertical Scaling (VM Size)

```bash
# Scale to larger VM
fly scale vm shared-cpu-2x --memory 1024 -a toolbridge-mcp-staging

# Scale back down
fly scale vm shared-cpu-1x --memory 512 -a toolbridge-mcp-staging
```

### Horizontal Scaling (Instance Count)

```bash
# Scale to 2 instances
fly scale count 2 -a toolbridge-mcp-staging

# Scale back to 1
fly scale count 1 -a toolbridge-mcp-staging

# Enable auto-scaling (experimental)
fly autoscale set min=1 max=5 -a toolbridge-mcp-staging
```

## Updating Deployment

### Deploy Code Changes

```bash
# After making changes to mcp/ directory
fly deploy --config fly.staging.toml -a toolbridge-mcp-staging

# Monitor deployment
fly logs -a toolbridge-mcp-staging
```

### Update Secrets

```bash
# Update a secret
fly secrets set TOOLBRIDGE_LOG_LEVEL="INFO" -a toolbridge-mcp-staging

# Note: Updating secrets triggers a restart
```

### Rollback

```bash
# List recent releases
fly releases -a toolbridge-mcp-staging

# Rollback to previous version
fly releases rollback -a toolbridge-mcp-staging
```

## Troubleshooting

### Service Won't Start

**Symptom:** Fly logs show `Error: Connection refused` or container crashes

**Solutions:**
1. Check Dockerfile builds locally:
   ```bash
   docker build -f Dockerfile.mcp-only -t test .
   docker run --rm test
   ```

2. Verify all required secrets are set:
   ```bash
   fly secrets list -a toolbridge-mcp-staging
   ```

3. Check health check configuration in `fly.staging.toml`

### Can't Connect to K8s Go API

**Symptom:** Logs show `httpx.ConnectError` or timeout errors

**Solutions:**
1. Verify K8s API is accessible from external network:
   ```bash
   curl https://toolbridgeapi.erauner.dev/healthz
   ```

2. Check `TOOLBRIDGE_GO_API_BASE_URL` secret is correct
3. Verify firewall/network policies allow Fly.io → K8s

### Tenant Header Validation Fails

**Symptom:** Go API returns 401/403 errors

**Solutions:**
1. Verify `TOOLBRIDGE_TENANT_HEADER_SECRET` matches K8s:
   ```bash
   # Get from K8s
   kubectl get secret toolbridge-secret -n toolbridge -o jsonpath='{.data.tenant-header-secret}' | base64 -d

   # Compare with Fly.io (can't see actual value, but can update)
   fly secrets set TOOLBRIDGE_TENANT_HEADER_SECRET="<k8s-value>" -a toolbridge-mcp-staging
   ```

2. Check timestamp skew (max 5 minutes allowed)
3. Enable DEBUG logging to see signature computation:
   ```bash
   fly secrets set TOOLBRIDGE_LOG_LEVEL="DEBUG" -a toolbridge-mcp-staging
   fly logs -a toolbridge-mcp-staging | grep signature
   ```

### High Latency

**Symptom:** Tool calls take >500ms

**Solutions:**
1. Check Fly region vs K8s cluster location:
   ```bash
   fly regions list
   fly regions set ord -a toolbridge-mcp-staging  # Set to nearest region
   ```

2. Scale up VM size:
   ```bash
   fly scale vm shared-cpu-2x --memory 1024 -a toolbridge-mcp-staging
   ```

3. Enable HTTP/2 and connection pooling (already enabled in httpx)

### Memory Issues

**Symptom:** Container OOM kills

**Solutions:**
1. Scale VM memory:
   ```bash
   fly scale vm shared-cpu-1x --memory 1024 -a toolbridge-mcp-staging
   ```

2. Check for memory leaks in logs
3. Review httpx client connection pooling

## Security Checklist

- [ ] `TOOLBRIDGE_TENANT_HEADER_SECRET` matches K8s secret
- [ ] JWT validation working (test with invalid token)
- [ ] No sensitive data in logs (check `fly logs`)
- [ ] HTTPS enforced (check `fly.staging.toml` force_https)
- [ ] Tenant isolation verified (test cross-tenant access)
- [ ] Secrets not committed to git (check `.gitignore`)

## Success Criteria

Before considering deployment complete:

- [ ] ✅ App deployed and healthy (`fly status` shows "running")
- [ ] ✅ Health checks passing (check Fly dashboard)
- [ ] ✅ Can list MCP tools via inspector
- [ ] ✅ At least 3 tools tested successfully
- [ ] ✅ Logs show successful Go API requests
- [ ] ✅ No errors in last 100 log lines
- [ ] ✅ Latency p95 < 200ms (check metrics)
- [ ] ✅ Documentation updated
- [ ] ✅ Secrets documented in secure location

## Multi-Tenant Deployment

To deploy per-tenant apps (future):

```bash
# Create tenant-specific app
fly apps create toolbridge-tenant-abc123

# Copy fly.staging.toml to fly.tenant-abc123.toml
cp fly.staging.toml fly.tenant-abc123.toml

# Edit fly.tenant-abc123.toml:
# - Change app name to "toolbridge-tenant-abc123"
# - Update TOOLBRIDGE_TENANT_ID

# Set secrets
fly secrets set \
  TOOLBRIDGE_TENANT_ID="abc123" \
  TOOLBRIDGE_TENANT_HEADER_SECRET="<tenant-specific-secret>" \
  TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-tenant-abc123

# Deploy
fly deploy --config fly.tenant-abc123.toml -a toolbridge-tenant-abc123
```

## References

- **Fly.io Docs:** https://fly.io/docs/
- **MCP Specification:** https://modelcontextprotocol.io/
- **ToolBridge Architecture:** `docs/SPEC-FASTMCP-INTEGRATION.md`
- **Local Development:** `docs/QUICKSTART-MCP.md`
- **K8s Deployment:** `../homelab-k8s/apps/toolbridge-api/README.md`

## Support

- Check logs first: `fly logs -a toolbridge-mcp-staging`
- Review troubleshooting section above
- Verify K8s Go API is healthy
- Test locally with Docker before deploying
