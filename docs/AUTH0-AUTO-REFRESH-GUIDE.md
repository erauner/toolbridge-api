# Auth0 Automatic Token Refresh - Deployment Guide

This guide explains how to configure and use the automatic Auth0 token refresh feature in the ToolBridge MCP server.

## Overview

The MCP server now automatically manages Auth0 access tokens using the OAuth2 client credentials flow. Tokens are:
- Fetched automatically on startup or when needed
- Cached in-memory for performance
- Refreshed automatically 5 minutes before expiry
- Never require manual intervention

## Configuration

### Environment Variables

Configure the following environment variables to enable automatic token refresh:

```bash
# Required: Auth0 client credentials
TOOLBRIDGE_AUTH0_CLIENT_ID=<your-client-id>
TOOLBRIDGE_AUTH0_CLIENT_SECRET=<your-client-secret>

# Optional: Auth0 configuration (defaults shown)
TOOLBRIDGE_AUTH0_DOMAIN=dev-zysv6k3xo7pkwmcb.us.auth0.com
TOOLBRIDGE_AUTH0_AUDIENCE=https://toolbridgeapi.erauner.dev
TOOLBRIDGE_TOKEN_REFRESH_BUFFER_SECONDS=300  # Refresh 5 minutes before expiry
TOOLBRIDGE_AUTH0_TIMEOUT_SECONDS=10.0         # HTTP timeout for Auth0 requests
```

### Getting Auth0 Credentials

Extract client credentials from terraform:

```bash
cd ~/git/side/homelab-k8s/terraform/auth0
terraform output -json mcp_introspection_client | jq -r '.client_id, .client_secret'
```

## Local Development

### 1. Create `.env` file

```bash
cd ~/git/side/toolbridge-api/mcp
cp .env.example .env
```

### 2. Configure Auth0 credentials

Edit `.env` and set:

```bash
TOOLBRIDGE_AUTH0_CLIENT_ID=<client-id-from-terraform>
TOOLBRIDGE_AUTH0_CLIENT_SECRET=<client-secret-from-terraform>
```

### 3. Start the MCP server

```bash
uv run python -m toolbridge_mcp.server
```

### 4. Verify automatic refresh

Check the logs for:

```
✓ Auth0 automatic token refresh enabled (domain=..., audience=...)
Auth0 TokenManager initialized (domain=..., audience=..., buffer=300s)
```

On first API request:

```
Fetching new Auth0 access token
Auth0 token refreshed successfully (expires in 86400s, at 2025-11-21T12:00:00Z)
```

## Fly.io Deployment

### 1. Update Fly.io secrets

```bash
fly secrets set \
  TOOLBRIDGE_AUTH0_CLIENT_ID="<client-id-from-terraform>" \
  TOOLBRIDGE_AUTH0_CLIENT_SECRET="<client-secret-from-terraform>" \
  TOOLBRIDGE_AUTH0_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com" \
  TOOLBRIDGE_AUTH0_AUDIENCE="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-mcp-staging
```

### 2. Remove deprecated static token (optional)

```bash
fly secrets unset TOOLBRIDGE_JWT_TOKEN -a toolbridge-mcp-staging
```

### 3. Deploy

```bash
fly deploy -a toolbridge-mcp-staging
```

### 4. Verify deployment

```bash
# Check logs
fly logs -a toolbridge-mcp-staging

# Should see:
# ✓ Auth0 automatic token refresh enabled
# Auth0 TokenManager initialized
```

### 5. Monitor token refresh

After ~24 hours (or 23 hours 55 minutes with default buffer):

```
Auth0 token refreshed successfully (expires in 86400s, at ...)
```

### 6. Remove manual refresh cron job

If you had a cron job running `scripts/refresh-flyio-auth0-token.sh`, remove it:

```bash
crontab -e
# Remove the line: 0 0 * * * cd /path/to/toolbridge-api && ./scripts/refresh-flyio-auth0-token.sh
```

## Health Monitoring

### Health Check Tool

The MCP server exposes a `health_check` tool that includes Auth0 status:

