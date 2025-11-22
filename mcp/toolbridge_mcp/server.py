"""
ToolBridge FastMCP Server with OAuth 2.1 Authentication

Main MCP server definition with per-user OAuth authentication.
Tools are registered via imports from the tools module.
"""

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

logger.info("🚀 ToolBridge MCP Server - WorkOS AuthKit Mode")
logger.info(f"✓ WorkOS AuthKit domain: {settings.authkit_domain}")
logger.info(f"✓ Backend API audience: {settings.backend_api_audience}")
logger.info(f"✓ MCP public URL: {settings.public_base_url}")
logger.info(
    f"✓ OAuth protected resource metadata: "
    f"{settings.public_base_url}/.well-known/oauth-protected-resource"
)

# Import MCP server instance (created in mcp_instance.py with AuthKitProvider)
from toolbridge_mcp.mcp_instance import mcp  # noqa: E402

# Import tools to register them with the server
# This triggers the @tool decorators which register tools with the mcp instance
from toolbridge_mcp.tools import notes  # noqa: F401, E402
from toolbridge_mcp.tools import tasks  # noqa: F401, E402
from toolbridge_mcp.tools import comments  # noqa: F401, E402
from toolbridge_mcp.tools import chats  # noqa: F401, E402
from toolbridge_mcp.tools import chat_messages  # noqa: F401, E402

logger.info("✓ ToolBridge MCP server initialized with 40 tools (8 per entity x 5 entities)")


# Health check endpoint (FastMCP-authenticated)
@mcp.tool()
async def health_check() -> dict:
    """
    Check MCP server health status.

    Note: This endpoint requires WorkOS AuthKit authentication via FastMCP.
    """
    return {
        "status": "healthy",
        "tenant_id": settings.tenant_id,
        "go_api_base_url": settings.go_api_base_url,
        "authkit_domain": settings.authkit_domain,
        "public_base_url": settings.public_base_url,
        "backend_api_audience": settings.backend_api_audience,
    }


if __name__ == "__main__":
    # Run the MCP server with Streamable HTTP transport at /mcp
    logger.info(f"🌐 Starting HTTP transport on {settings.host}:{settings.port} at /mcp")
    logger.info(f"✓ MCP endpoint: {settings.public_base_url}/mcp")

    mcp.run(
        transport="http",  # Use Streamable HTTP transport
        host=settings.host,
        port=settings.port,
        path="/mcp",  # MCP endpoint path
    )
