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

# Create MCP server instance
mcp = FastMCP(
    name="ToolBridge",
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
    # For development: run with uvicorn
    import uvicorn
    
    uvicorn.run(
        "toolbridge_mcp.server:mcp",
        host=settings.host,
        port=settings.port,
        reload=True,
        log_level=settings.log_level.lower(),
    )
