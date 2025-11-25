"""
HTTP request helpers for calling the Go API with per-user authentication.

WorkOS AuthKit OAuth 2.1 Flow:
1. FastMCP validates user OAuth token via AuthKitProvider
2. Token exchange converts MCP token → backend JWT
3. Tenant resolution (if needed) calls /v1/auth/tenant to determine organization
4. Backend JWT sent to Go API with tenant header
5. Go API validates backend JWT and creates per-user session

Session management: Each request creates a fresh sync session before calling
the API. Sessions are NOT cached or reused to avoid stale session issues.

Tenant resolution: Supports two modes:
- Single-tenant mode: TENANT_ID env var set → uses hardcoded tenant (smoke testing)
- Multi-tenant mode: TENANT_ID not set → dynamically resolves via /v1/auth/tenant (primary mode)
"""

from typing import Any, Dict, Optional

import httpx
from fastmcp.server.dependencies import get_access_token
from loguru import logger

from toolbridge_mcp.auth import (
    exchange_for_backend_jwt,
    extract_user_id_from_backend_jwt,
    resolve_tenant,
    TenantResolutionError,
)
from toolbridge_mcp.config import settings
from toolbridge_mcp.utils.session import create_session


class AuthorizationError(Exception):
    """Raised when token exchange fails."""
    pass


# Per-user tenant cache: key is user_id (from backend JWT), value is tenant_id
# This prevents cross-tenant data leakage in multi-user MCP deployments
_tenant_cache: Dict[str, str] = {}

# Per-user backend JWT cache: key is user_id, value is backend JWT
# Prevents double token exchange per request (ensure_tenant_resolved + get_backend_auth_header)
_jwt_cache: Dict[str, str] = {}


def get_cached_tenant_id(user_id: str) -> Optional[str]:
    """Get cached tenant ID for specific user."""
    return _tenant_cache.get(user_id)


def get_cached_backend_jwt(user_id: str) -> Optional[str]:
    """Get cached backend JWT for specific user."""
    return _jwt_cache.get(user_id)


async def ensure_tenant_resolved(client: httpx.AsyncClient) -> str:
    """
    Ensure tenant ID is resolved for the current user.

    Two modes:
    - Single-tenant: If TENANT_ID configured, use it directly (smoke testing)
    - Multi-tenant: Call /v1/auth/tenant to resolve dynamically (primary mode)

    Both modes cache tenant per-user to:
    - Avoid cross-tenant leakage in multi-user MCP deployments
    - Enable TenantDirectTransport to inject X-TB-Tenant-ID header

    Also caches backend JWT to avoid double token exchange per request.

    Args:
        client: httpx client for tenant resolution API call

    Returns:
        Tenant ID string

    Raises:
        AuthorizationError: If tenant resolution fails
    """
    try:
        # Get user_id from MCP OAuth token first (no network call needed)
        # This allows us to check caches before doing expensive token exchange
        mcp_token = get_access_token()
        user_id = mcp_token.claims.get("sub")

        if not user_id:
            raise AuthorizationError("MCP token missing 'sub' claim")

        # Check per-user caches first to avoid unnecessary network calls
        cached_tenant = _tenant_cache.get(user_id)
        cached_jwt = _jwt_cache.get(user_id)

        if cached_tenant and cached_jwt:
            logger.debug(f"Using cached tenant and JWT for user {user_id}: {cached_tenant}")
            return cached_tenant

        # Need to exchange for backend JWT (cache miss or first request)
        backend_jwt = await exchange_for_backend_jwt(client)
        _jwt_cache[user_id] = backend_jwt

        # Check if tenant was cached (JWT cache miss but tenant cache hit)
        if cached_tenant:
            logger.debug(f"Using cached tenant for user {user_id}: {cached_tenant}")
            return cached_tenant

        # Single-tenant mode: Use configured TENANT_ID (smoke testing)
        if settings.tenant_id:
            logger.warning(f"⚠️  Using configured tenant: {settings.tenant_id} (single-tenant mode)")
            # Cache so TenantDirectTransport can inject header
            _tenant_cache[user_id] = settings.tenant_id
            return settings.tenant_id

        # Multi-tenant mode: Resolve tenant dynamically via /v1/auth/tenant
        logger.debug("Resolving tenant dynamically via /v1/auth/tenant (multi-tenant mode)")

        # Get ID token from MCP OAuth context
        mcp_token = get_access_token()
        id_token = mcp_token.token

        # Call backend tenant resolution endpoint
        tenant_id = await resolve_tenant(
            id_token=id_token,
            api_base_url=settings.go_api_base_url,
        )

        # Cache per-user for subsequent requests
        _tenant_cache[user_id] = tenant_id
        logger.success(f"✓ Tenant cached for user {user_id}: {tenant_id} (multi-tenant mode)")
        return tenant_id

    except TenantResolutionError as e:
        logger.error(f"Tenant resolution failed: {e}")
        raise AuthorizationError(
            f"Failed to resolve tenant ID: {e}"
        ) from e
    except Exception as e:
        logger.error(f"Unexpected error during tenant resolution: {e}")
        raise AuthorizationError(f"Tenant resolution error: {e}") from e