```bash
# Call health_check via MCP client
# Returns:
{
  "status": "healthy",
  "auth_mode": "auth0",
  "auth0_status": {
    "last_refresh_success": true,
    "expires_at": "2025-11-21T12:00:00Z",
    "last_refresh_at": "2025-11-20T12:00:00Z",
    "failure_count": 0
  }
}
```

### Log Monitoring

Key log messages to monitor:

**Success**:
```
✓ Auth0 automatic token refresh enabled
Auth0 token refreshed successfully (expires in 86400s, at ...)
Using cached Auth0 token
```

**Warnings**:
```
Auth0 token request failed (attempt 1/4): HTTP 503 - Service unavailable
Retrying in 0.5s...
```

**Errors**:
```
Auth0 authentication failed: 401 Unauthorized
Failed to fetch Auth0 token after 4 attempts
```

### Alerting

Set up alerts for:

1. **Token refresh failures** (3+ consecutive failures)
   - Log pattern: `"Failed to fetch Auth0 token after 4 attempts"`
   - Action: Check Auth0 service status, verify credentials

2. **High failure count**
   - Monitor `health_check.auth0_status.failure_count > 5`
   - Action: Investigate Auth0 connectivity or rate limits

3. **Token expiry approaching with failed refresh**
   - Monitor `expires_at - now < 1 hour` AND `last_refresh_success == false`
   - Action: Emergency intervention (restore static token if needed)

## Authentication Modes

The server auto-detects authentication mode based on configuration:

### Mode 1: Auth0 Auto-Refresh (Recommended)

**When**: All Auth0 credentials configured
**Logs**: `✓ Auth0 automatic token refresh enabled`
**Use case**: Production deployments

### Mode 2: Static Token (Deprecated)

**When**: `TOOLBRIDGE_JWT_TOKEN` set but no Auth0 credentials
**Logs**: `⚠ Using static JWT token (DEPRECATED)`
**Use case**: Emergency fallback only

### Mode 3: Passthrough (Per-User)

**When**: No backend credentials configured
**Logs**: `Using per-user authentication mode`
**Use case**: Future per-user OAuth implementation

## Automatic Fallback on Initialization Failure

If Auth0 TokenManager initialization fails during server startup (bad credentials, network issues, Auth0 outage), the server **automatically falls back** to a working authentication mode:

### Fallback Priority

1. **Static Token Mode** (if `TOOLBRIDGE_JWT_TOKEN` is configured)
   - Server logs: `⚠ Falling back to static JWT token mode due to Auth0 initialization failure`
   - All requests use the static token
   - Requests continue to succeed

2. **Passthrough Mode** (if no static token available)
   - Server logs: `⚠ Falling back to passthrough mode (per-user tokens from MCP headers) due to Auth0 initialization failure`
   - Requests must provide Authorization headers
   - Useful for per-user authentication scenarios

### Health Check During Fallback

The `health_check` tool reports the actual runtime mode:

```json
{
  "status": "healthy",
  "auth_mode": "static",  // Runtime mode (not "auth0")
  "auth_mode_note": "Configured for auth0 but running in static mode due to initialization failure",
  "auth0_init_failed": true
}
```

### Testing Fallback Behavior

Run the fallback tests to verify graceful degradation:

```bash
make test-mcp-auth
```

This test suite simulates Auth0 initialization failures and verifies:
- ✅ Fallback to static mode when JWT token available
- ✅ Fallback to passthrough mode when no JWT token
- ✅ `get_auth_header()` works correctly in fallback modes
- ✅ Requests succeed despite Auth0 being unavailable

### Recovery from Fallback

When Auth0 becomes available again:
1. Restart the MCP server
2. TokenManager initialization will succeed
3. Server returns to Auth0 auto-refresh mode
4. Health check shows `"auth_mode": "auth0"` with no fallback note

### Best Practices

1. **Keep static token as backup**: Maintain `TOOLBRIDGE_JWT_TOKEN` in secrets during initial Auth0 rollout
2. **Monitor fallback events**: Alert on `auth0_init_failed: true` in health checks
3. **Test fallback regularly**: Run `make test-mcp-auth` as part of CI/CD
4. **Document recovery**: Ensure team knows fallback is automatic and temporary

## Troubleshooting

### Issue: "TokenManager not initialized"

