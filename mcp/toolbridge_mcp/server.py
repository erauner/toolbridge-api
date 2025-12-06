"""
ToolBridge FastMCP Server with OAuth 2.1 Authentication

Main MCP server definition with per-user OAuth authentication.
Tools are registered via imports from the tools module.
"""

from toolbridge_mcp.config import settings
from loguru import logger
import sys

# Custom filter to improve OAuth token expiration logging
class OAuthTokenFilter:
    """Filter to provide better context for OAuth token expiration messages."""

    def __call__(self, record):
        # Check if this is an invalid_token auth error from FastMCP middleware
        if (
            record["name"] == "fastmcp.server.auth.middleware"
            and "Auth error returned: invalid_token" in record["message"]
        ):
            # Replace with more informative message
            record["message"] = (
                "🔄 OAuth token expired - client will automatically re-authenticate "
                "(this is normal and expected)"
            )
            # Optionally lower the level to DEBUG instead of INFO to reduce noise
            record["level"] = logger.level("DEBUG")
        return True

# Configure logging
logger.remove()  # Remove default handler
logger.add(
    lambda msg: print(msg, end=""),
    format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan> - <level>{message}</level>",
    level=settings.log_level,
    colorize=True,
    filter=OAuthTokenFilter(),
)

logger.info("🚀 ToolBridge MCP Server - WorkOS AuthKit Mode")
logger.info(f"✓ WorkOS AuthKit domain: {settings.authkit_domain}")
logger.info(f"✓ Backend API audience: {settings.backend_api_audience}")
logger.info(f"✓ MCP public URL: {settings.public_base_url}")
logger.info(
    f"✓ OAuth protected resource metadata: "
    f"{settings.public_base_url}/.well-known/oauth-protected-resource"
)

# Log tenant mode configuration
if settings.tenant_id:
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    logger.warning(f"⚠️  SINGLE-TENANT MODE: Using configured tenant {settings.tenant_id}")
    logger.warning("⚠️  This mode is for smoke testing only. Production should use multi-tenant mode.")
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
else:
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    logger.info("🔐 MULTI-TENANT MODE (B2C/B2B Hybrid): Backend-driven tenant resolution")
    logger.info("✓ Tenants resolved dynamically via /v1/auth/tenant endpoint")
    logger.info("✓ B2C users (no org memberships) → tenant_thinkpen_b2c (backend default)")
    logger.info("✓ B2B users (single org) → organization ID")
    logger.info("✓ Multi-org users → require organization selection")
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

# Import MCP server instance (created in mcp_instance.py with AuthKitProvider)
from toolbridge_mcp.mcp_instance import mcp  # noqa: E402

# Import tools to register them with the server
# This triggers the @tool decorators which register tools with the mcp instance
from toolbridge_mcp.tools import notes  # noqa: F401, E402
from toolbridge_mcp.tools import tasks  # noqa: F401, E402
from toolbridge_mcp.tools import comments  # noqa: F401, E402
from toolbridge_mcp.tools import chats  # noqa: F401, E402
from toolbridge_mcp.tools import chat_messages  # noqa: F401, E402

# Import MCP-UI enabled tools (return both text and UIResource)
from toolbridge_mcp.tools import notes_ui  # noqa: F401, E402
from toolbridge_mcp.tools import tasks_ui  # noqa: F401, E402

logger.info("✓ ToolBridge MCP server initialized with 44 tools (40 data + 4 UI)")

# Note: health_check tool is provided by FastMCP by default
# No need to register a custom one to avoid "Tool already exists" warnings

# Create ASGI app for Streamable HTTP transport
# This exposes /mcp endpoint and OAuth protected resource metadata at /.well-known/*
# We use mcp.http_app() instead of mcp.run() to gain explicit control over uvicorn
# shutdown behavior (critical for clean Fly.io auto-stop on scale-to-zero)
app = mcp.http_app()


if __name__ == "__main__":
    import asyncio
    import signal
    import uvicorn

    logger.info(f"🌐 Starting Uvicorn on {settings.host}:{settings.port} (path=/mcp)")
    logger.info(f"✓ MCP endpoint: {settings.public_base_url}/mcp")
    logger.info(
        f"✓ Graceful shutdown timeout: {settings.shutdown_timeout_seconds}s "
        f"(Fly kill_timeout should be > {settings.shutdown_timeout_seconds}s)"
    )

    async def serve() -> None:
        """Run uvicorn with explicit signal handling for graceful shutdown."""
        config = uvicorn.Config(
            "toolbridge_mcp.server:app",
            host=settings.host,
            port=settings.port,
            log_level=settings.log_level.lower(),
            access_log=settings.uvicorn_access_log,
            timeout_graceful_shutdown=settings.shutdown_timeout_seconds,
        )
        server = uvicorn.Server(config)

        loop = asyncio.get_running_loop()

        def handle_exit(sig: int, *_: object) -> None:
            """Handle SIGTERM/SIGINT gracefully without noisy stack traces."""
            logger.info(f"Received signal {sig}, initiating graceful shutdown")
            server.should_exit = True

        # Register signal handlers for graceful shutdown
        for sig in (signal.SIGINT, signal.SIGTERM):
            try:
                loop.add_signal_handler(sig, handle_exit, sig)
            except NotImplementedError:
                # Non-POSIX platforms (not relevant for Fly.io)
                pass

        await server.serve()

    asyncio.run(serve())
