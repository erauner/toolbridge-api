# Architectural Planning Prompt: Auth0 Token Management for MCP Server

**Context:** This prompt is for an architect to review and plan the proper implementation of automatic Auth0 token refresh in the ToolBridge MCP server before development begins.

---

## Background

Our Fly.io MCP server currently uses a **static Auth0 access token** stored as an environment variable to authenticate with our Go API. This creates operational overhead because Auth0 tokens expire after 24 hours, requiring daily manual refresh.

We've implemented a temporary workaround (see `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md`), but need to architect a production-ready solution.

## Current Architecture

### Authentication Flow
```
MCP Client (Claude Desktop)
    ↓ (SSE)
Fly.io MCP Server (Python/FastMCP)
    ↓ (HTTP + Static Auth0 token + Tenant headers)
K8s Go API (toolbridgeapi.erauner.dev)
    ↓ (Auth0 RS256 validation)
Auth0 (dev-zysv6k3xo7pkwmcb.us.auth0.com)
```

### Current Implementation
- **MCP Server:** Stores static `TOOLBRIDGE_JWT_TOKEN` (expires 24h)
- **Go API:** Validates Auth0 RS256 tokens + HMAC-signed tenant headers
- **Problem:** Manual token refresh required daily via `scripts/refresh-flyio-auth0-token.sh`

### Proposed Solution (High Level)
- MCP server stores Auth0 **client credentials** (not tokens)
- Implements `TokenManager` class to fetch and cache tokens
- Auto-refreshes tokens before expiry
- See `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md` for implementation details

## Architectural Review Request

Please review the proposed approach and provide architectural guidance on the following areas:

### 1. Token Management Strategy

**Current Proposal:**
- In-memory token caching with 5-minute refresh buffer
- OAuth2 client credentials flow (M2M authentication)
- Single shared token for all MCP → Go API requests

**Questions:**
1. **Caching Strategy:**
   - Is in-memory caching appropriate for this use case?
   - Should we consider distributed caching (Redis) for future horizontal scaling?
   - What happens if the MCP server restarts mid-request?

2. **Refresh Timing:**
   - Is 5-minute buffer before expiry appropriate?
   - Should we implement exponential backoff for refresh failures?
   - How should we handle Auth0 rate limits?

3. **Concurrency:**
   - How do we handle concurrent requests during token refresh?
   - Should we implement request queuing during refresh?
   - Thread safety considerations for Python asyncio?

### 2. Security Considerations

**Current Proposal:**
- Store client credentials as Fly.io secrets
- Credentials loaded on server startup
- Tokens cached in-memory (never persisted)

**Questions:**
1. **Credential Storage:**
   - Are Fly.io secrets appropriate for production client credentials?
   - Should we use a secrets manager (Vault, AWS Secrets Manager)?
   - Credential rotation strategy?

2. **Token Exposure:**
   - In-memory token storage - acceptable risk?
   - Should tokens be encrypted in memory?
   - Logging considerations (ensure tokens never logged)?

3. **Failure Scenarios:**
   - What if Auth0 is unavailable during startup?
   - Graceful degradation strategy?
   - Should we cache the last valid token to disk for cold starts?

### 3. Multi-Tenancy & Per-User Authentication

**Current State:**
- Single shared token for all users (M2M authentication)
- All requests use same Auth0 subject (`<client-id>@clients`)
- User context lost at Go API layer

**Future Consideration:**
- MCP clients could obtain their own Auth0 tokens (OAuth2/PKCE)
- Pass user tokens through MCP → Go API
- Go API sees actual user identity

**Questions:**
1. Should we design `TokenManager` to support both modes?
   - Shared backend token (current need)
   - Per-user token passthrough (future enhancement)
2. How does this affect audit logging and user attribution?
3. Migration path from M2M to per-user auth?

### 4. Observability & Monitoring

**Questions:**
1. **Metrics:**
   - What metrics should we expose? (token refresh count, failures, latency)
   - Should we integrate with Prometheus/Datadog?
   - SLO for token refresh success rate?

2. **Logging:**
   - What events need logging? (token fetch, refresh, expiry, errors)
   - Structured logging format?
   - Log retention and PII considerations?

3. **Alerting:**
   - When should we alert? (refresh failures, Auth0 unavailable)
   - Escalation policy?
   - Health check integration?

### 5. Error Handling & Resilience

**Questions:**
1. **Failure Modes:**
   - Auth0 API unavailable → Retry strategy?
   - Invalid credentials → Alert and fail fast?
   - Token expired mid-request → Retry with new token?

2. **Circuit Breaker:**
   - Should we implement circuit breaker for Auth0 calls?
   - Fallback strategy if Auth0 persistently fails?
   - Health check behavior when token refresh failing?

