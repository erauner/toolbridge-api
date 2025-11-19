# MCP-Only Deployment to Fly.io Plan

## Architecture Overview

```
┌──────────────────┐
│  LLM Client      │  (Claude Desktop, VS Code)
└────────┬─────────┘
         │ MCP Protocol (SSE)
         ▼
┌──────────────────────────────┐
│  Python MCP Server           │  ← Deploy to Fly.io
│  (Fly.io)                    │     (stateless proxy)
└────────┬─────────────────────┘
         │ HTTPS REST calls
         │ + Signed tenant headers
         ▼
┌──────────────────────────────┐
│  Go REST API                 │  ← Already deployed
│  (Kubernetes)                │     (your existing service)
└────────┬─────────────────────┘
         │
         ▼
┌──────────────────────────────┐
│  PostgreSQL                  │  ← Already deployed
│  (K8s or managed)            │     (your existing database)
└──────────────────────────────┘
```

## What We're Deploying

**ONLY the Python MCP service to Fly.io**

- ✅ Stateless proxy (no database)
- ✅ Connects to your existing K8s Go API
- ✅ Python-only Docker container (~500MB, 512MB RAM)
- ✅ Auto-scales to zero when idle
- ✅ Simple deployment (no supervisor, no multi-process)

## Prerequisites

### 1. Information You Need

Before starting, gather:

- [ ] **Your K8s Go API URL**
  - Example: `https://toolbridge-api.yourcluster.com`
  - Or: `https://api.example.com`
  - Must be accessible from internet (Fly.io → K8s)

- [ ] **Tenant Configuration**
  - Tenant ID (e.g., `staging-tenant-001`)
  - Tenant header secret (generate new or use existing)

- [ ] **Kubernetes Go API Configuration**
  - Is `TENANT_HEADER_SECRET` already set in your K8s deployment?
  - If not, you'll need to add it for the MCP integration to work

### 2. Tools Installed

- [ ] Fly.io CLI: `brew install flyctl` or `curl -L https://fly.io/install.sh | sh`
- [ ] Docker (for local testing)
- [ ] Git (already have this)

### 3. Accounts & Access

- [ ] Fly.io account created and logged in: `fly auth login`
- [ ] Access to your K8s cluster (to update Go API config if needed)

## Deployment Steps

### Step 1: Update K8s Go API Configuration

**If not already done**, ensure your K8s Go API has tenant header validation enabled:

```yaml
# In your K8s deployment manifest for the Go API
env:
  - name: TENANT_HEADER_SECRET
    valueFrom:
      secretKeyRef:
        name: toolbridge-secrets
        key: tenant-header-secret
```

**Generate secret if needed:**
```bash
openssl rand -base64 32
# Save this - you'll use it for both K8s and Fly.io
```

**Apply to K8s:**
```bash
kubectl create secret generic toolbridge-secrets \
  --from-literal=tenant-header-secret="<your-generated-secret>"

kubectl rollout restart deployment/toolbridge-api
```

### Step 2: Test Local Connection to K8s API

Verify MCP can reach your K8s API:

```bash
# Test from local machine
curl https://your-k8s-api.example.com/health

# Should return 200 OK
```

### Step 3: Create Fly.io App

```bash
# Create app (choose a name)
fly apps create toolbridge-mcp-staging

# Optionally set region (default: closest to you)
fly apps create toolbridge-mcp-staging --region ord  # Chicago
# Other regions: iad (Virginia), lhr (London), syd (Sydney), etc.
```

### Step 4: Configure Secrets

Set all required environment variables:

```bash
# Point to your K8s API
fly secrets set \
  TOOLBRIDGE_GO_API_BASE_URL="https://your-k8s-api.example.com" \
  --app toolbridge-mcp-staging

# Tenant configuration (must match K8s secret)
fly secrets set \
  TOOLBRIDGE_TENANT_ID="staging-tenant-001" \
  TOOLBRIDGE_TENANT_HEADER_SECRET="<same-secret-as-k8s>" \
  --app toolbridge-mcp-staging

# Optional: Increase log level for initial testing
fly secrets set \
  TOOLBRIDGE_LOG_LEVEL="DEBUG" \
  --app toolbridge-mcp-staging
```

