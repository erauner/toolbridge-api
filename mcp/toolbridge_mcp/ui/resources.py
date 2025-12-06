"""
MCP-UI resource helpers for ToolBridge.

Provides utilities for creating UIResource content blocks alongside text fallbacks,
following the MCP-UI specification for interactive UI resources.
"""

from typing import List, Union

from mcp_ui_server import create_ui_resource
from mcp.types import TextContent, EmbeddedResource

# Type alias for content blocks that include both text and UI
UIContent = List[Union[TextContent, EmbeddedResource]]


def build_ui_with_text(
    uri: str,
    html: str,
    text_summary: str,
) -> UIContent:
    """
    Build a content list with a text fallback and a UIResource.

    This follows the MCP-UI pattern where tools return both:
    - TextContent: Human-readable fallback for non-UI hosts (e.g., Claude Desktop text mode)
    - UIResource: Interactive HTML for MCP-UI aware hosts (e.g., Goose, Nanobot)

    Args:
        uri: Stable ui:// URI for caching and identity (e.g., "ui://toolbridge/notes/list")
        html: HTML markup to render (from templates)
        text_summary: Human-readable explanation for non-UI hosts

    Returns:
        List of content blocks [TextContent, EmbeddedResource]
        Order matters: most hosts show blocks in array order.

    Example:
        >>> content = build_ui_with_text(
        ...     uri="ui://toolbridge/notes/list",
        ...     html="<ul><li>Note 1</li></ul>",
        ...     text_summary="Showing 5 notes"
        ... )
    """
    text_block = TextContent(
        type="text",
        text=text_summary,
    )

    # create_ui_resource returns an EmbeddedResource compatible with MCP content blocks
    ui_resource = create_ui_resource({
        "uri": uri,
        "content": {
            "type": "rawHtml",
            "htmlString": html,
        },
        "encoding": "text",
    })

    return [text_block, ui_resource]
