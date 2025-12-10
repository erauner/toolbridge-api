"""
MCP-UI resource helpers for ToolBridge.

Provides utilities for creating UIResource content blocks alongside text fallbacks,
following the MCP-UI specification for interactive UI resources.
"""

import json
from enum import Enum
from typing import Dict, Any, Optional, List, Union

from mcp_ui_server import create_ui_resource
from mcp.types import TextContent, EmbeddedResource

# Type alias for content blocks that include both text and UI
UIContent = List[Union[TextContent, EmbeddedResource]]


# Default MIME type for HTML UI resources.
# For ChatGPT Apps (Skybridge), this can be set to "text/html+skybridge".
# Standard MCP-UI hosts use "text/html".
HTML_MIME_TYPE = "text/html"


class UIFormat(str, Enum):
    """Supported UI output formats."""
    HTML = "html"
    REMOTE_DOM = "remote-dom"
    BOTH = "both"


def _build_html_resource(uri: str, html: str) -> EmbeddedResource:
    """
    Build an HTML UIResource via mcp-ui-server.

    Args:
        uri: Stable ui:// URI for caching and identity
        html: HTML markup to render

    Returns:
        EmbeddedResource with mimeType set to HTML_MIME_TYPE
        (text/html for standard MCP-UI, text/html+skybridge for ChatGPT Apps)
    """
    ui_resource = create_ui_resource({
        "uri": uri,
        "content": {
            "type": "rawHtml",
            "htmlString": html,
        },
        "uiMetadata": {
            "preferred-frame-size": ["100%", "100%"],
        },
        "metadata": {
            "ai.nanobot.meta/workspace": True,
        },
        "encoding": "text",
    })

    # Enforce configured HTML MIME type (e.g., for ChatGPT Apps Skybridge compatibility)
    if hasattr(ui_resource, 'resource') and hasattr(ui_resource.resource, 'mimeType'):
        if ui_resource.resource.mimeType != HTML_MIME_TYPE:
            ui_resource.resource.mimeType = HTML_MIME_TYPE

    return ui_resource


def _build_remote_dom_resource(
    uri: str,
    dom: Dict[str, Any],
    ui_metadata: Optional[Dict[str, Any]] = None,
    metadata: Optional[Dict[str, Any]] = None,
) -> EmbeddedResource:
    """
    Build a Remote DOM UIResource.

    Args:
        uri: Stable ui:// URI for caching and identity
        dom: Remote DOM tree (root node dict) compatible with RemoteDomNode.fromJson
        ui_metadata: Optional additional uiMetadata fields (e.g., chat.frameStyle, chat.maxWidth)
        metadata: Optional additional metadata fields

    Returns:
        EmbeddedResource with application/vnd.mcp-ui.remote-dom mimeType
    """
    dom_json = json.dumps(dom, separators=(",", ":"))

    # Base uiMetadata
    base_ui_metadata: Dict[str, Any] = {
        "preferred-frame-size": ["100%", "100%"],
    }
    if ui_metadata:
        base_ui_metadata.update(ui_metadata)

    # Base metadata
    base_metadata: Dict[str, Any] = {
        "ai.nanobot.meta/workspace": True,
    }
    if metadata:
        base_metadata.update(metadata)

    return EmbeddedResource(
        type="resource",
        resource={
            "uri": uri,
            "mimeType": "application/vnd.mcp-ui.remote-dom",
            "text": dom_json,
            "uiMetadata": base_ui_metadata,
            "metadata": base_metadata,
            "encoding": "text",
        },
    )


def build_ui_with_text(
    uri: str,
    html: str,
    text_summary: str,
) -> UIContent:
    """
    Build a content list with a text fallback and an HTML UIResource.

    BACKWARDS-COMPATIBLE HTML-only builder.

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

    html_resource = _build_html_resource(uri, html)

    return [text_block, html_resource]


def build_ui_with_text_and_dom(
    uri: str,
    html: Optional[str],
    remote_dom: Optional[Dict[str, Any]],
    text_summary: str,
    ui_format: UIFormat,
    remote_dom_ui_metadata: Optional[Dict[str, Any]] = None,
    remote_dom_metadata: Optional[Dict[str, Any]] = None,
) -> UIContent:
    """
    Build UI content with text + HTML and/or Remote DOM, depending on ui_format.

    Args:
        uri: Stable ui:// URI for caching and identity
        html: HTML markup (required when ui_format is HTML or BOTH)
        remote_dom: Remote DOM tree dict (required when ui_format is REMOTE_DOM or BOTH)
        text_summary: Human-readable explanation for non-UI hosts
        ui_format: Which format(s) to include in the response
        remote_dom_ui_metadata: Optional uiMetadata for Remote DOM resource
            (e.g., {"chat.frameStyle": "card", "chat.maxWidth": 640})
        remote_dom_metadata: Optional metadata for Remote DOM resource

    Returns:
        List of content blocks:
        - Always: TextContent summary (first element)
        - HTML: EmbeddedResource with text/html (when format is HTML or BOTH)
        - Remote DOM: EmbeddedResource with application/vnd.mcp-ui.remote-dom (when format is REMOTE_DOM or BOTH)

    Raises:
        ValueError: If required content (html/remote_dom) is None for the specified format

    Example:
        >>> content = build_ui_with_text_and_dom(
        ...     uri="ui://toolbridge/notes/list",
        ...     html="<ul><li>Note 1</li></ul>",
        ...     remote_dom={"type": "column", "children": [...]},
        ...     text_summary="Showing 5 notes",
        ...     ui_format=UIFormat.BOTH,
        ...     remote_dom_ui_metadata={"chat.frameStyle": "card", "chat.maxWidth": 640},
        ... )
    """
    content: UIContent = [
        TextContent(type="text", text=text_summary),
    ]

    if ui_format in (UIFormat.HTML, UIFormat.BOTH):
        if html is None:
            raise ValueError("html must be provided for ui_format=html/both")
        content.append(_build_html_resource(uri, html))

    if ui_format in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        if remote_dom is None:
            raise ValueError("remote_dom must be provided for ui_format=remote-dom/both")
        content.append(_build_remote_dom_resource(
            uri,
            remote_dom,
            ui_metadata=remote_dom_ui_metadata,
            metadata=remote_dom_metadata,
        ))

    return content