### Step 5: Deploy to Fly.io

```bash
# Deploy using the fly.toml config
fly deploy --config fly.toml --app toolbridge-mcp-staging

# Watch deployment logs
fly logs --app toolbridge-mcp-staging
```

**Expected output:**
```
2025-01-15 10:00:00 | INFO | ToolBridge MCP server initialized with 40 tools
2025-01-15 10:00:01 | INFO | TenantDirectTransport initialized: tenant_id=staging-tenant-001
2025-01-15 10:00:02 | INFO | Uvicorn running on http://0.0.0.0:8001
```

### Step 6: Verify Deployment

```bash
# Get app URL
APP_URL=$(fly info --app toolbridge-mcp-staging --json | jq -r '.Hostname')
echo "MCP Service: https://${APP_URL}"

# Test health endpoint
curl https://${APP_URL}/

# Should return FastMCP server info
```

### Step 7: Test MCP Tools

```bash
# Install MCP Inspector
npm install -g @modelcontextprotocol/inspector

# Connect to your deployed MCP service
npx @modelcontextprotocol/inspector https://${APP_URL}/sse
```

**In the Inspector UI:**
1. Click "List Tools" - should see all 40 tools
2. Generate a test JWT token (use your K8s auth method)
3. Try calling `health_check` tool
4. Try calling `list_notes` tool with JWT

### Step 8: Network Verification

**Test the complete flow:**

```bash
# From your local machine, simulate LLM client

# 1. Generate JWT (use your auth system)
JWT_TOKEN="eyJ..."  # Your valid JWT token

# 2. Call MCP tool
curl -X POST "https://${APP_URL}/sse" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${JWT_TOKEN}" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "health_check",
      "arguments": {}
    }
  }'
```

**Expected flow:**
1. Fly.io MCP receives request
2. MCP creates session with K8s API
3. MCP calls K8s API with signed headers
4. K8s API validates JWT + tenant headers
5. K8s API returns response
6. MCP forwards response to client

### Step 9: Monitor & Debug

```bash
# Watch live logs
fly logs --app toolbridge-mcp-staging

# Check app status
fly status --app toolbridge-mcp-staging

# Check metrics
fly metrics --app toolbridge-mcp-staging

# SSH into container (if needed)
fly ssh console --app toolbridge-mcp-staging
```

**Look for in logs:**
- ✅ "ToolBridge MCP server initialized"
- ✅ "TenantDirectTransport initialized"
- ✅ Session creation logs when tools are called
- ❌ Connection refused errors (K8s API unreachable)
- ❌ Signature validation failures (secret mismatch)

## Troubleshooting

### Issue: MCP can't reach K8s API

**Symptoms:**
```
Connection refused to https://your-k8s-api.example.com
```

**Check:**
1. Is K8s API accessible from internet? (Not internal-only)
2. Is there a firewall blocking Fly.io IP ranges?
3. Try from another external source: `curl https://your-k8s-api.example.com/health`

**Fix:**
- Update K8s Ingress/Service to allow external traffic
- Add Fly.io IP ranges to allowlist if using IP filtering

### Issue: Signature validation failures

**Symptoms:**
```
401 Unauthorized: Invalid tenant header signature
```

**Check:**
1. Are secrets identical in Fly.io and K8s?
2. Is `TENANT_HEADER_SECRET` set in K8s Go API?
3. Are clock times in sync? (NTP)

**Fix:**
```bash
# Verify secrets match
fly ssh console --app toolbridge-mcp-staging
echo $TOOLBRIDGE_TENANT_HEADER_SECRET

# Compare with K8s
kubectl get secret toolbridge-secrets -o jsonpath='{.data.tenant-header-secret}' | base64 -d
```

### Issue: Tools not appearing

**Symptoms:**
```
tools/list returns empty array or fewer than 40 tools
```

**Check:**
1. Check MCP server logs for import errors
2. Verify all Python dependencies installed

**Fix:**
```bash
fly logs --app toolbridge-mcp-staging | grep -i error
```

### Issue: Session creation fails

**Symptoms:**
```
Failed to create session: 401 Unauthorized
```

**Check:**
1. Is JWT token valid?
2. Does K8s API accept the JWT?
3. Is `X-Debug-Sub` header needed for dev mode?

