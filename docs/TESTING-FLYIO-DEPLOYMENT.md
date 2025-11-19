# Testing Fly.io MCP Deployment - Step-by-Step Guide

This guide walks you through testing the complete Fly.io MCP-only deployment manually.

## Current Status

✅ **K8s Go API:** Running and healthy at `https://toolbridgeapi.erauner.dev`  
❌ **Tenant Secret:** Missing `tenant-header-secret` in K8s secret - **MUST ADD FIRST**  
⏳ **Fly.io App:** Not yet created

## Prerequisites Checklist

- [x] K8s cluster access configured
- [x] Go API deployed and healthy
- [ ] `tenant-header-secret` added to K8s secret
- [ ] Fly.io CLI installed (`brew install flyctl`)
- [ ] Fly.io account authenticated (`fly auth login`)
- [ ] Docker installed (for local testing)

## Phase 1: Add Tenant Secret to K8s (CRITICAL)

### Why This is Required

The MCP service uses HMAC-signed tenant headers to authenticate with the Go API. Both services must share the same secret. Currently, your K8s secret has these keys:

```
Available keys in toolbridge-secret:
- database-url
- jwt-secret
- password
- username
```

**Missing:** `tenant-header-secret`

### Step 1.1: Generate the Secret

```bash
# Generate a secure random secret
TENANT_SECRET=$(openssl rand -base64 32)
echo "Generated secret: $TENANT_SECRET"

# SAVE THIS VALUE! You'll need it for Fly.io later
echo "$TENANT_SECRET" > /tmp/tenant-secret.txt
```

**Generated for you:**
```
<TENANT_HEADER_SECRET_FROM_K8S>
```

### Step 1.2: Add to K8s SOPS Secret

```bash
# Navigate to the K8s repo
cd /Users/erauner/git/side/homelab-k8s/apps/toolbridge-api/production-overlays

# Edit the SOPS-encrypted secret
sops toolbridge-secret.sops.yaml
```

Add this line under `stringData:`:

```yaml
stringData:
  # ... existing keys ...
  tenant-header-secret: "<TENANT_HEADER_SECRET_FROM_K8S>"
```

### Step 1.3: Commit and Sync

```bash
# Commit the change (file stays encrypted)
git add toolbridge-secret.sops.yaml
git commit -m "chore: add tenant-header-secret for MCP authentication"
git push

# Sync ArgoCD
argocd app sync toolbridge-api

# Wait for rollout
kubectl rollout status deployment/toolbridge-api -n toolbridge
```

### Step 1.4: Verify Secret Updated

```bash
# Check the secret now has the new key
kubectl get secret toolbridge-secret -n toolbridge -o jsonpath='{.data.tenant-header-secret}' | base64 -d
# Should output: <TENANT_HEADER_SECRET_FROM_K8S>
```

## Phase 2: Test Docker Build Locally

Before deploying to Fly.io, test the MCP-only Docker image locally.

### Step 2.1: Build the Image

```bash
cd /Users/erauner/git/side/toolbridge-api

# Build MCP-only image
docker build -f Dockerfile.mcp-only -t toolbridge-mcp:local .
```

**Expected output:**
```
[+] Building 45.3s (12/12) FINISHED
 => [internal] load build definition from Dockerfile.mcp-only
 => => transferring dockerfile: 1.23kB
 ...
 => => writing image sha256:...
```

### Step 2.2: Run Locally

```bash
# Start container
docker run --rm -d --name toolbridge-mcp-test \
  -p 8001:8001 \
  -e TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  -e TOOLBRIDGE_TENANT_ID="local-test-tenant" \
  -e TOOLBRIDGE_TENANT_HEADER_SECRET="<TENANT_HEADER_SECRET_FROM_K8S>" \
  -e TOOLBRIDGE_LOG_LEVEL="DEBUG" \
  toolbridge-mcp:local

# Check logs
docker logs -f toolbridge-mcp-test
```

**Expected logs:**
```
INFO:     Started server process [1]
INFO:     Waiting for application startup.
INFO:     Application startup complete.
INFO:     Uvicorn running on http://0.0.0.0:8001
```

### Step 2.3: Test Health

```bash
# Test basic connectivity
curl http://localhost:8001/

# Expected: 404 or JSON response (not connection refused)
```

### Step 2.4: Stop Test Container

```bash
docker stop toolbridge-mcp-test
```

## Phase 3: Deploy to Fly.io

### Option A: Automated Deployment (Recommended)

