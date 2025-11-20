# Auth0 Token Refresh Workaround

> **⚠️ DEPRECATED**: This workaround has been replaced with automatic token refresh.
>
> **New Implementation**: See ADR `docs/adr/0001-auth0-automatic-token-refresh.md`
>
> The MCP server now includes a built-in `TokenManager` that automatically fetches and refreshes Auth0 tokens using client credentials flow. Configure `TOOLBRIDGE_AUTH0_CLIENT_ID` and `TOOLBRIDGE_AUTH0_CLIENT_SECRET` instead of manually managing tokens.
>
> **Migration**: Update Fly.io secrets with Auth0 credentials and remove the manual refresh cron job. See deployment guide in the ADR.

---

# Historical Documentation (Legacy)

## Problem Statement

The Fly.io MCP server requires valid Auth0 RS256 access tokens to authenticate with the Go API. Currently, the access token is manually stored as a Fly.io secret (`TOOLBRIDGE_JWT_TOKEN`), but Auth0 tokens expire after 24 hours.

This creates operational overhead requiring daily manual token refresh.

## Current Workaround

### What We're Doing

1. Manually fetching Auth0 access token using client credentials flow
2. Storing the token as `TOOLBRIDGE_JWT_TOKEN` in Fly.io secrets
3. MCP server uses this static token for all Go API requests
4. Token expires after 24 hours, requiring manual refresh

### How to Use the Workaround

Run the refresh script daily:

```bash
./scripts/refresh-flyio-auth0-token.sh
```

Or set up a cron job:

```bash
# Run daily at midnight
0 0 * * * cd /path/to/toolbridge-api && ./scripts/refresh-flyio-auth0-token.sh >> /var/log/auth0-refresh.log 2>&1
```

### What the Script Does

1. Extracts Auth0 client credentials from terraform outputs
2. Calls Auth0 token endpoint with client credentials grant
3. Updates `TOOLBRIDGE_JWT_TOKEN` secret in Fly.io
4. Triggers rolling restart of MCP server
5. Verifies deployment health

## Proper Solution

### Architecture Changes Needed

Instead of storing a static access token, the MCP server should:

1. **Store Auth0 client credentials** (not tokens) as Fly.io secrets
2. **Fetch tokens on demand** using OAuth2 client credentials flow
3. **Cache tokens in-memory** with automatic refresh before expiry
4. **Handle token expiration gracefully** with retry logic

### Implementation Plan

#### 1. Update Fly.io Secrets

Remove the static token and add client credentials:

```bash
# Remove static token
fly secrets unset TOOLBRIDGE_JWT_TOKEN -a toolbridge-mcp-staging

# Add client credentials
fly secrets set \
  TOOLBRIDGE_AUTH0_CLIENT_ID="<client-id>" \
  TOOLBRIDGE_AUTH0_CLIENT_SECRET="<client-secret>" \
  TOOLBRIDGE_AUTH0_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com" \
  TOOLBRIDGE_AUTH0_AUDIENCE="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-mcp-staging
```

#### 2. Add Token Manager to MCP Server

Create `mcp/toolbridge_mcp/auth/token_manager.py`:

```python
"""
Auth0 token manager with automatic refresh.

Handles OAuth2 client credentials flow and token caching.
"""

import time
import asyncio
from typing import Optional
from datetime import datetime, timedelta

import httpx
from loguru import logger
from pydantic import BaseModel


class TokenResponse(BaseModel):
    """Auth0 token response."""
    access_token: str
    expires_in: int
    token_type: str
    scope: Optional[str] = None


class TokenManager:
    """
    Manages Auth0 access tokens with automatic refresh.

    Fetches tokens using OAuth2 client credentials flow and caches
    them in-memory until they're close to expiration (with 5min buffer).
    """

    def __init__(
        self,
        client_id: str,
        client_secret: str,
        domain: str,
        audience: str,
    ):
        self.client_id = client_id
        self.client_secret = client_secret
        self.domain = domain
        self.audience = audience

        # Token cache
        self._token: Optional[str] = None
        self._expires_at: Optional[datetime] = None

        # HTTP client for token requests
        self._client = httpx.AsyncClient(timeout=10.0)

        # Buffer before expiry to refresh token (5 minutes)
        self._refresh_buffer = timedelta(minutes=5)

    async def get_token(self) -> str:
        """
        Get valid access token, refreshing if needed.

        Returns:
            Valid Auth0 access token

        Raises:
            httpx.HTTPError: If token request fails
        """
        # Check if we have a valid cached token
        if self._is_token_valid():
            logger.debug("Using cached Auth0 token")
            return self._token

        # Fetch new token
        logger.info("Fetching new Auth0 access token")
        await self._refresh_token()
        return self._token

    def _is_token_valid(self) -> bool:
        """Check if cached token is still valid."""
        if not self._token or not self._expires_at:
            return False

        # Token is valid if it hasn't expired yet (with buffer)
        now = datetime.utcnow()
        return now < (self._expires_at - self._refresh_buffer)

    async def _refresh_token(self) -> None:
        """Fetch new access token from Auth0."""
        token_url = f"https://{self.domain}/oauth/token"

        payload = {
            "client_id": self.client_id,
            "client_secret": self.client_secret,
            "audience": self.audience,
            "grant_type": "client_credentials",
        }

        try:
            response = await self._client.post(
                token_url,
                json=payload,
                headers={"content-type": "application/json"},
            )
            response.raise_for_status()

            token_data = TokenResponse(**response.json())

            # Update cache
            self._token = token_data.access_token
            self._expires_at = datetime.utcnow() + timedelta(seconds=token_data.expires_in)

            logger.info(
                f"Auth0 token refreshed (expires in {token_data.expires_in}s, "
                f"at {self._expires_at.isoformat()}Z)"
            )

        except httpx.HTTPError as e:
            logger.error(f"Failed to fetch Auth0 token: {e}")
            raise

    async def close(self) -> None:
        """Close HTTP client."""
        await self._client.aclose()


# Global token manager instance (initialized on startup)
_token_manager: Optional[TokenManager] = None


def init_token_manager(
    client_id: str,
    client_secret: str,
    domain: str,
    audience: str,
) -> None:
    """Initialize global token manager."""
    global _token_manager
    _token_manager = TokenManager(
        client_id=client_id,
        client_secret=client_secret,
        domain=domain,
        audience=audience,
    )
    logger.info("Auth0 token manager initialized")


async def get_access_token() -> str:
    """
    Get valid Auth0 access token.

    Returns:
        Valid access token

    Raises:
        RuntimeError: If token manager not initialized
    """
    if not _token_manager:
        raise RuntimeError("Token manager not initialized. Call init_token_manager() first.")

    return await _token_manager.get_token()
```

#### 3. Update Configuration

Add Auth0 settings to `mcp/toolbridge_mcp/config.py`:

```python
class Settings(BaseSettings):
    """Application settings loaded from environment variables."""

    # Tenant configuration
    tenant_id: str
    tenant_header_secret: str

    # Go API connection
    go_api_base_url: str = "http://localhost:8081"

    # Auth0 configuration (for automatic token refresh)
    auth0_client_id: str | None = None
    auth0_client_secret: str | None = None
    auth0_domain: str | None = None
    auth0_audience: str | None = None

    # Deprecated: Static JWT token (use Auth0 client credentials instead)
    jwt_token: str | None = None

    # Logging
    log_level: str = "INFO"

    # Server configuration
    host: str = "0.0.0.0"
    port: int = 8001

    # Security
    max_timestamp_skew_seconds: int = 300

    model_config = SettingsConfigDict(
        env_prefix="TOOLBRIDGE_",
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )
```

#### 4. Update Request Helpers

Modify `mcp/toolbridge_mcp/utils/requests.py`:

```python
from toolbridge_mcp.auth.token_manager import get_access_token

async def get_auth_header() -> str:
    """
    Get Authorization header with valid Auth0 token.

    Automatically fetches and refreshes Auth0 tokens as needed.
    Falls back to static jwt_token if Auth0 not configured.

    Returns:
        Authorization header value (e.g., "Bearer eyJ...")

    Raises:
        AuthorizationError: If no authentication method configured
    """
    # Option 1: Use Auth0 token manager (preferred)
    if settings.auth0_client_id:
        logger.debug("Fetching Auth0 token from token manager")
        token = await get_access_token()
        return f"Bearer {token}"

    # Option 2: Use static JWT token (deprecated)
    if settings.jwt_token:
        logger.warning("Using static JWT token (deprecated - switch to Auth0 client credentials)")
        return f"Bearer {settings.jwt_token}"

    # Option 3: Extract from MCP request (for per-user OAuth)
    headers = get_http_headers()
    auth = headers.get("authorization") or headers.get("Authorization")

    if not auth:
        logger.error("No authentication configured")
        raise AuthorizationError(
            "Missing authentication. Configure TOOLBRIDGE_AUTH0_CLIENT_ID or TOOLBRIDGE_JWT_TOKEN."
        )

    logger.debug("Using Authorization header from MCP request")
    return auth
```

#### 5. Initialize Token Manager on Startup

Update `mcp/toolbridge_mcp/server.py`:

```python
from toolbridge_mcp.auth.token_manager import init_token_manager
from toolbridge_mcp.config import settings

# Initialize Auth0 token manager if configured
if settings.auth0_client_id:
    init_token_manager(
        client_id=settings.auth0_client_id,
        client_secret=settings.auth0_client_secret,
        domain=settings.auth0_domain,
        audience=settings.auth0_audience,
    )
    logger.info("Auth0 automatic token refresh enabled")
elif settings.jwt_token:
    logger.warning(
        "Using static JWT token (deprecated). "
        "Switch to Auth0 client credentials for automatic refresh."
    )
else:
    logger.info("No backend authentication configured (per-user mode)")
```