async def get_backend_auth_header(client: httpx.AsyncClient) -> str:
    """
    Get Authorization header for backend API calls.

    First checks the JWT cache (populated by ensure_tenant_resolved) for the
    CURRENT user (identified via MCP OAuth context). Falls back to exchanging
    MCP OAuth token if not cached.

    Args:
        client: httpx client for token exchange requests

    Returns:
        Authorization header value (e.g., "Bearer eyJ...")

    Raises:
        AuthorizationError: If token exchange fails
    """
    from toolbridge_mcp.auth import TokenExchangeError

    try:
        # Get current user's identity from MCP OAuth context
        # This ensures we return the correct user's JWT in multi-user scenarios
        mcp_token = get_access_token()
        current_user_id = mcp_token.claims.get("sub")

        # Try to get cached JWT for THIS specific user (avoids double token exchange)
        # ensure_tenant_resolved caches the JWT when it exchanges for user_id
        if current_user_id and current_user_id in _jwt_cache:
            cached_jwt = _jwt_cache[current_user_id]
            logger.debug(f"Using cached backend JWT for user {current_user_id}")
            return f"Bearer {cached_jwt}"

        # Fall back to token exchange if not cached
        # (shouldn't happen if ensure_tenant_resolved was called first)
        logger.debug(f"Exchanging MCP OAuth token for backend JWT (cache miss for user {current_user_id})")
        backend_jwt = await exchange_for_backend_jwt(client)
        return f"Bearer {backend_jwt}"
    except TokenExchangeError as e:
        logger.error(f"Token exchange failed: {e}")
        raise AuthorizationError(
            "Failed to exchange MCP token for backend JWT. "
            "Check token exchange configuration."
        ) from e
    except Exception as e:
        logger.error(f"Unexpected error during token exchange: {e}")
        raise AuthorizationError(f"Token exchange error: {e}") from e




async def ensure_session(client: httpx.AsyncClient, auth_header: str) -> Dict[str, str]:
    """
    Create a fresh sync session for this request.

    Always creates a new session - no caching or reuse.
    This ensures MCP tools can recover from session expiration.

    Args:
        client: httpx client (with TenantDirectTransport)
        auth_header: Backend JWT Authorization header

    Returns:
        Dict with session headers
    """
    # Extract user ID from backend JWT
    token = auth_header[7:]  # Remove "Bearer " prefix
    user_id = extract_user_id_from_backend_jwt(token)
    
    # Always create a fresh session for each request
    return await create_session(client, auth_header, user_id)


