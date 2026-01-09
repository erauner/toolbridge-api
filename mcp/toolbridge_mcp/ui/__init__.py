"""
ToolBridge MCP-UI module.

Provides UI resource helpers and template rendering for MCP-UI compatible hosts.
Supports both HTML templates and Remote DOM for native Flutter rendering.

Also supports ChatGPT Apps SDK integration via structuredContent and tool _meta.
"""

from toolbridge_mcp.ui.resources import (
    build_ui_with_text,
    build_ui_with_text_and_dom,
    build_ui_with_structured_content,
    UIContent,
    UIContentWithStructured,
    UIFormat,
)

__all__ = [
    "build_ui_with_text",
    "build_ui_with_text_and_dom",
    "build_ui_with_structured_content",
    "UIContent",
    "UIContentWithStructured",
    "UIFormat",
]