Use the deployment helper script:

```bash
cd /Users/erauner/git/side/toolbridge-api
./scripts/deploy-mcp-flyio.sh
```

The script will:
1. ✓ Verify prerequisites
2. ✓ Check K8s secret (should pass now)
3. ? Ask if you want to test Docker locally
4. ✓ Create Fly.io app
5. ✓ Set secrets
6. ? Ask if you want to deploy
7. ✓ Verify deployment
8. ? Ask if you want to run tests

### Option B: Manual Deployment

If you prefer manual control:

#### Step 3.1: Create Fly.io App

```bash
# Create the app
fly apps create toolbridge-mcp-staging --org personal

# Verify
fly apps list | grep toolbridge-mcp-staging
```

#### Step 3.2: Set Secrets

```bash
# Set all required secrets
fly secrets set \
  TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  TOOLBRIDGE_TENANT_ID="staging-tenant-001" \
  TOOLBRIDGE_TENANT_HEADER_SECRET="<TENANT_HEADER_SECRET_FROM_K8S>" \
  TOOLBRIDGE_LOG_LEVEL="INFO" \
  -a toolbridge-mcp-staging
```

**Critical:** The `TOOLBRIDGE_TENANT_HEADER_SECRET` **MUST** match the value in your K8s secret!

#### Step 3.3: Deploy

```bash
# Deploy using the staging configuration
fly deploy --config fly.staging.toml -a toolbridge-mcp-staging

# Watch the logs
fly logs -a toolbridge-mcp-staging
```

**Expected deployment output:**
```
==> Verifying app config
--> Verified app config
==> Building image
...
==> Pushing image to fly
...
--> Pushing image done
==> Creating release
...
--> Release v1 created
```

## Phase 4: Verify Deployment

### Step 4.1: Check Status

```bash
# Get app status
fly status -a toolbridge-mcp-staging
```

**Expected output:**
```
App
  Name     = toolbridge-mcp-staging
  Owner    = personal
  Hostname = toolbridge-mcp-staging.fly.dev
  ...

Machines
PROCESS ID              VERSION REGION  STATE   HEALTH CHECKS   ...
app     ... v1      ord     started 1 total      ...
```

### Step 4.2: Check Logs

```bash
# View recent logs
fly logs -a toolbridge-mcp-staging

# Follow logs in real-time
fly logs -a toolbridge-mcp-staging -f
```

**Look for:**
- ✅ `INFO:     Uvicorn running on http://0.0.0.0:8001`
- ✅ No connection errors
- ✅ No authentication errors

### Step 4.3: Test Health Endpoint

```bash
# Test the deployed service
curl https://toolbridge-mcp-staging.fly.dev/
```

**Expected:** 404 or JSON response (not 502/503)

## Phase 5: Run Integration Tests

### Step 5.1: Update JWT Secret

First, get your JWT secret from K8s:

```bash
kubectl get secret toolbridge-secret -n toolbridge \
  -o jsonpath='{.data.jwt-secret}' | base64 -d
```

### Step 5.2: Run Test Script

```bash
cd /Users/erauner/git/side/toolbridge-api

export MCP_BASE_URL="https://toolbridge-mcp-staging.fly.dev"
export GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"
export JWT_SECRET="<your-jwt-secret-from-above>"
export TENANT_ID="staging-tenant-001"

python3 scripts/test-mcp-staging.py
```

**Expected output:**
```
========================================
ToolBridge MCP Staging Integration Tests
========================================

Test 1: MCP Service Health Check
--------------------------------------------------
✓ MCP service is healthy
Status: 200
...

Test 2: Direct Go API Access
--------------------------------------------------
✓ Go API is accessible
...

Test 3: End-to-End Flow (MCP → Go API)
--------------------------------------------------
✓ Created test note via MCP
✓ Retrieved note from Go API
...

Test 4: Latency Test
--------------------------------------------------
Avg: 123ms, p95: 178ms, p99: 234ms
✓ All tests passed!
```

### Step 5.3: Test with MCP Inspector (Optional)

```bash
# Install MCP inspector
npm install -g @modelcontextprotocol/inspector

# Connect to your deployed service
npx @modelcontextprotocol/inspector https://toolbridge-mcp-staging.fly.dev
```

In the inspector UI:
1. Click "Tools" tab
2. You should see ~40 ToolBridge tools
3. Try the `list_notes` tool
4. Verify it returns data (or empty list if no notes)

## Phase 6: Monitor and Scale

