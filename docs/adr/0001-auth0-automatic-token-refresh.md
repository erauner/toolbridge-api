# ADR 0001: Auth0 Automatic Token Refresh Architecture

## Status

**Accepted** - Implemented in PR #XX

## Context

The ToolBridge MCP server running on Fly.io requires valid Auth0 RS256 access tokens to authenticate with the Go API. Previously, we used a static token stored in `TOOLBRIDGE_JWT_TOKEN`, which expired after 24 hours and required manual daily refresh via a shell script (`scripts/refresh-flyio-auth0-token.sh`).

This created operational overhead, risk of service downtime if tokens expired, and was not production-ready.

## Decision

We have implemented an automatic Auth0 token management system with the following architecture:

### 1. Token Caching Strategy

**Decision**: In-memory token caching with singleton `TokenManager`

**Rationale**:
- Current deployment is single Fly.io instance - no need for distributed cache
- In-memory cache minimizes latency (no external service calls)
- Tokens are ephemeral and should not be persisted
- 5-minute refresh buffer guards against clock skew and network jitter

**Concurrency Control**: Uses `asyncio.Lock` to serialize token refresh operations, preventing thundering herd when multiple concurrent requests trigger refresh.

**Restart Behavior**: On server restart, first request triggers token fetch (lazy initialization). This adds ~100-500ms latency to first request but simplifies startup.

**Future Scaling Path**: For horizontal scaling (multiple instances):
- Option A: Shared Redis cache keyed by tenant/app
- Option B: Per-instance refresh with jitter to respect Auth0 rate limits
- Documented but not implemented (YAGNI principle)

### 2. Security Model

**Credential Storage**:
- Store Auth0 client credentials (not tokens) as Fly.io secrets
- Credentials loaded on startup via pydantic-settings
- Acceptable for current deployment stage; documented migration to Vault if needed

**Token Handling**:
- Tokens never logged (only truncated hashes in debug logs)
- Cached in-memory only - never persisted to disk
- HTTP client confined to `TokenManager` module

**Rotation**:
- Credential rotation: update Fly.io secrets → restart server
- No automatic credential rotation (future enhancement)

**Backward Compatibility**:
- Keep `TOOLBRIDGE_JWT_TOKEN` support with deprecation warning
- Allows gradual migration and emergency rollback

### 3. Failure Handling & Resilience

**Retry Strategy**:
- Capped exponential backoff: 0.5s, 1.0s, 2.0s (4 attempts total)
- No retry on 4xx errors (indicates bad credentials - fail fast)
- Retry on 5xx and network errors (transient failures)
- Manual implementation (no external dependency like `tenacity`)

**Refresh Failures**:
- Log structured error with retry count
- Surface `TokenError` to caller with actionable message
- Health check exposes `last_refresh_success` and `failure_count`

**Request Path**:
- `get_auth_header()` wraps token errors in `AuthorizationError`
- Clear error messages tell operators which env vars to configure
- Optional future enhancement: retry request on 401/403 with fresh token

**Startup Behavior**:
- If credentials configured but refresh fails, server still boots
- First request will trigger retry; if fails, returns error to client
- Allows server to start even if Auth0 temporarily unavailable

### 4. Authentication Modes

**Three-Mode Design**:

1. **auth0** (recommended): Automatic token refresh using client credentials
   - All Auth0 credentials present → TokenManager initialized
   - Tokens fetched on-demand and cached

2. **static** (deprecated): Static JWT token from config
   - `TOOLBRIDGE_JWT_TOKEN` configured but no Auth0 credentials
   - Logs deprecation warning once per server lifetime

3. **passthrough**: Per-user tokens from MCP request headers
   - No backend credentials configured
   - Each MCP request provides its own Authorization header

**Mode Detection**: `settings.auth_mode()` helper determines mode at runtime based on config presence.

### 5. Observability

**Logging**:
- Structured logs: "Auth0 token refreshed" with expiry timestamp
- Failure logs with error details and retry count
- Authentication mode logged on startup
- Debug logs for cache hits (minimize noise)