3. **Request Retry:**
   - If Go API rejects token, should MCP server retry with fresh token?
   - Idempotency considerations?

### 6. Deployment & Operations

**Questions:**
1. **Deployment Strategy:**
   - Blue/green deployment with Fly.io?
   - How to update credentials without downtime?
   - Rollback strategy if token manager has bugs?

2. **Testing:**
   - How to test token refresh in staging?
   - Mock Auth0 for local development?
   - Integration tests for token expiry scenarios?

3. **Documentation:**
   - Runbook for token refresh failures?
   - Disaster recovery procedures?
   - Onboarding docs for new developers?

### 7. Scalability & Performance

**Questions:**
1. **Horizontal Scaling:**
   - Current proposal: single Fly.io instance (in-memory cache works)
   - Future: multiple instances → need shared token cache (Redis)?
   - When should we move to distributed caching?

2. **Performance Impact:**
   - Auth0 token fetch latency (~100-500ms)
   - Impact on first request after server start?
   - Should we pre-fetch token during server initialization?

3. **Rate Limits:**
   - Auth0 client credentials rate limits?
   - How many token refreshes per day?
   - Mitigation if we hit rate limits?

### 8. Alternative Approaches

Please evaluate these alternatives:

**Option A: TokenManager (Proposed)**
- In-memory caching with automatic refresh
- See `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md`

**Option B: Sidecar Token Proxy**
- Separate service manages tokens
- MCP server calls proxy for tokens
- Allows multiple MCP instances to share tokens

**Option C: AWS Secrets Manager / Vault Integration**
- Store tokens in secrets manager
- Secrets manager handles rotation
- MCP server fetches from secrets API

**Option D: Fly.io Secrets Rotation**
- Use Fly.io native secrets rotation
- External cron job updates secrets
- MCP server restarts on secret change

**Questions:**
1. Which approach best fits our architecture?
2. Trade-offs in complexity vs. reliability?
3. Cost considerations?
4. Operational overhead?

## Deliverables

Please provide:

1. **Architectural Decision Records (ADRs):**
   - Token caching strategy (in-memory vs distributed)
   - Security model for credential storage
   - Error handling and resilience patterns
   - Observability and monitoring approach

2. **System Design:**
   - Component diagram showing token flow
   - Sequence diagram for token refresh
   - Error handling flowchart

3. **Implementation Recommendations:**
   - Preferred libraries/frameworks (if any)
   - Code structure and patterns
   - Testing strategy

4. **Operational Plan:**
   - Deployment steps
   - Monitoring and alerting setup
   - Runbook for common failure scenarios

5. **Risk Assessment:**
   - Security risks and mitigations
   - Availability/reliability concerns
   - Migration risks from current workaround

6. **Timeline & Effort Estimate:**
   - Development effort (broken down by component)
   - Testing and QA effort
   - Documentation and runbook creation

## Reference Materials

- **Workaround Documentation:** `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md`
- **Implementation Prompt:** `docs/PROMPT-IMPLEMENT-AUTO-TOKEN-REFRESH.md`
- **Current Workaround Script:** `scripts/refresh-flyio-auth0-token.sh`
- **Fly.io Deployment Guide:** `docs/DEPLOYMENT-FLYIO.md`

## Current Environment

- **MCP Server:** Python 3.10+, FastMCP 2.13.1, running on Fly.io (single instance)
- **Go API:** Running in K8s with Auth0 RS256 validation + tenant header validation
- **Auth0:** Tenant `dev-zysv6k3xo7pkwmcb.us.auth0.com`
- **Secrets:** Currently managed via Fly.io secrets + SOPS-encrypted K8s secrets

## Success Criteria

The final architecture should:
- ✅ Eliminate manual token refresh operations
- ✅ Handle token expiry gracefully (zero downtime)
- ✅ Be secure (credentials never exposed, tokens ephemeral)
- ✅ Be observable (metrics, logs, alerts)
- ✅ Be testable (local dev, staging, production)
- ✅ Be maintainable (clear code, good documentation)
- ✅ Be scalable (support future horizontal scaling if needed)

## Questions or Clarifications?

If you need additional context about:
- Current authentication flow details
- Auth0 configuration (terraform in `homelab-k8s/terraform/auth0/`)
- Go API implementation
- Tenant header validation logic

Please ask and I can provide more details.

---

**Next Steps After Architectural Review:**

1. Review and approve/modify architectural decisions
2. Create implementation tasks based on ADRs
3. Use `docs/PROMPT-IMPLEMENT-AUTO-TOKEN-REFRESH.md` for development
4. Iterate on implementation with architect feedback
5. Deploy to staging and validate
6. Deploy to production with monitoring

Thank you for your architectural guidance!
