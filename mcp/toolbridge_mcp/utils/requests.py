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


# Module-level cache for tenant ID (resolved once per MCP server lifetime)
_cached_tenant_id: Optional[str] = None


def get_cached_tenant_id() -> Optional[str]:
    """Get cached tenant ID for X-Tenant-ID header."""
    return _cached_tenant_id


async def ensure_tenant_resolved(client: httpx.AsyncClient) -> str:
    """
    Ensure tenant ID is resolved and cached.

    Two modes:
    - Single-tenant: If TENANT_ID configured, use it directly (smoke testing)
    - Multi-tenant: Call /v1/auth/tenant to resolve dynamically (primary mode)

    Args:
        client: httpx client for tenant resolution API call

    Returns:
        Tenant ID string

    Raises:
        AuthorizationError: If tenant resolution fails
    """
    global _cached_tenant_id

    # Return cached value if already resolved
    if _cached_tenant_id:
        return _cached_tenant_id

    # Single-tenant mode: Use configured TENANT_ID (smoke testing)
    if settings.tenant_id:
        _cached_tenant_id = settings.tenant_id
        logger.info(f"Using configured tenant ID: {_cached_tenant_id} (single-tenant mode)")
        return _cached_tenant_id

    # Multi-tenant mode: Resolve tenant dynamically via /v1/auth/tenant
    logger.debug("Resolving tenant dynamically via /v1/auth/tenant (multi-tenant mode)")

    try:
        # Get ID token from MCP OAuth context
        mcp_token = get_access_token()
        id_token = mcp_token.token

        # Call backend tenant resolution endpoint
        tenant_id = await resolve_tenant(
            id_token=id_token,
            api_base_url=settings.go_api_base_url,
        )

        # Cache for subsequent requests
        _cached_tenant_id = tenant_id
        logger.info(f"✓ Tenant resolved and cached: {tenant_id} (multi-tenant mode)")
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

    Exchanges MCP OAuth token for backend JWT via token_exchange module.
    FastMCP has already validated the user's OAuth token via AuthKitProvider.

    Args:
        client: httpx client for token exchange requests

    Returns:
        Authorization header value (e.g., "Bearer eyJ...")

    Raises:
        AuthorizationError: If token exchange fails
    """
    from toolbridge_mcp.auth import TokenExchangeError
    
    try:
        logger.debug("Exchanging MCP OAuth token for backend JWT")
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
