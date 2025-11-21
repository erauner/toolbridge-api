"""
Token exchange for converting MCP OAuth tokens to backend API JWTs.

Supports three exchange patterns:
1. Backend token exchange endpoint (recommended) - POST /auth/token-exchange
2. MCP server issues JWTs (if we control signing key)
3. Pass-through with validated claims

This module enables per-user authentication flow:
- User authenticates via WorkOS AuthKit OAuth 2.1 + PKCE
- MCP server validates user token via FastMCP AuthKitProvider
- Token exchange converts MCP token → backend JWT
- Backend API validates backend JWT and creates per-user session
"""

from typing import Optional
from datetime import datetime, timedelta

import httpx
from jose import jwt
from fastmcp.server.dependencies import get_access_token
from loguru import logger

from toolbridge_mcp.config import settings


class TokenExchangeError(Exception):
    """Raised when token exchange fails."""
    pass


async def exchange_for_backend_jwt(
    http_client: httpx.AsyncClient,
) -> str:
    """
    Exchange MCP OAuth token for backend API JWT.
    
    Three-tier fallback strategy:
    1. Try backend /auth/token-exchange endpoint (recommended)
    2. Issue JWT locally if JWT_SIGNING_KEY configured
    3. Fail with clear error message
    
    Args:
        http_client: httpx client for making requests
        
    Returns:
        Backend JWT token string (Bearer token)
        
    Raises:
        TokenExchangeError: If exchange fails after all attempts
    """
    # Get authenticated user from MCP OAuth context
    # FastMCP has already validated this token via AuthKitProvider
    token = get_access_token()
    user_id = token.claims.get("sub")
    email = token.claims.get("email")
    tenant_id = token.claims.get("tenant_id")  # Custom claim if configured

    logger.debug(f"Exchanging WorkOS AuthKit token for user: {user_id}, tenant: {tenant_id or 'default'}")
    
    # OPTION 1: Backend token exchange endpoint (RECOMMENDED)
    # This delegates JWT signing to the backend, keeping secrets centralized
    try:
        response = await http_client.post(
            f"{settings.go_api_base_url}/auth/token-exchange",
            headers={
                "Authorization": f"Bearer {token.token}",
                "Content-Type": "application/json",
            },
            json={
                "audience": settings.backend_api_audience,
                "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
            },
            timeout=10.0,
        )
        
        if response.status_code == 200:
            exchange_response = response.json()
            backend_jwt = exchange_response["access_token"]
            logger.debug(f"✓ Token exchanged via backend endpoint for user {user_id}")
            return backend_jwt
        else:
            logger.warning(
                f"Token exchange endpoint returned {response.status_code}: {response.text}. "
                f"Falling back to MCP-issued JWT"
            )
    except httpx.HTTPError as e:
        logger.warning(f"Token exchange endpoint failed: {e}. Falling back to MCP-issued JWT")
    except Exception as e:
        logger.warning(f"Unexpected error during token exchange: {e}. Falling back to MCP-issued JWT")
    
    # OPTION 2: MCP server issues JWTs (if we control backend auth)
    # This requires JWT_SIGNING_KEY environment variable
    if settings.jwt_signing_key:
        backend_jwt = issue_backend_jwt(
            user_id=user_id,
            email=email,
            tenant_id=tenant_id,
            scopes=token.scopes or [],
            raw_token=token.token,
        )
        return backend_jwt
    
    # OPTION 3: No exchange method available - fail with clear error
    raise TokenExchangeError(
        "Token exchange failed. Either:\n"
        "1. Implement backend /auth/token-exchange endpoint (recommended), OR\n"
        "2. Set TOOLBRIDGE_JWT_SIGNING_KEY environment variable to enable MCP-issued JWTs\n"
        f"Backend endpoint: {settings.go_api_base_url}/auth/token-exchange\n"
        f"User: {user_id}"
    )


def issue_backend_jwt(
    user_id: str,
    email: Optional[str],
    tenant_id: Optional[str],
    scopes: list[str],
    raw_token: str,
) -> str:
    """
    Issue a JWT for the backend API (Option 2).

    This allows the MCP server to issue backend JWTs without calling an endpoint.
    Requires JWT_SIGNING_KEY configured with RS256 private key.

    Args:
        user_id: User subject from MCP token (e.g., "user_abc123")
        email: User email (optional)
        tenant_id: Tenant ID from custom claim (optional)
        scopes: OAuth scopes from MCP token
        raw_token: Original MCP token (for debugging/audit)

    Returns:
        Signed JWT for backend API

    Raises:
        TokenExchangeError: If signing key not configured or invalid
    """
    if not settings.jwt_signing_key:
        raise TokenExchangeError("JWT_SIGNING_KEY not configured")
    
    # Build JWT payload for backend API
    payload = {
        # Standard claims
        "sub": user_id,  # User identity from WorkOS AuthKit
        "iss": "toolbridge-mcp",  # MCP server as issuer
        "aud": settings.backend_api_audience,  # Backend API audience
        "exp": datetime.utcnow() + timedelta(hours=1),  # 1 hour expiry
        "iat": datetime.utcnow(),
        "nbf": datetime.utcnow(),

        # Optional claims
        "email": email,
        "tenant_id": tenant_id or settings.tenant_id,  # Use from claim or fallback to config
        "scope": " ".join(scopes),

        # Metadata (for debugging)
        "token_type": "backend",
        "exchanged_from": "mcp_oauth",
    }
    
    # Sign JWT with RS256
    try:
        backend_jwt = jwt.encode(
            payload,
            settings.jwt_signing_key,
            algorithm="RS256",
        )
        
        logger.debug(f"✓ Issued backend JWT for user {user_id}, tenant {tenant_id or 'default'}")
        return backend_jwt
        
    except Exception as e:
        raise TokenExchangeError(f"Failed to sign backend JWT: {e}")


def extract_user_id_from_backend_jwt(backend_jwt: str) -> str:
    """
    Extract user ID from backend JWT without signature verification.
    
    This is safe because:
    1. We just issued/received this JWT ourselves
    2. Backend will verify signature anyway
    3. Only used for session creation logging
    
    Args:
        backend_jwt: Backend JWT token string
        
    Returns:
        User ID (sub claim)
    """
    try:
        decoded = jwt.decode(
            backend_jwt,
            options={"verify_signature": False},
        )
        return decoded.get("sub", "unknown")
    except Exception as e:
        logger.warning(f"Failed to decode backend JWT: {e}")
        return "unknown"
