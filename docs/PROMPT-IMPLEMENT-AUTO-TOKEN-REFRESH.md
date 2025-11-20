# Prompt: Implement Automatic Auth0 Token Refresh

Use this prompt with Claude to implement the proper fix for Auth0 token management in the MCP server.

---

## Prompt

I need you to implement automatic Auth0 token refresh in the ToolBridge MCP server to replace the current manual workaround.

### Context

**Current Problem:**
- MCP server uses static Auth0 access token stored in `TOOLBRIDGE_JWT_TOKEN`
- Token expires every 24 hours, requiring manual refresh via `scripts/refresh-flyio-auth0-token.sh`
- This creates operational overhead and risk of service downtime

**Desired Solution:**
- MCP server stores Auth0 **client credentials** (not tokens) as environment variables
- Fetches access tokens automatically using OAuth2 client credentials flow
- Caches tokens in-memory and refreshes before expiry (with 5-minute buffer)
- Zero manual intervention required

### Requirements

1. **Create Token Manager** (`mcp/toolbridge_mcp/auth/token_manager.py`):
   - Async token manager class with in-memory caching
   - Fetch tokens from Auth0 `/oauth/token` endpoint using client credentials grant
   - Auto-refresh tokens 5 minutes before expiry
   - Global singleton pattern for easy access
   - Comprehensive logging (token fetch, refresh, expiry times)

2. **Update Configuration** (`mcp/toolbridge_mcp/config.py`):
   - Add Auth0 settings:
     - `auth0_client_id` (required for auto-refresh)
     - `auth0_client_secret` (required for auto-refresh)
     - `auth0_domain` (default: `dev-zysv6k3xo7pkwmcb.us.auth0.com`)
     - `auth0_audience` (default: `https://toolbridgeapi.erauner.dev`)
   - Keep `jwt_token` for backward compatibility (with deprecation warning)

3. **Update Request Helpers** (`mcp/toolbridge_mcp/utils/requests.py`):
   - Modify `get_auth_header()` to:
     1. Try Auth0 token manager first (if `auth0_client_id` configured)
     2. Fall back to static `jwt_token` (with deprecation warning)
     3. Fall back to MCP request headers (per-user mode)
   - No changes needed to `call_get`, `call_post`, etc. - they already use `get_auth_header()`

4. **Initialize on Startup** (`mcp/toolbridge_mcp/server.py`):
   - Initialize token manager on server startup if Auth0 credentials configured
   - Log authentication mode (auto-refresh, static token, or per-user)

5. **Update Dependencies** (`mcp/pyproject.toml`):
   - Ensure `httpx` is in dependencies (should already be present)

6. **Testing:**
   - Test locally with Auth0 credentials from terraform
   - Verify token fetch and refresh logic
   - Confirm backward compatibility with static `jwt_token`

### Implementation Details

**Token Manager Design:**
```python
class TokenManager:
    - __init__(client_id, client_secret, domain, audience)
    - async get_token() -> str  # Returns valid token, refreshing if needed
    - _is_token_valid() -> bool  # Check if cached token still valid (with 5min buffer)
    - async _refresh_token()     # Fetch new token from Auth0
    - async close()              # Cleanup
```

**Auth0 Token Request:**
```python
POST https://{domain}/oauth/token
Content-Type: application/json

{
  "client_id": "...",
  "client_secret": "...",
  "audience": "...",
  "grant_type": "client_credentials"
}

Response:
{
  "access_token": "eyJ...",
  "expires_in": 86400,
  "token_type": "Bearer"
}
```

**Environment Variables:**
```bash
# New (auto-refresh mode)
TOOLBRIDGE_AUTH0_CLIENT_ID=<client-id-from-terraform>
TOOLBRIDGE_AUTH0_CLIENT_SECRET=<client-secret-from-terraform>
TOOLBRIDGE_AUTH0_DOMAIN=dev-zysv6k3xo7pkwmcb.us.auth0.com
TOOLBRIDGE_AUTH0_AUDIENCE=https://toolbridgeapi.erauner.dev

# Deprecated (static token mode)
TOOLBRIDGE_JWT_TOKEN=eyJ...
```

### Files to Create/Modify

- **Create:** `mcp/toolbridge_mcp/auth/__init__.py`
- **Create:** `mcp/toolbridge_mcp/auth/token_manager.py`
- **Modify:** `mcp/toolbridge_mcp/config.py`
- **Modify:** `mcp/toolbridge_mcp/utils/requests.py`
- **Modify:** `mcp/toolbridge_mcp/server.py`
- **Modify:** `mcp/pyproject.toml` (if needed)

### Success Criteria

After implementation:

1. ✅ MCP server starts with Auth0 credentials and logs "Auth0 token manager initialized"
2. ✅ First API request triggers token fetch, logs "Auth0 token refreshed"
3. ✅ Subsequent requests use cached token (no new fetch)
4. ✅ Token refreshes automatically before expiry (monitor logs after ~23.5 hours)
5. ✅ Backward compatible - still works with static `TOOLBRIDGE_JWT_TOKEN`
6. ✅ Integration tests pass: `python scripts/test-mcp-staging.py`

### Testing Commands

```bash
# 1. Get Auth0 credentials from terraform
cd ~/git/side/homelab-k8s/terraform/auth0
terraform output -json mcp_introspection_client | jq -r '.client_id, .client_secret'

# 2. Test locally
cd ~/git/side/toolbridge-api/mcp
export TOOLBRIDGE_AUTH0_CLIENT_ID="<client-id-from-terraform>"
export TOOLBRIDGE_AUTH0_CLIENT_SECRET="<client-secret-from-terraform>"
export TOOLBRIDGE_AUTH0_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com"
export TOOLBRIDGE_AUTH0_AUDIENCE="https://toolbridgeapi.erauner.dev"
export TOOLBRIDGE_TENANT_ID="staging-tenant-001"
export TOOLBRIDGE_TENANT_HEADER_SECRET="<tenant-secret-from-k8s>"
export TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"

uv run python -m toolbridge_mcp.server

# 3. Deploy to Fly.io
fly secrets set \
  TOOLBRIDGE_AUTH0_CLIENT_ID="<client-id-from-terraform>" \
  TOOLBRIDGE_AUTH0_CLIENT_SECRET="<client-secret-from-terraform>" \
  TOOLBRIDGE_AUTH0_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com" \
  TOOLBRIDGE_AUTH0_AUDIENCE="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-mcp-staging

# Remove deprecated static token
fly secrets unset TOOLBRIDGE_JWT_TOKEN -a toolbridge-mcp-staging

# 4. Run integration tests
cd ~/git/side/toolbridge-api
python scripts/test-mcp-staging.py
```

### References

- See `docs/WORKAROUND-AUTH0-TOKEN-REFRESH.md` for detailed architecture
- Auth0 Client Credentials: https://auth0.com/docs/get-started/authentication-and-authorization-flow/client-credentials-flow
- Current workaround script: `scripts/refresh-flyio-auth0-token.sh`

---

## Expected Output

After running this prompt with Claude, you should receive:

1. **Complete implementation** of token manager
2. **Updated configuration** with Auth0 settings
3. **Modified request helpers** with auto-refresh logic
4. **Updated server startup** code
5. **Test results** showing successful token fetch and API calls
6. **Migration guide** for deploying to Fly.io

**Estimated implementation time:** 2-3 hours