**Symptom**: `RuntimeError: TokenManager not initialized`

**Cause**: Auth0 credentials incomplete or server failed to initialize

**Solution**:
1. Check all required env vars are set: `AUTH0_CLIENT_ID`, `AUTH0_CLIENT_SECRET`
2. Restart server and check startup logs for initialization errors
3. Verify credentials with test request:
   ```bash
   curl -X POST https://<auth0-domain>/oauth/token \
     -H 'content-type: application/json' \
     -d '{"client_id":"...","client_secret":"...","audience":"...","grant_type":"client_credentials"}'
   ```

### Issue: "Auth0 authentication failed: 401 Unauthorized"

**Symptom**: All token refresh attempts fail with 401

**Cause**: Invalid client credentials

**Solution**:
1. Verify credentials from terraform match deployed secrets
2. Check Auth0 application is active and not disabled
3. Verify client credentials grant is enabled in Auth0 application settings

### Issue: "Failed to fetch Auth0 token after 4 attempts"

**Symptom**: All retry attempts exhausted

**Cause**: Auth0 service unavailable or network issues

**Solution**:
1. Check Auth0 service status: https://status.auth0.com
2. Verify network connectivity from deployment to Auth0
3. Temporarily use static token as fallback:
   ```bash
   # Get token manually
   curl -X POST https://<domain>/oauth/token \
     -H 'content-type: application/json' \
     -d '{"client_id":"...","client_secret":"...","audience":"...","grant_type":"client_credentials"}'

   # Set as static token
   fly secrets set TOOLBRIDGE_JWT_TOKEN="<token>" -a toolbridge-mcp-staging

   # Unset Auth0 credentials to use static mode
   fly secrets unset TOOLBRIDGE_AUTH0_CLIENT_ID TOOLBRIDGE_AUTH0_CLIENT_SECRET -a toolbridge-mcp-staging
   ```

### Issue: High latency on first request after restart

**Symptom**: First API call takes 100-500ms longer

**Cause**: Token fetch happens lazily on first request

**Solution**: This is expected behavior. Token is cached for subsequent requests.
- Future enhancement: Add eager token prefetch during startup

## Rollback Plan

If automatic refresh causes issues, rollback to static token mode:

### 1. Get a fresh token manually

```bash
./scripts/refresh-flyio-auth0-token.sh
```

### 2. Disable automatic refresh

```bash
fly secrets unset \
  TOOLBRIDGE_AUTH0_CLIENT_ID \
  TOOLBRIDGE_AUTH0_CLIENT_SECRET \
  -a toolbridge-mcp-staging
```

### 3. Server automatically falls back to static token mode

Logs will show: `⚠ Using static JWT token (DEPRECATED)`

### 4. Re-enable manual refresh cron job

```bash
crontab -e
# Add: 0 0 * * * cd /path/to/toolbridge-api && ./scripts/refresh-flyio-auth0-token.sh
```

## Migration from Static Token

### Phase 1: Test locally

1. Get Auth0 credentials from terraform
2. Configure `.env` with credentials
3. Test MCP server locally
4. Verify token refresh in logs

### Phase 2: Deploy to staging

1. Update Fly.io secrets with Auth0 credentials
2. Keep `TOOLBRIDGE_JWT_TOKEN` as backup initially
3. Deploy and monitor logs
4. Verify automatic refresh after ~24 hours

### Phase 3: Remove static token

1. After confirming automatic refresh works (>24 hours)
2. Remove static token: `fly secrets unset TOOLBRIDGE_JWT_TOKEN`
3. Remove manual refresh cron job
4. Update runbooks to reference automatic refresh

## References

- **ADR**: `docs/adr/0001-auth0-automatic-token-refresh.md` - Architectural decisions
- **Implementation**: `mcp/toolbridge_mcp/auth/token_manager.py` - Token manager code
- **Legacy Workaround**: `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md` - Historical reference
- **Auth0 Docs**: https://auth0.com/docs/get-started/authentication-and-authorization-flow/client-credentials-flow

## Support

For issues or questions:
1. Check logs for detailed error messages
2. Review ADR for architecture details
3. Consult Auth0 documentation for client credentials flow
4. Open issue with logs and configuration (redact secrets!)