async def call_get(
    client: httpx.AsyncClient,
    path: str,
    params: Optional[Dict[str, Any]] = None,
) -> httpx.Response:
    """
    Make GET request to Go API.

    Ensures tenant is resolved, creates a fresh sync session, and includes all required headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes")
        params: Query parameters

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing or tenant resolution fails
    """
    # Ensure tenant is resolved (single-tenant mode or dynamic resolution)
    await ensure_tenant_resolved(client)

    auth_header = await get_backend_auth_header(client)
    session_headers = await ensure_session(client, auth_header)

    headers = {
        "Authorization": auth_header,
        **session_headers,
    }

    logger.debug(f"GET {path} params={params}")
    response = await client.get(path, params=params, headers=headers)
    response.raise_for_status()
    return response


async def call_post(
    client: httpx.AsyncClient,
    path: str,
    json: Optional[Dict[str, Any]] = None,
) -> httpx.Response:
    """
    Make POST request to Go API.

    Ensures tenant is resolved, creates a fresh sync session, and includes all required headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes")
        json: JSON request body

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing or tenant resolution fails
    """
    # Ensure tenant is resolved (single-tenant mode or dynamic resolution)
    await ensure_tenant_resolved(client)

    auth_header = await get_backend_auth_header(client)
    session_headers = await ensure_session(client, auth_header)

    headers = {
        "Authorization": auth_header,
        **session_headers,
    }

    logger.debug(f"POST {path}")
    response = await client.post(path, json=json, headers=headers)
    response.raise_for_status()
    return response


async def call_put(
    client: httpx.AsyncClient,
    path: str,
    json: Optional[Dict[str, Any]] = None,
    if_match: Optional[int] = None,
) -> httpx.Response:
    """
    Make PUT request to Go API.

    Ensures tenant is resolved, creates a fresh sync session, and includes all required headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes/{uid}")
        json: JSON request body
        if_match: Optional version for optimistic locking

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing or tenant resolution fails
    """
    # Ensure tenant is resolved (single-tenant mode or dynamic resolution)
    await ensure_tenant_resolved(client)

    auth_header = await get_backend_auth_header(client)
    session_headers = await ensure_session(client, auth_header)

    headers = {
        "Authorization": auth_header,
        **session_headers,
    }

    if if_match is not None:
        headers["If-Match"] = str(if_match)

    logger.debug(f"PUT {path} if_match={if_match}")
    response = await client.put(path, json=json, headers=headers)
    response.raise_for_status()
    return response


async def call_patch(
    client: httpx.AsyncClient,
    path: str,
    json: Optional[Dict[str, Any]] = None,
) -> httpx.Response:
    """
    Make PATCH request to Go API.

    Ensures tenant is resolved, creates a fresh sync session, and includes all required headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes/{uid}")
        json: JSON request body (partial update)

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing or tenant resolution fails
    """
    # Ensure tenant is resolved (single-tenant mode or dynamic resolution)
    await ensure_tenant_resolved(client)

    auth_header = await get_backend_auth_header(client)
    session_headers = await ensure_session(client, auth_header)

    headers = {
        "Authorization": auth_header,
        **session_headers,
    }

    logger.debug(f"PATCH {path}")
    response = await client.patch(path, json=json, headers=headers)
    response.raise_for_status()
    return response


async def call_delete(
    client: httpx.AsyncClient,
    path: str,
) -> httpx.Response:
    """
    Make DELETE request to Go API.

    Ensures tenant is resolved, creates a fresh sync session, and includes all required headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes/{uid}")

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing or tenant resolution fails
    """
    # Ensure tenant is resolved (single-tenant mode or dynamic resolution)
    await ensure_tenant_resolved(client)

    auth_header = await get_backend_auth_header(client)
    session_headers = await ensure_session(client, auth_header)

    headers = {
        "Authorization": auth_header,
        **session_headers,
    }

    logger.debug(f"DELETE {path}")
    response = await client.delete(path, headers=headers)
    response.raise_for_status()
    return response