**Health Check**:
- `health_check()` tool exposes:
  - `auth_mode`: current authentication mode
  - `auth0_status`: token expiry, last refresh time, failure count
  - Non-blocking (doesn't trigger refresh)

**Metrics Hooks** (future):
- TokenManager designed to expose metrics:
  - `refresh_count`, `failure_count`, `last_refresh_duration`
  - Properties available but not yet wired to Prometheus
  - Allows future integration without code changes

### 6. Multi-Tenancy & Per-User Auth

**Current State**:
- Shared backend token for all users (M2M authentication)
- All requests use same Auth0 subject (`<client-id>@clients`)
- User context handled via session management

**Future Extension**:
- `get_auth_header()` already supports passthrough mode
- Can add per-user tokens even when backend token exists
- Requires determining priority (user token > backend token)

**Session User ID**:
- With client credentials, `sub` is `<client-id>@clients`
- Go API session creation accepts this (confirmed)
- Future: may need synthetic user ID for client credentials mode

### 7. Implementation Interfaces

**Files Created**:
- `mcp/toolbridge_mcp/auth/__init__.py` - Public API
- `mcp/toolbridge_mcp/auth/token_manager.py` - Core implementation

**Files Modified**:
- `mcp/toolbridge_mcp/config.py` - Added Auth0 settings and `auth_mode()` helper
- `mcp/toolbridge_mcp/server.py` - Initialize TokenManager on startup
- `mcp/toolbridge_mcp/utils/requests.py` - Use TokenManager in `get_auth_header()`
- `mcp/.env.example` - Document Auth0 configuration

**Dependencies**:
- No new dependencies (already have `httpx`)
- Uses Python stdlib `asyncio`, `datetime`, `atexit`

## Consequences

### Positive

✅ **Zero manual intervention** - Tokens refresh automatically
✅ **No service interruption** - Refresh happens before expiry (5min buffer)
✅ **Better security** - Client credentials stored, not tokens
✅ **Production-ready** - Handles failures gracefully with retries
✅ **Observability** - Comprehensive logging and health check
✅ **Backward compatible** - Static token mode still works (with warning)
✅ **Simple deployment** - Update Fly.io secrets, restart server

### Negative

⚠ **First-request latency** - After restart, first request waits for Auth0 (~100-500ms)
⚠ **Single point of failure** - If Auth0 down, all authentication fails (but with retries)
⚠ **Session semantics** - Client credentials subject may differ from expected user subject
⚠ **Horizontal scaling** - Requires distributed cache for multi-instance deployments

### Neutral

ℹ️ **In-memory cache** - Simple and fast, but lost on restart (acceptable trade-off)
ℹ️ **Manual retry logic** - No external dependency, but custom code to maintain
ℹ️ **Logging volume** - Additional logs for token refresh events

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Auth0 downtime during startup | Server boots anyway; first request triggers retry |
| Auth0 rate limits | Caching reduces requests; 5min buffer provides margin |
| Clock skew | 5min buffer accommodates typical clock drift |
| Token subject mismatch | Confirmed Go API accepts client credentials subject |
| Concurrent refresh attempts | asyncio.Lock ensures only one refresh at a time |
| Failed refresh | Comprehensive logging; health check exposes status |

## Alternatives Considered

### Option A: TokenManager (Chosen)

**Pros**: Simple, no external dependencies, fits single-instance deployment
**Cons**: Doesn't scale horizontally without modification

### Option B: Sidecar Token Proxy

**Pros**: Allows multiple MCP instances to share tokens
**Cons**: Adds complexity, another service to deploy and monitor

### Option C: Secrets Manager (Vault/AWS)

**Pros**: Centralized secret management, automatic rotation
**Cons**: Requires additional infrastructure, more complex deployment

### Option D: Fly.io Secrets Rotation

**Pros**: Uses platform features, simple conceptually
**Cons**: Still requires external cron job, server restarts on rotation

**Decision Rationale**: Option A (TokenManager) best fits current needs - simple, self-contained, production-ready. Other options add complexity without immediate benefit. Can migrate later if needs change.

## Migration Plan

1. **Phase 1**: Implement TokenManager (this PR)
   - Add Auth0 configuration to codebase
   - Test locally with Auth0 credentials
   - Deploy to Fly.io staging

2. **Phase 2**: Update Fly.io secrets
   ```bash
   fly secrets set \
     TOOLBRIDGE_AUTH0_CLIENT_ID="..." \
     TOOLBRIDGE_AUTH0_CLIENT_SECRET="..." \
     -a toolbridge-mcp-staging
   ```

3. **Phase 3**: Verify automatic refresh
   - Monitor logs for "Auth0 token refreshed"
   - Check health_check for token status
   - Verify tokens refresh ~24 hours after start

4. **Phase 4**: Remove workaround
   - Delete cron job running `refresh-flyio-auth0-token.sh`
   - Optionally remove `TOOLBRIDGE_JWT_TOKEN` secret
   - Update documentation to mark workaround as legacy

5. **Rollback Plan**:
   - Re-enable static token: `fly secrets set TOOLBRIDGE_JWT_TOKEN=...`
   - Remove Auth0 credentials: `fly secrets unset TOOLBRIDGE_AUTH0_CLIENT_ID ...`
   - Server automatically falls back to static mode

## References

- **Architecture Plan**: `docs/ARCHITECT-PROMPT-TOKEN-REFRESH.md`
- **Implementation Prompt**: `docs/PROMPT-IMPLEMENT-AUTO-TOKEN-REFRESH.md`
- **Workaround Documentation**: `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md`
- **Refresh Script**: `scripts/refresh-flyio-auth0-token.sh` (legacy)
- **Auth0 Client Credentials Flow**: https://auth0.com/docs/get-started/authentication-and-authorization-flow/client-credentials-flow

## Approval

**Author**: Claude (AI Assistant)
**Date**: 2025-11-20
**Reviewers**: TBD
**Status**: Awaiting review
