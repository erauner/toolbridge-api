"""
HTML template helpers for MCP-UI resources.

This package contains template rendering functions that convert ToolBridge
data models into HTML for display in MCP-UI compatible hosts.

NOTE: Initial templates are minimal stubs. A separate ticket will add
proper styled templates with CSS, interactivity, and MCP-UI actions.
"""

from toolbridge_mcp.ui.templates import notes
from toolbridge_mcp.ui.templates import tasks

__all__ = ["notes", "tasks"]