### Monitoring

```bash
# View metrics
fly dashboard metrics -a toolbridge-mcp-staging

# Watch resource usage
fly metrics -a toolbridge-mcp-staging

# SSH into container (if needed)
fly ssh console -a toolbridge-mcp-staging
```

### Scaling

```bash
# Scale to larger VM
fly scale vm shared-cpu-2x --memory 1024 -a toolbridge-mcp-staging

# Scale to multiple instances
fly scale count 2 -a toolbridge-mcp-staging

# Enable auto-scaling
fly autoscale set min=1 max=5 -a toolbridge-mcp-staging
```

## Troubleshooting

### Issue: "tenant-header-secret not found"

**Cause:** K8s secret missing the key  
**Solution:** Complete Phase 1 above

### Issue: "Invalid tenant headers" in Go API logs

**Cause:** Secret mismatch between Fly.io and K8s  
**Solution:**
```bash
# Verify K8s secret
kubectl get secret toolbridge-secret -n toolbridge \
  -o jsonpath='{.data.tenant-header-secret}' | base64 -d

# Update Fly.io secret to match
fly secrets set TOOLBRIDGE_TENANT_HEADER_SECRET="<k8s-value>" \
  -a toolbridge-mcp-staging
```

### Issue: "Connection refused" to Go API

**Cause:** Incorrect GO_API_BASE_URL or network issue  
**Solution:**
```bash
# Test Go API directly
curl https://toolbridgeapi.erauner.dev/healthz

# Update Fly.io secret if URL is wrong
fly secrets set TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-mcp-staging
```

### Issue: High latency (>500ms)

**Cause:** VM too small or wrong region  
**Solution:**
```bash
# Check current region
fly regions list -a toolbridge-mcp-staging

# Set to nearest region to K8s
fly regions set ord -a toolbridge-mcp-staging

# Scale up VM
fly scale vm shared-cpu-2x --memory 1024 -a toolbridge-mcp-staging
```

## Success Criteria

Before considering deployment complete:

- [ ] ✅ K8s secret has `tenant-header-secret` key
- [ ] ✅ Docker image builds successfully locally
- [ ] ✅ Local container runs without errors
- [ ] ✅ Fly.io app created and deployed
- [ ] ✅ App status shows "started" and healthy
- [ ] ✅ No errors in last 100 log lines
- [ ] ✅ Health endpoint returns 2xx or 404
- [ ] ✅ Integration tests pass (all 4 tests)
- [ ] ✅ Can list MCP tools via inspector
- [ ] ✅ Latency p95 < 500ms

## Quick Reference

### Common Commands

```bash
# Check app status
fly status -a toolbridge-mcp-staging

# View logs
fly logs -a toolbridge-mcp-staging

# Update secrets
fly secrets set KEY="value" -a toolbridge-mcp-staging

# Redeploy
fly deploy --config fly.staging.toml -a toolbridge-mcp-staging

# Rollback
fly releases rollback -a toolbridge-mcp-staging

# SSH into container
fly ssh console -a toolbridge-mcp-staging
```

### Important URLs

- **Deployed MCP Service:** https://toolbridge-mcp-staging.fly.dev
- **K8s Go API:** https://toolbridgeapi.erauner.dev
- **Fly.io Dashboard:** https://fly.io/apps/toolbridge-mcp-staging

### Critical Secrets

Save these securely (e.g., 1Password):

```
TOOLBRIDGE_TENANT_HEADER_SECRET=<TENANT_HEADER_SECRET_FROM_K8S>
TOOLBRIDGE_TENANT_ID=staging-tenant-001
TOOLBRIDGE_GO_API_BASE_URL=https://toolbridgeapi.erauner.dev
```

## Next Steps

After successful deployment:

1. **Load Testing:** Run k6 tests (see `docs/DEPLOYMENT-FLYIO.md`)
2. **Security Validation:** Test tenant isolation and JWT validation
3. **Configure Claude Desktop:** Add MCP server to Claude
4. **Production Deployment:** Create `fly.production.toml`
5. **Multi-Tenant Setup:** Deploy per-tenant apps if needed

## Support

If you encounter issues:

1. Check logs: `fly logs -a toolbridge-mcp-staging`
2. Verify K8s API: `curl https://toolbridgeapi.erauner.dev/healthz`
3. Check secrets match between K8s and Fly.io
4. Review troubleshooting section above
5. See `docs/DEPLOYMENT-FLYIO.md` for detailed guidance