**Fix:**
- Test JWT against K8s API directly: `curl -H "Authorization: Bearer $JWT" https://your-k8s-api.example.com/v1/sync/sessions`

## Success Criteria

Before moving to production:

- [ ] ✅ Fly.io app deployed and healthy
- [ ] ✅ MCP service accessible via HTTPS
- [ ] ✅ All 40 tools discoverable via `tools/list`
- [ ] ✅ Can create sessions with K8s API
- [ ] ✅ Dual authentication working (JWT + tenant headers)
- [ ] ✅ At least 5 different tools tested successfully
- [ ] ✅ No errors in logs for 24 hours
- [ ] ✅ Latency acceptable (<500ms for tool calls)
- [ ] ✅ Tested with real LLM client (Claude Desktop or VS Code)

## Cost Estimate

**Fly.io Pricing (as of 2025):**
- Shared CPU 1x, 512MB RAM: ~$5-10/month
- With auto-scale to zero: Even less (billed per second)
- Bandwidth: Included for reasonable usage
- Storage: Not needed (stateless)

**Total estimated cost:** ~$5-15/month for staging

## Files Created

For this deployment, we have:

1. **`fly.toml`** - Fly.io configuration
   - App name, region, VM size
   - Environment variables
   - Health checks
   - Secrets documentation

2. **`mcp/Dockerfile`** - Python-only container
   - Based on `python:3.11-slim`
   - Installs uv + dependencies
   - Runs MCP server on port 8001
   - Health check included

## Next Steps After Deployment

1. **Test with Claude Desktop**
   - Configure MCP server in Claude settings
   - Test natural language interactions
   - Verify all CRUD operations work

2. **Performance Testing**
   - Load test with 10-50 concurrent requests
   - Measure latency (target: <200ms p95)
   - Check auto-scaling behavior

3. **Production Deployment**
   - Create production app: `toolbridge-mcp-production`
   - Use production secrets
   - Point to production K8s API
   - Set up monitoring/alerts

4. **Multi-Tenant Scaling**
   - Create one Fly.io app per tenant
   - Automate with Terraform or Pulumi
   - Implement tenant routing

## Security Considerations

### Network Security

- ✅ Fly.io → K8s traffic encrypted (HTTPS)
- ✅ MCP enforces dual authentication
- ✅ Tenant headers prevent cross-tenant access
- ⚠️ Ensure K8s API has rate limiting
- ⚠️ Consider IP allowlisting if needed

### Secrets Management

- ✅ Secrets stored in Fly.io secrets (encrypted at rest)
- ✅ Secrets not in git or logs
- ⚠️ Rotate secrets every 90 days
- ⚠️ Use different secrets for staging vs production

### JWT Validation

- ✅ K8s API validates JWT signatures
- ✅ MCP forwards JWT without modification
- ⚠️ Ensure JWT has appropriate expiration (not too long)
- ⚠️ Consider refresh token flow for long-lived sessions

## Rollback Plan

If deployment fails or issues found:

```bash
# Destroy app completely
fly apps destroy toolbridge-mcp-staging

# Or scale down to zero
fly scale count 0 --app toolbridge-mcp-staging

# Or rollback to previous version
fly releases --app toolbridge-mcp-staging
fly releases rollback <version-number> --app toolbridge-mcp-staging
```

## Questions to Answer

Before deploying, clarify:

1. **What's your K8s API URL?** (needed for TOOLBRIDGE_GO_API_BASE_URL)
2. **Is TENANT_HEADER_SECRET already set in K8s?** (or do we need to add it?)
3. **What region should we deploy to?** (closest to K8s for lower latency)
4. **What tenant ID should we use?** (staging-tenant-001 or different?)
5. **Do you want auto-scale to zero?** (saves money, adds cold start)

## Estimated Time

- **If K8s already has tenant headers:** 30-60 minutes
- **If need to update K8s config:** 1-2 hours
- **Testing and validation:** 1-2 hours
- **Total:** 2-4 hours

---

**Status:** Planning phase
**Next Action:** Gather prerequisites (K8s API URL, secrets, etc.)
**Blocker:** None (all code is ready, just need deployment info)