#### 6. Update Dependencies

Add `httpx` to `mcp/pyproject.toml` if not already present:

```toml
[tool.poetry.dependencies]
python = "^3.10"
fastmcp = "^2.0.0"
httpx = "^0.27.0"
pyjwt = "^2.8.0"
loguru = "^0.7.0"
pydantic = "^2.0.0"
pydantic-settings = "^2.0.0"
```

#### 7. Update Deployment Documentation

Update `docs/DEPLOYMENT-FLYIO.md` to document the new approach:

```markdown
## Auth0 Configuration

The MCP server authenticates with the Go API using Auth0 access tokens.
Two modes are supported:

### Automatic Token Refresh (Recommended)

Store Auth0 client credentials as Fly.io secrets. The MCP server will
automatically fetch and refresh tokens as needed:

\`\`\`bash
fly secrets set \
  TOOLBRIDGE_AUTH0_CLIENT_ID="<client-id-from-terraform>" \
  TOOLBRIDGE_AUTH0_CLIENT_SECRET="<client-secret-from-terraform>" \
  TOOLBRIDGE_AUTH0_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com" \
  TOOLBRIDGE_AUTH0_AUDIENCE="https://toolbridgeapi.erauner.dev" \
  -a toolbridge-mcp-staging
\`\`\`

Get credentials from terraform:

\`\`\`bash
cd ~/git/side/homelab-k8s/terraform/auth0
terraform output -json mcp_introspection_client
\`\`\`

### Static Token (Deprecated)

For testing only. Requires manual refresh every 24 hours:

\`\`\`bash
fly secrets set TOOLBRIDGE_JWT_TOKEN="<auth0-token>" -a toolbridge-mcp-staging
\`\`\`

Use `scripts/refresh-flyio-auth0-token.sh` for manual refresh.
```

### Testing the Fix

1. **Local Testing:**
   ```bash
   # Set Auth0 credentials in .env
   export TOOLBRIDGE_AUTH0_CLIENT_ID="..."
   export TOOLBRIDGE_AUTH0_CLIENT_SECRET="..."
   export TOOLBRIDGE_AUTH0_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com"
   export TOOLBRIDGE_AUTH0_AUDIENCE="https://toolbridgeapi.erauner.dev"

   # Start MCP server
   cd mcp
   uv run python -m toolbridge_mcp.server
   ```

2. **Verify Token Refresh:**
   - Check logs for "Auth0 token refreshed" messages
   - Tokens should refresh ~5 minutes before expiry
   - No manual intervention needed

3. **Integration Testing:**
   ```bash
   # Run full integration tests
   python scripts/test-mcp-staging.py
   ```

### Migration Steps

1. Implement token manager code
2. Test locally with Auth0 credentials
3. Deploy to Fly.io staging with new secrets
4. Monitor logs for successful token refresh
5. Remove workaround script and cron jobs
6. Update documentation

### Benefits

- ✅ **No manual intervention** - Tokens refresh automatically
- ✅ **No service interruption** - Refresh happens before expiry
- ✅ **Better security** - Client credentials stored, not tokens
- ✅ **Production-ready** - Handles token expiry gracefully
- ✅ **Observability** - Token refresh events logged

### Alternative: Per-User OAuth

For a more advanced implementation supporting per-user authentication:

1. MCP clients (Claude Desktop) obtain Auth0 tokens via OAuth2/PKCE
2. Pass tokens in MCP request Authorization headers
3. MCP server forwards user tokens to Go API
4. Go API validates user-specific tokens

This requires:
- Auth0 OAuth2/PKCE integration in MCP clients
- Token refresh logic in MCP clients
- No shared backend token needed

See `docs/OAUTH-PER-USER-AUTH.md` for detailed design.

## Summary

**Current State:**
- Manual token refresh every 24 hours
- Operational overhead
- Risk of service downtime if token expires

**Workaround:**
- Run `scripts/refresh-flyio-auth0-token.sh` daily
- Set up cron job for automation

**Proper Fix:**
- Store Auth0 client credentials (not tokens)
- Implement automatic token refresh in MCP server
- Zero manual intervention required
- Production-ready authentication flow

**Estimated Effort:**
- Implementation: 2-3 hours
- Testing: 1 hour
- Documentation: 30 minutes
- **Total: ~4 hours**

## References

- Auth0 Client Credentials Flow: https://auth0.com/docs/get-started/authentication-and-authorization-flow/client-credentials-flow
- OAuth2 Token Refresh: https://www.rfc-editor.org/rfc/rfc6749#section-6
- FastMCP Authentication: https://gofastmcp.com/docs/authentication
