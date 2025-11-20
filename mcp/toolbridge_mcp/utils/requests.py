"""
HTTP request helpers for calling the Go API with per-user authentication.

Path B OAuth 2.1 Flow:
1. FastMCP validates user OAuth token via Auth0Provider
2. Token exchange converts MCP token → backend JWT
3. Backend JWT sent to Go API with tenant headers
4. Go API validates backend JWT and creates per-user session

Session management: Each request creates a fresh sync session before calling
the API. Sessions are NOT cached or reused to avoid stale session issues.
"""

from typing import Any, Dict, Optional

import httpx
from loguru import logger

from toolbridge_mcp.auth import exchange_for_backend_jwt, extract_user_id_from_backend_jwt
from toolbridge_mcp.utils.session import create_session
from toolbridge_mcp.config import settings


class AuthorizationError(Exception):
    """Raised when token exchange fails."""
    pass


async def get_backend_auth_header(client: httpx.AsyncClient) -> str:
    """
    Get Authorization header for backend API calls.
    
    Exchanges MCP OAuth token for backend JWT via token_exchange module.
    FastMCP has already validated the user's OAuth token via Auth0Provider.
    
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

    Creates a fresh sync session for this request and includes session headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes")
        params: Query parameters

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing
    """
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

    Creates a fresh sync session for this request and includes session headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes")
        json: JSON request body

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing
    """
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

    Creates a fresh sync session for this request and includes session headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes/{uid}")
        json: JSON request body
        if_match: Optional version for optimistic locking

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing
    """
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

    Creates a fresh sync session for this request and includes session headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes/{uid}")
        json: JSON request body (partial update)

    auth_header = await get_backend_auth_header(client)
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing
    """
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

    Creates a fresh sync session for this request and includes session headers.

    Args:
        client: httpx client (with TenantDirectTransport)
        path: API endpoint path (e.g., "/v1/notes/{uid}")

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing
    """
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
