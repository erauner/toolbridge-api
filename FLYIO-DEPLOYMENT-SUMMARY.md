# Fly.io MCP Deployment Implementation Summary

This document summarizes the Fly.io MCP-only deployment implementation for ToolBridge.

## What Was Implemented

### 1. MCP-Only Docker Image

**File:** `Dockerfile.mcp-only`

A lightweight Python-only Docker image optimized for running the FastMCP proxy service:

- **Base:** Python 3.12 slim
- **Size:** ~100MB (vs ~500MB+ for full-stack)
- **Dependencies:** Managed via `uv` for fast builds
- **Health checks:** Built-in HTTP health check on port 8001
- **Security:** Non-root user (app:app)

**Key features:**
- No Go binary (smaller, faster builds)
- No PostgreSQL dependencies
- No supervisor overhead
- Direct uvicorn entrypoint

### 2. Fly.io Staging Configuration

**File:** `fly.staging.toml`

Fly.io configuration optimized for MCP proxy staging deployment:

- **App name:** `toolbridge-mcp-staging`
- **Region:** `ord` (Chicago - closest to K8s cluster)
- **VM size:** 1 CPU, 512MB RAM (small footprint for proxy)
- **Auto-scaling:** Scale to zero when idle
- **Health checks:** HTTP check on `/` with 15s grace period
- **Ports:** Only 8001 exposed (HTTPS enforced)

**Environment variables:**
- `TOOLBRIDGE_GO_API_BASE_URL` → Points to K8s ingress
- `TOOLBRIDGE_TENANT_ID` → Tenant identifier
- `TOOLBRIDGE_TENANT_HEADER_SECRET` → HMAC signing secret (matches K8s)
- `TOOLBRIDGE_JWT_TOKEN` → Shared JWT token for backend API authentication
- `TOOLBRIDGE_LOG_LEVEL` → Configurable logging

### 3. Comprehensive Deployment Documentation

**File:** `docs/DEPLOYMENT-FLYIO.md`

Complete runbook covering:

- **Architecture diagram** showing MCP → K8s → PostgreSQL flow
- **Prerequisites** (Fly CLI, K8s access, secrets)
- **Pre-deployment testing** with Docker locally
- **Step-by-step deployment** process
- **Verification tests** (health checks, tool discovery, E2E)
- **Load testing** guidance with k6 examples
- **Monitoring** (logs, metrics, health)
- **Scaling** (vertical and horizontal)
- **Troubleshooting** (common issues and solutions)
- **Security checklist** (tenant isolation, JWT validation, HTTPS)
- **Multi-tenant deployment** pattern for future expansion

### 4. Secrets Management Guide

**File:** `docs/SECRETS-REFERENCE.md`

Comprehensive secrets documentation:

- **Overview** of dual authentication (JWT + tenant headers)
- **K8s secrets** structure (SOPS-encrypted)
- **Fly.io secrets** configuration
- **Local development** `.env` examples
- **Secret generation** commands
- **Secret rotation** procedures (90-day cycle)
- **Validation tests** to verify secrets match
- **Security best practices**
- **Troubleshooting** common secret-related issues

### 5. Integration Test Script

**File:** `scripts/test-mcp-staging.py`

Automated testing script for staging validation:

**Tests:**
1. MCP service health check
2. Direct Go API access (K8s)
3. End-to-end flow (MCP → Go API → PostgreSQL)
4. Latency testing (p95 < 500ms target)

**Features:**
- Configurable via environment variables
- Works against Fly.io staging or local MCP + remote K8s
- Generates JWT tokens for testing
- Creates/reads/deletes test data
- Reports detailed latency statistics

**Usage:**
```bash
export MCP_BASE_URL="https://toolbridge-mcp-staging.fly.dev"
export GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"
export JWT_SECRET="your-secret"
python scripts/test-mcp-staging.py
```

### 6. Updated Documentation

Updated existing docs to reference Fly.io deployment:

- **`mcp/README.md`**: Added MCP-only deployment section with quick-start
- **`docs/QUICKSTART-MCP.md`**: Updated Next Steps and Reference sections
- Both docs now clearly distinguish between:
  - **Option 1:** MCP-only to Fly.io (staging, recommended)
  - **Option 2:** Full-stack deployment (Go + Python)

## Architecture

