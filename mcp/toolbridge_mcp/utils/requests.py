"""
HTTP request helpers for calling the Go API.

These helpers extract the Authorization header from the current MCP request
context and forward it to the Go API along with tenant headers (which are
added automatically by TenantDirectTransport).

Session management: Each request creates a fresh sync session before calling
the API. Sessions are NOT cached or reused to avoid stale session issues.
"""

from typing import Any, Dict, Optional

import httpx
import jwt as pyjwt
from fastmcp.server.dependencies import get_http_headers
from loguru import logger

from toolbridge_mcp.utils.session import create_session
from toolbridge_mcp.config import settings


class AuthorizationError(Exception):
    """Raised when Authorization header is missing from MCP request."""

    pass


class JWTDecodeError(Exception):
    """Raised when JWT token cannot be decoded."""

    pass


# Module-level flag for logging deprecation warning only once
_static_token_warning_shown = False


async def get_auth_header() -> str:
    """
    Get Authorization header with valid Auth0 token.

    Supports three authentication modes (auto-detected from config):
    1. Auth0 auto-refresh: Fetch tokens using client credentials (recommended)
    2. Static token: Use pre-configured JWT_TOKEN (deprecated)
    3. Passthrough: Extract from MCP request Authorization header (per-user mode)

    Uses the runtime auth mode which may differ from configured mode if Auth0
    initialization failed during startup (automatic fallback to static/passthrough).

    Returns:
        Authorization header value (e.g., "Bearer eyJ...")

    Raises:
        AuthorizationError: If no authentication method configured or token fetch fails
    """
    global _static_token_warning_shown

    # Import runtime auth mode getter
    from toolbridge_mcp.server import get_runtime_auth_mode

    auth_mode = get_runtime_auth_mode()

    # Mode 1: Auth0 automatic token refresh (recommended)
    if auth_mode == "auth0":
        from toolbridge_mcp.auth import get_access_token, TokenError

        try:
            logger.debug("Fetching Auth0 token from TokenManager")
            token = await get_access_token()
            return f"Bearer {token}"
        except TokenError as e:
            logger.error(f"Auth0 token acquisition failed: {e}")
            raise AuthorizationError(
                "Failed to acquire Auth0 token. Check TOOLBRIDGE_AUTH0_CLIENT_ID/SECRET "
                "and Auth0 service availability."
            ) from e
        except Exception as e:
            logger.error(f"Unexpected error getting Auth0 token: {e}")
            raise AuthorizationError(f"Auth0 token error: {e}") from e

    # Mode 2: Static JWT token (deprecated)
    if auth_mode == "static":
        if not _static_token_warning_shown:
            logger.warning(
                "Using static JWT token (DEPRECATED). "
                "Configure TOOLBRIDGE_AUTH0_CLIENT_ID/SECRET for automatic refresh."
            )
            _static_token_warning_shown = True
        logger.debug("Using configured static JWT token")
        return f"Bearer {settings.jwt_token}"

    # Mode 3: Passthrough (extract from MCP request headers)
    headers = get_http_headers()
    auth = headers.get("authorization") or headers.get("Authorization")

    if not auth:
        logger.error("No authentication configured")
        raise AuthorizationError(
            "Missing authentication. Configure one of:\n"
            "  - TOOLBRIDGE_AUTH0_CLIENT_ID/SECRET (automatic refresh)\n"
            "  - TOOLBRIDGE_JWT_TOKEN (static token)\n"
            "  - Provide Authorization header in MCP requests (per-user mode)"
        )

    logger.debug("Using Authorization header from MCP request (passthrough mode)")
    return auth


def extract_user_id_from_jwt(auth_header: str) -> str:
    """
    Extract user ID (sub claim) from JWT token.

    Args:
        auth_header: Authorization header (e.g., "Bearer eyJ...")

    Returns:
        User ID from JWT sub claim

    Raises:
        JWTDecodeError: If token cannot be decoded
    """
    try:
        # Extract token from "Bearer <token>"
        if not auth_header.startswith("Bearer "):
            raise JWTDecodeError("Authorization header must start with 'Bearer '")

        token = auth_header[7:]  # Remove "Bearer " prefix

        # Decode without verification (we're just extracting the sub claim)
        # The Go API will verify the signature
        decoded = pyjwt.decode(token, options={"verify_signature": False})

        user_id = decoded.get("sub")
        if not user_id:
            raise JWTDecodeError("JWT token missing 'sub' claim")

        return user_id

    except pyjwt.InvalidTokenError as e:
        logger.error(f"Failed to decode JWT: {e}")
        raise JWTDecodeError(f"Invalid JWT token: {e}") from e
    except Exception as e:
        logger.error(f"Unexpected error decoding JWT: {e}")
        raise JWTDecodeError(f"Failed to extract user ID from JWT: {e}") from e


async def ensure_session(client: httpx.AsyncClient, auth_header: str) -> Dict[str, str]:
    """
    Create a fresh sync session for this request.

    Always creates a new session - no caching or reuse.
    This ensures MCP tools can recover from session expiration.

    Args:
        client: httpx client (with TenantDirectTransport)
        auth_header: Authorization header

    Returns:
        Dict with session headers
    """
    # Always create a fresh session for each request
    user_id = extract_user_id_from_jwt(auth_header)
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
    auth_header = await get_auth_header()
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
    auth_header = await get_auth_header()
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
    auth_header = await get_auth_header()
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

    Returns:
        HTTP response

    Raises:
        httpx.HTTPStatusError: If request fails
        AuthorizationError: If Authorization header missing
    """
    auth_header = await get_auth_header()
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
    auth_header = await get_auth_header()
    session_headers = await ensure_session(client, auth_header)

    headers = {
        "Authorization": auth_header,
        **session_headers,
    }

    logger.debug(f"DELETE {path}")
    response = await client.delete(path, headers=headers)
    response.raise_for_status()
    return response
