"""
ToolBridge FastMCP Server

Main MCP server definition. Tools are registered via imports from the tools module.
"""

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

# Configure authentication
# Currently using shared JWT token mode - MCP server accepts unauthenticated requests
# and uses configured JWT token for backend API authentication.
# TODO: Add OAuth/PKCE support for per-user authentication in future PR
auth_provider = None

if settings.jwt_token:
    logger.info("Using shared JWT token for backend API authentication")
else:
    logger.warning("No JWT token configured - backend API calls will fail")

# Create MCP server instance
mcp = FastMCP(
    name="ToolBridge",
    auth=auth_provider,
)

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
    return {
        "status": "healthy",
        "tenant_id": settings.tenant_id,
        "go_api_base_url": settings.go_api_base_url,
    }


if __name__ == "__main__":
    # Run the MCP server with SSE transport for HTTP access
    mcp.run(
        transport="sse",  # Use SSE transport for HTTP/web access
        host=settings.host,
        port=settings.port,
    )