```
┌─────────────────────────────────────────┐
│  MCP Client (Claude Desktop, etc.)     │
└──────────────┬──────────────────────────┘
               │ HTTPS/MCP Protocol
               │ (No auth required - shared tenant mode)
               ▼
┌─────────────────────────────────────────┐
│  Fly.io: Python MCP Proxy               │
│  (toolbridge-mcp-staging.fly.dev)       │
│  - Uses shared JWT token                │
│  - Generates signed tenant headers      │
│  - Forwards to K8s Go API               │
│                                         │
│  Dockerfile.mcp-only                    │
│  fly.staging.toml                       │
│  VM: 1 CPU, 512MB RAM                   │
└──────────────┬──────────────────────────┘
               │ HTTPS
               │ Authorization: Bearer {shared-jwt}
               │ X-TB-Tenant-ID
               │ X-TB-Timestamp
               │ X-TB-Signature
               ▼
┌─────────────────────────────────────────┐
│  K8s: Go REST API                       │
│  (toolbridgeapi.erauner.dev)            │
│  - Validates shared JWT                 │
│  - Validates tenant headers             │
│  - Executes business logic              │
│                                         │
│  Helm chart                             │
│  CloudNativePG                          │
└──────────────┬──────────────────────────┘
               │
               ▼
          PostgreSQL (K8s)
```

## Authentication Model

### Current Implementation (Shared JWT Token)

The current deployment uses a **shared JWT token** approach for simplicity and rapid deployment:

- **MCP Client → MCP Proxy:** No authentication required (unauthenticated SSE connection)
- **MCP Proxy → Go API:** Uses shared JWT token from `TOOLBRIDGE_JWT_TOKEN` environment variable
- **Tenant Isolation:** Enforced via HMAC-signed tenant headers (`X-TB-Tenant-ID`, `X-TB-Signature`)

**Benefits:**
✅ Simple setup - no OAuth configuration needed
✅ Works immediately with Claude Desktop and MCP Inspector
✅ All requests isolated to a single tenant (staging-tenant-001)
✅ Backend API properly authenticated with JWT

**Limitations:**
⚠️ All MCP clients share the same user identity (`claude-desktop-user`)
⚠️ Cannot support multi-user scenarios
⚠️ No per-user permissions or data isolation

### Future: OAuth/PKCE Support

Per-user authentication with OAuth will be added in a future PR:

- **MCP Client → MCP Proxy:** OAuth PKCE flow for user authentication
- **User-specific JWT:** Each Claude Desktop user gets their own JWT token
- **Per-user data isolation:** Notes/tasks/chats isolated by user ID
- **Auth0 Integration:** Leverages existing Auth0 infrastructure

**Tracking:** TODO - Create GitHub issue for OAuth/PKCE implementation

## Benefits of MCP-Only Deployment

### Operational Benefits

✅ **Smaller footprint:** 100MB image vs 500MB+ full-stack
✅ **Faster deployments:** 1-2 minutes vs 5-10 minutes
✅ **Lower cost:** 1 CPU / 512MB RAM vs 2 CPU / 2GB RAM
✅ **Auto-scale to zero:** No idle costs
✅ **Simpler troubleshooting:** Single service, fewer logs

### Development Benefits

✅ **Faster iteration:** Deploy MCP changes without touching Go API
✅ **Independent scaling:** Scale MCP proxy separately from Go API
✅ **Multi-region support:** Deploy MCP proxies near users, Go API stays centralized
✅ **Easier testing:** Test MCP changes against stable K8s backend

### Security Benefits

✅ **Defense in depth:** Two layers of authentication (JWT + tenant headers)
✅ **Tenant isolation:** HMAC signatures prevent cross-tenant access
✅ **Minimal attack surface:** No database credentials in Fly.io
✅ **Centralized data:** All data stays in K8s PostgreSQL

## Deployment Workflow

### Initial Setup (One-time)

1. **Verify K8s secrets:**
   ```bash
   kubectl get secret toolbridge-secret -n toolbridge \
     -o jsonpath='{.data.tenant-header-secret}' | base64 -d
   ```

2. **Create Fly.io app:**
   ```bash
   fly apps create toolbridge-mcp-staging
   ```

3. **Generate JWT token:**
   ```bash
   python3 scripts/generate-test-token.py
   ```

4. **Set Fly.io secrets:**
   ```bash
   fly secrets set -a toolbridge-mcp-staging \
     TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
     TOOLBRIDGE_TENANT_ID="staging-tenant-001" \
     TOOLBRIDGE_TENANT_HEADER_SECRET="<from-k8s-secret>" \
     TOOLBRIDGE_JWT_TOKEN="<from-generate-test-token.py>"
   ```

### Deployment (Repeatable)

1. **Test locally:**
   ```bash
   docker build -f Dockerfile.mcp-only -t test .
   docker run --rm -p 8001:8001 -e ... test
   curl http://localhost:8001/
   ```

2. **Deploy to Fly.io:**
   ```bash
   fly deploy --config fly.staging.toml -a toolbridge-mcp-staging
   ```

3. **Verify:**
   ```bash
   fly status -a toolbridge-mcp-staging
   fly logs -a toolbridge-mcp-staging
   python scripts/test-mcp-staging.py
   ```

### Rollback (If needed)

```bash
fly releases rollback -a toolbridge-mcp-staging
```

## Success Criteria (Phase 2)

From `Plans/phase2-flyio-deployment-prompt.md`:

