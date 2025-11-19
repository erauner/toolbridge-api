"""
MCP server instance.

This module creates the MCP server instance that can be imported by both
server.py and tool modules, avoiding circular import issues.
"""

from fastmcp import FastMCP

# Create MCP server instance (no auth provider - using shared JWT token mode)
mcp = FastMCP(
    name="ToolBridge",
    auth=None,
)
