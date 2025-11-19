"""
Session management for MCP tool requests.

Each MCP tool invocation creates a new sync session with the Go API.
This session-per-request approach is simple and doesn't require cleanup.

Note: Sessions are NOT cached or reused. Every call to create_session
creates a fresh session to avoid stale session issues when sessions expire.
"""

from typing import Dict

import httpx
from loguru import logger

from toolbridge_mcp.config import settings


class SessionError(Exception):
    """Raised when session creation fails."""
    pass


async def create_session(client: httpx.AsyncClient, auth_header: str, user_id: str) -> Dict[str, str]:
    """
    Create a new sync session with the Go API.

    This always creates a fresh session - no caching or reuse.
    This ensures MCP tools can recover from session expiration.

    Args:
        client: httpx client (with TenantDirectTransport)
        auth_header: Authorization header (e.g., "Bearer eyJ...")
        user_id: User ID to create session for (extracted from JWT sub claim)

    Returns:
        Dict with session headers: {"X-Sync-Session": "...", "X-Sync-Epoch": "..."}

    Raises:
        SessionError: If session creation fails
        httpx.HTTPStatusError: If request fails
    """
    try:
        logger.debug(f"Creating fresh sync session for user: {user_id}")

        response = await client.post(
            "/v1/sync/sessions",
            headers={
                "Authorization": auth_header,
                "X-Debug-Sub": user_id,  # In dev mode, this sets the user
            },
        )
        response.raise_for_status()

        data = response.json()
        session_id = data["id"]
        session_epoch = data["epoch"]

        session_headers = {
            "X-Sync-Session": session_id,
            "X-Sync-Epoch": str(session_epoch),
        }

        logger.debug(f"✓ Session created: {session_id} (epoch={session_epoch})")

        return session_headers

    except httpx.HTTPStatusError as e:
        logger.error(f"Failed to create session: {e.response.status_code} {e.response.text}")
        raise SessionError(f"Session creation failed: {e}") from e
    except Exception as e:
        logger.error(f"Unexpected error creating session: {e}")
        raise SessionError(f"Session creation failed: {e}") from e