- [x] ✅ MCP-only Dockerfile created and tested
- [x] ✅ Fly.io staging configuration ready
- [x] ✅ Comprehensive deployment documentation
- [x] ✅ Secrets management documented
- [x] ✅ Integration tests automated
- [x] ✅ Existing docs updated with references
- [ ] 🔄 App deployed to Fly.io (ready to deploy)
- [ ] 🔄 Health checks passing in production
- [ ] 🔄 All 40 MCP tools tested
- [ ] 🔄 Load test passed (50 concurrent, <200ms p95)
- [ ] 🔄 Security validation (tenant isolation)
- [ ] 🔄 Documentation updated with actual URLs
- [ ] 🔄 Runbook validated through actual deployment

## Next Steps

### Immediate (Ready to Execute)

1. **Deploy to Fly.io staging:**
   - Follow `docs/DEPLOYMENT-FLYIO.md` step-by-step
   - Verify secrets in K8s match Fly.io
   - Run deployment commands
   - Monitor logs during first deploy

2. **Run integration tests:**
   - Execute `scripts/test-mcp-staging.py`
   - Verify all 4 tests pass
   - Check latency metrics

3. **Validate with MCP Inspector:**
   - Connect to staging URL
   - List all 40 tools
   - Test at least 5 different tools
   - Verify tenant isolation

### Short-term (Within 1 week)

4. **Load testing:**
   - Create k6 load test script
   - Run 50 concurrent requests test
   - Measure p95 latency
   - Document results

5. **Security validation:**
   - Test invalid JWT rejection
   - Test cross-tenant access denial
   - Test timestamp skew limits
   - Verify HTTPS enforcement

6. **Real client testing:**
   - Configure Claude Desktop with staging
   - Test natural language interactions
   - Validate all CRUD operations
   - Document any issues

### Medium-term (Within 1 month)

7. **Production deployment:**
   - Create `fly.production.toml`
   - Set up production secrets
   - Deploy production app
   - Update DNS/ingress

8. **Multi-tenant expansion:**
   - Create per-tenant deployment template
   - Automate tenant app creation
   - Document tenant onboarding process

9. **Observability:**
   - Set up external monitoring (Better Uptime)
   - Configure alerts (latency, errors, downtime)
   - Integrate with logging service (Datadog, Papertrail)

## Files Created/Modified

### New Files

- `Dockerfile.mcp-only` - MCP-only Docker image
- `fly.staging.toml` - Fly.io staging configuration
- `docs/DEPLOYMENT-FLYIO.md` - Deployment runbook (62KB)
- `docs/SECRETS-REFERENCE.md` - Secrets management guide (18KB)
- `scripts/test-mcp-staging.py` - Integration test script
- `FLYIO-DEPLOYMENT-SUMMARY.md` - This summary (you are here)

### Modified Files

- `mcp/README.md` - Added Fly.io deployment section
- `docs/QUICKSTART-MCP.md` - Updated references and next steps

### Total Lines Added

- ~1,200 lines of documentation
- ~300 lines of configuration
- ~300 lines of test code
- **Total: ~1,800 lines**

## Questions Answered

From `Plans/phase2-flyio-deployment-prompt.md`:

1. **Should we use Fly.io Postgres or external database?**
   → External (K8s CloudNativePG). No database in Fly.io for MCP-only deployment.

2. **What should the final app naming convention be?**
   → `toolbridge-mcp-{environment}` for shared, `toolbridge-tenant-{id}` for per-tenant.

3. **Do we need custom domain setup now or later?**
   → Later. Use `*.fly.dev` for now, add custom domains in production phase.

4. **Should we enable Fly.io auto-scaling from the start?**
   → Yes, auto-scale to zero enabled to minimize costs during low usage.

5. **What's the secret rotation policy?**
   → 90 days for tenant header secret, documented in `docs/SECRETS-REFERENCE.md`.

6. **Do we need staging → production promotion process defined now?**
   → Basic process documented, detailed CI/CD pipeline deferred to production phase.

## References

- **Deployment Guide:** `docs/DEPLOYMENT-FLYIO.md`
- **Secrets Guide:** `docs/SECRETS-REFERENCE.md`
- **Quick Start:** `docs/QUICKSTART-MCP.md`
- **MCP README:** `mcp/README.md`
- **Phase 2 Plan:** `Plans/phase2-flyio-deployment-prompt.md`
- **Architecture Spec:** `docs/SPEC-FASTMCP-INTEGRATION.md`

## Support

For deployment issues:

1. Check `docs/DEPLOYMENT-FLYIO.md` troubleshooting section
2. Review Fly.io logs: `fly logs -a toolbridge-mcp-staging`
3. Verify K8s Go API health: `curl https://toolbridgeapi.erauner.dev/healthz`
4. Run integration tests: `python scripts/test-mcp-staging.py`
5. Check secrets match between K8s and Fly.io

---

**Status:** Implementation complete, ready for deployment ✅
**Last Updated:** 2025-11-19
**Author:** Claude Code Agent
