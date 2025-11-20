"""
ToolBridge FastMCP Server

Main MCP server definition. Tools are registered via imports from the tools module.
"""

import atexit

from fastmcp import FastMCP
from toolbridge_mcp.config import settings
from loguru import logger

# Configure logging
logger.remove()  # Remove default handler
logger.add(
    lambda msg: print(msg, end=""),
    format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan> - <level>{message}</level>",
    level=settings.log_level,
    colorize=True,
)

# Runtime authentication mode tracking
# This tracks the *actual* auth mode in use, which may differ from configured mode
# if Auth0 initialization fails and we fall back to static/passthrough
_runtime_auth_mode = settings.auth_mode()
_auth0_init_failed = False


def get_runtime_auth_mode() -> str:
    """
    Get the actual runtime authentication mode.

    This may differ from settings.auth_mode() if Auth0 initialization failed
    and the server fell back to static/passthrough mode.

    Returns:
        Actual auth mode: "auth0", "static", or "passthrough"
    """
    return _runtime_auth_mode


def auth0_init_failed() -> bool:
    """Check if Auth0 initialization failed during startup."""
    return _auth0_init_failed


# Initialize Auth0 token manager if credentials configured
configured_auth_mode = settings.auth_mode()

if configured_auth_mode == "auth0":
    from toolbridge_mcp.auth import init_token_manager, shutdown_token_manager
    import asyncio

    try:
        init_token_manager(
            client_id=settings.auth0_client_id,
            client_secret=settings.auth0_client_secret,
            domain=settings.auth0_domain,
            audience=settings.auth0_audience,
            refresh_buffer_seconds=settings.token_refresh_buffer_seconds,
            timeout=settings.auth0_timeout_seconds,
        )
        logger.info(
            f"✓ Auth0 automatic token refresh enabled "
            f"(domain={settings.auth0_domain}, audience={settings.auth0_audience})"
        )

        # Register cleanup handler
        def cleanup():
            """Cleanup token manager on shutdown."""
            asyncio.run(shutdown_token_manager())

        atexit.register(cleanup)

    except Exception as e:
        logger.error(f"Failed to initialize Auth0 token manager: {e}")
        _auth0_init_failed = True

        # Fall back to static token if available, otherwise passthrough
        if settings.jwt_token:
            _runtime_auth_mode = "static"
            logger.warning(
                "⚠ Falling back to static JWT token mode due to Auth0 initialization failure"
            )
        else:
            _runtime_auth_mode = "passthrough"
            logger.warning(
                "⚠ Falling back to passthrough mode (per-user tokens from MCP headers) "
                "due to Auth0 initialization failure"
            )

elif configured_auth_mode == "static":
    logger.warning(
        "⚠ Using static JWT token (DEPRECATED) - "
        "Configure TOOLBRIDGE_AUTH0_CLIENT_ID/SECRET for automatic refresh"
    )

else:  # passthrough
    logger.info("Using per-user authentication mode (tokens from MCP request headers)")

# Import MCP server instance (created in mcp_instance.py to avoid circular imports)
from toolbridge_mcp.mcp_instance import mcp  # noqa: E402

# Import tools to register them with the server
# This triggers the @tool decorators which register tools with the mcp instance
from toolbridge_mcp.tools import notes  # noqa: F401, E402
from toolbridge_mcp.tools import tasks  # noqa: F401, E402
from toolbridge_mcp.tools import comments  # noqa: F401, E402
from toolbridge_mcp.tools import chats  # noqa: F401, E402
from toolbridge_mcp.tools import chat_messages  # noqa: F401, E402

logger.info("ToolBridge MCP server initialized with 40 tools (8 per entity x 5 entities)")


# Optional: Add health check endpoint
@mcp.tool()
async def health_check() -> dict:
    """Check MCP server health status."""
    from toolbridge_mcp.auth import get_token_manager

    runtime_mode = get_runtime_auth_mode()
    configured_mode = settings.auth_mode()

    status = {
        "status": "healthy",
        "tenant_id": settings.tenant_id,
        "go_api_base_url": settings.go_api_base_url,
        "auth_mode": runtime_mode,
    }

    # Show if runtime mode differs from configured mode (fallback occurred)
    if runtime_mode != configured_mode:
        status["auth_mode_note"] = (
            f"Configured for {configured_mode} but running in {runtime_mode} mode "
            f"due to initialization failure"
        )
        status["auth0_init_failed"] = True

    # Add Auth0 token status if running in auth0 mode
    if runtime_mode == "auth0":
        token_manager = get_token_manager()
        if token_manager:
            status["auth0_status"] = {
                "last_refresh_success": token_manager.last_refresh_success,
                "expires_at": (
                    token_manager.expires_at.isoformat() + "Z" if token_manager.expires_at else None
                ),
                "last_refresh_at": (
                    token_manager.last_refresh_at.isoformat() + "Z"
                    if token_manager.last_refresh_at
                    else None
                ),
                "failure_count": token_manager.failure_count,
            }

    return status


if __name__ == "__main__":
    # Run the MCP server with SSE transport for HTTP access
    mcp.run(
        transport="sse",  # Use SSE transport for HTTP/web access
        host=settings.host,
        port=settings.port,
    )
