# Phase 2: Deploy FastMCP to Fly.io - Implementation Prompt

> **DEPRECATED:** This planning prompt references the old HMAC-signed tenant header architecture.
> The system now uses WorkOS API-based tenant authorization instead.
> See `docs/DEPLOYMENT-FLYIO.md` for current deployment instructions.

**Use this prompt to kick off Fly.io deployment implementation:**

---

## Context

We've successfully completed Phase 1 of the FastMCP integration for ToolBridge:
- ✅ All 40 MCP tools implemented (8 tools × 5 entities)
- ✅ Session-per-request pattern working reliably
- ✅ Dual authentication (JWT + HMAC-signed tenant headers)
- ✅ E2E tests passing (4/4)
- ✅ Code review approved (PR #43)
- ✅ Comprehensive documentation complete

**Current State:**
- Branch: `feat/fastmcp-integration` (ready to merge or deploy from)
- Local testing: ✅ Working perfectly
- Production deployment: ❌ Not yet done

**Key Files:**
- `Dockerfile.mcp` - Multi-stage build (Go + Python + supervisor)
- `fly.mcp.toml` - Deployment template (needs customization)
- `deployment/supervisord.conf` - Process management config
- `mcp/.env.example` - Environment variable template
- `Plans/fastmcp-integration.md` - Complete implementation plan
- `docs/SPEC-FASTMCP-INTEGRATION.md` - Architecture specification

## Goal

Deploy the ToolBridge FastMCP service to Fly.io staging environment and validate it works end-to-end with real LLM clients (Claude Desktop, VS Code MCP extension, or MCP Inspector).

## What Needs to Be Done

### 1. Pre-Deployment Setup

- [ ] **Review Deployment Architecture**
  - Read `fly.mcp.toml` and understand the configuration
  - Verify `Dockerfile.mcp` multi-stage build is correct
  - Check `deployment/supervisord.conf` process management
  - Understand the dual-service setup (Go API + Python MCP)

- [ ] **Generate Production Secrets**
  - Generate tenant header secret: `openssl rand -base64 32`
  - Generate JWT secret: `openssl rand -base64 32`
  - Document these securely (we'll need them for Fly.io secrets)

- [ ] **Database Setup**
  - Decide: Use Fly Postgres or existing external PostgreSQL?
  - If Fly Postgres: Create `fly postgres create` and attach
  - If external: Get connection string ready
  - Verify connection string format matches Go API expectations

### 2. Fly.io App Creation

- [ ] **Create Staging App**
  - Install Fly.io CLI if not already: `brew install flyctl` or `curl -L https://fly.io/install.sh | sh`
  - Login to Fly.io: `fly auth login`
  - Create app: `fly apps create toolbridge-mcp-staging` (or similar name)
  - Set region: Choose based on latency requirements

- [ ] **Configure App from Template**
  - Copy `fly.mcp.toml` to `fly.staging.toml`
  - Update app name to match created app
  - Adjust VM sizing if needed (default: shared-cpu-2x, 2048MB)
  - Configure health checks and concurrency settings

### 3. Secrets Configuration

Set all required secrets on Fly.io app:

```bash
# Database
fly secrets set DATABASE_URL="postgres://..." --app toolbridge-mcp-staging

# JWT Configuration
fly secrets set JWT_HS256_SECRET="<generated-secret>" --app toolbridge-mcp-staging
fly secrets set ENV="staging" --app toolbridge-mcp-staging

# MCP Tenant Configuration
fly secrets set TOOLBRIDGE_TENANT_ID="staging-tenant-001" --app toolbridge-mcp-staging
fly secrets set TOOLBRIDGE_TENANT_HEADER_SECRET="<generated-secret>" --app toolbridge-mcp-staging
fly secrets set TENANT_HEADER_SECRET="<same-as-above>" --app toolbridge-mcp-staging

# MCP Service Config
fly secrets set TOOLBRIDGE_GO_API_BASE_URL="http://localhost:8081" --app toolbridge-mcp-staging
fly secrets set TOOLBRIDGE_HOST="0.0.0.0" --app toolbridge-mcp-staging
fly secrets set TOOLBRIDGE_PORT="8001" --app toolbridge-mcp-staging
fly secrets set TOOLBRIDGE_LOG_LEVEL="DEBUG" --app toolbridge-mcp-staging
```

### 4. Build and Deploy

- [ ] **Build Docker Image Locally First (Test)**
  ```bash
  docker build -f Dockerfile.mcp -t toolbridge-mcp:test .
  docker run --rm \
    -e DATABASE_URL="postgres://..." \
    -e JWT_HS256_SECRET="test-secret" \
    -e TENANT_HEADER_SECRET="test-secret" \
    -e TOOLBRIDGE_TENANT_ID="test-tenant" \
    -e TOOLBRIDGE_TENANT_HEADER_SECRET="test-secret" \
    -e ENV="dev" \
    -p 8080:8080 \
    -p 8001:8001 \
    toolbridge-mcp:test

  # Verify both services start
  curl http://localhost:8080/health
  curl http://localhost:8001/
  ```

- [ ] **Deploy to Fly.io**
  ```bash
  fly deploy --config fly.staging.toml --dockerfile Dockerfile.mcp --app toolbridge-mcp-staging
  ```

- [ ] **Monitor Deployment**
  - Watch logs: `fly logs --app toolbridge-mcp-staging`
  - Check health: `fly status --app toolbridge-mcp-staging`
  - Verify both services started via supervisor logs
  - Check for any errors in startup sequence

### 5. Post-Deployment Validation

- [ ] **Health Check Verification**
  ```bash
  # Get app hostname
  APP_URL=$(fly info --app toolbridge-mcp-staging --json | jq -r '.Hostname')

  # Test Go API health
  curl https://${APP_URL}:8080/health

  # Test MCP service health
  curl https://${APP_URL}:8001/
  ```

- [ ] **MCP Tools Discovery**
  ```bash
  # Generate a test JWT token for staging tenant
  # (Use scripts/test-mcp-integration.py generate_jwt_token function as reference)

  # Test with MCP Inspector
  npx @modelcontextprotocol/inspector https://${APP_URL}:8001/sse

  # Should see all 40 tools listed
  ```

- [ ] **End-to-End Tool Execution**
  - Test `health_check` tool (simplest, no auth needed)
  - Test `create_note` tool with valid JWT
  - Test `list_notes` tool
  - Verify data persists in database
  - Test session creation (check logs for session IDs)

### 6. Real LLM Client Testing

- [ ] **Option A: Claude Desktop**
  - Add MCP server configuration to Claude Desktop config
  - Provide JWT token via environment or config
  - Test creating, listing, updating notes via natural language
  - Verify all 40 tools work as expected

- [ ] **Option B: VS Code MCP Extension**
  - Install MCP extension in VS Code
  - Configure connection to staging MCP server
  - Test tool invocations through extension
  - Validate responses and error handling

- [ ] **Option C: MCP Inspector (Web)**
  - Open Inspector UI: https://inspector.modelcontextprotocol.io
  - Connect to staging server: `https://${APP_URL}:8001/sse`
  - Manually invoke tools and verify responses
  - Test error scenarios (invalid UIDs, missing fields)

### 7. Load and Performance Testing

- [ ] **Concurrent Request Testing**
  ```bash
  # Use a tool like k6 or Apache Bench
  # Test 10-50 concurrent MCP tool calls
  # Measure p50, p95, p99 latencies
  # Target: <200ms p95 for tool calls
  ```

- [ ] **Session Creation Performance**
  - Monitor session creation time in logs
  - Verify session-per-request pattern working
  - Check for any session creation failures
  - Validate HMAC signature performance (<1ms)

- [ ] **Resource Monitoring**
  ```bash
  # Check resource usage
  fly metrics --app toolbridge-mcp-staging

  # Watch for:
  # - Memory usage (should be <512MB)
  # - CPU usage (should be <50% under load)
  # - Request latency
  # - Error rates
  ```

### 8. Observability Setup

- [ ] **Structured Logging**
  - Verify logs include trace IDs for correlation
  - Check Python → Go request flow is traceable
  - Validate tenant ID appears in all logs
  - Test log filtering by tenant/user

- [ ] **Metrics (Optional but Recommended)**
  - Set up basic metrics collection
  - Track: request count, latency, error rate
  - Monitor session creation success/failure rate
  - Alert on authentication failures

### 9. Documentation Updates

- [ ] **Update Deployment Docs**
  - Document actual Fly.io app name and URL
  - Record secrets management approach
  - Document rollback procedure
  - Add troubleshooting section for common issues

- [ ] **Create Deployment Runbook**
  - Step-by-step deployment procedure
  - Secret rotation process
  - Scale-up/scale-down procedures
  - Incident response guidelines

## Success Criteria

Before considering Phase 2 complete, verify:

- [ ] ✅ Fly.io app deployed and healthy
- [ ] ✅ Both services (Go + Python) running via supervisor
- [ ] ✅ Health checks passing consistently (>99% uptime in 24h)
- [ ] ✅ All 40 MCP tools discoverable via tools/list
- [ ] ✅ At least 5 different tools tested successfully
- [ ] ✅ Session-per-request working (check logs)
- [ ] ✅ Dual authentication enforced (JWT + tenant headers)
- [ ] ✅ No cross-tenant access possible (security test)
- [ ] ✅ Tested with at least one real LLM client
- [ ] ✅ Load test passed: 50 concurrent requests, <200ms p95
- [ ] ✅ Zero data corruption or integrity issues
- [ ] ✅ Logs structured and useful for debugging
- [ ] ✅ Documentation updated with staging details

## Known Issues to Watch For

Based on our local testing and architecture:

1. **Port Configuration**
   - Fly.io may require different internal/external ports
   - Verify `fly.toml` services configuration matches supervisor
   - Check firewall rules if services can't communicate

2. **Supervisor Startup Order**
   - Go API must start before Python MCP (dependency)
   - Check supervisor logs if MCP fails to connect
   - May need to adjust startup timeouts

3. **Database Connectivity**
   - Fly.io postgres uses different connection string format
   - May need SSL mode configuration
   - Check connection pooling settings

4. **Environment Variables**
   - Supervisor needs env vars passed through
   - Check `deployment/supervisord.conf` environment section
   - Verify secrets accessible to both services

5. **Health Check Timing**
   - Services may need 10-30 seconds to fully start
   - Fly.io health checks might be too aggressive
   - Adjust grace period if needed

## Rollback Plan

If deployment fails or issues found:

1. **Quick Rollback:**
   ```bash
   fly apps destroy toolbridge-mcp-staging
   ```

2. **Partial Rollback (Fix and Redeploy):**
   ```bash
   # Fix issue locally
   # Test build: docker build -f Dockerfile.mcp .
   # Redeploy: fly deploy --config fly.staging.toml
   ```

3. **Database Rollback:**
   - If migrations ran, may need to rollback schema
   - Check `internal/storage/migrations/` for down migrations
   - Be cautious with data integrity

## Reference Documentation

- **Architecture:** `docs/SPEC-FASTMCP-INTEGRATION.md`
- **Local Setup:** `docs/QUICKSTART-MCP.md`
- **Implementation Plan:** `Plans/fastmcp-integration.md`
- **MCP Service Docs:** `mcp/README.md`
- **Fly.io Docs:** https://fly.io/docs/
- **Fly.io Multi-Process Apps:** https://fly.io/docs/app-guides/multiple-processes/

## Questions to Answer During Implementation

1. Should we use Fly.io Postgres or external database?
2. What should the final app naming convention be? (toolbridge-mcp-{tenant-id}?)
3. Do we need custom domain setup now or later?
4. Should we enable Fly.io auto-scaling from the start?
5. What's the secret rotation policy? (90 days? 180 days?)
6. Do we need staging → production promotion process defined now?

## Output Expected

After completing this phase, you should have:

1. ✅ Working Fly.io staging deployment
2. ✅ Documentation of deployment process
3. ✅ Test results proving all 40 tools work
4. ✅ Performance benchmarks (latency, throughput)
5. ✅ Security validation (tenant isolation tested)
6. ✅ Runbook for future deployments
7. ✅ Clear go/no-go decision for production rollout

---

**Estimated Time:** 4-8 hours (including testing and documentation)
**Complexity:** Medium (we have all the pieces, just need to assemble)
**Blocker Risk:** Low (can test everything locally first)

**Ready to begin?** Start with section 1 (Pre-Deployment Setup) and work through sequentially.
