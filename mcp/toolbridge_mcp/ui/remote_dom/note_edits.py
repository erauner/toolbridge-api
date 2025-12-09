"""Remote DOM templates for Note Edit UI.

Builds Remote DOM tree structures for diff preview rendering via RemoteDomView.
Uses design tokens for consistent styling with native ToolBridge UI.
"""

from typing import Dict, Any, List, TYPE_CHECKING

from toolbridge_mcp.ui.remote_dom.design import (
    TextStyle,
    Spacing,
    Layout,
    Color,
    Icon,
    ButtonVariant,
    text_node,
    get_chat_metadata,
)

if TYPE_CHECKING:
    from toolbridge_mcp.tools.notes import Note
    from toolbridge_mcp.utils.diff import DiffHunk


def render_note_edit_diff_dom(
    note: "Note",
    diff_hunks: List["DiffHunk"],
    edit_id: str,
    summary: str | None = None,
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for the note edit diff preview.
    
    Args:
        note: The current note being edited
        diff_hunks: List of diff hunks from compute_line_diff
        edit_id: The edit session ID for action payloads
        summary: Optional summary of the changes
        
    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    title = (note.payload.get("title") or "Untitled note").strip()
    
    children: List[Dict[str, Any]] = [
        # Header with icon and title
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.EDIT, "size": 24, "color": Color.PRIMARY}},
                text_node("Proposed changes", TextStyle.HEADLINE_MEDIUM),
            ],
        },
        # Subtitle with note title and version
        text_node(
            f"{title} (v{note.version})",
            TextStyle.BODY_SMALL,
            Color.ON_SURFACE_VARIANT,
        ),
    ]
    
    # Add summary if provided
    if summary:
        children.append(
            text_node(summary, TextStyle.BODY_MEDIUM, Color.ON_SURFACE),
        )
    
    # Diff card containing all hunks
    diff_children: List[Dict[str, Any]] = []
    for hunk in diff_hunks:
        hunk_node = _render_diff_hunk(hunk)
        if hunk_node:
            diff_children.append(hunk_node)
    
    if diff_children:
        children.append({
            "type": "card",
            "props": {"padding": 20},
            "children": [
                {
                    "type": "column",
                    "props": {
                        "gap": Spacing.GAP_MD,
                        "crossAxisAlignment": "stretch",
                    },
                    "children": diff_children,
                }
            ],
        })
    
    # Action row (Accept / Discard)
    children.append({
        "type": "row",
        "props": {
            "gap": Spacing.GAP_SM,
            "mainAxisAlignment": "end",
        },
        "children": [
            {
                "type": "button",
                "props": {
                    "label": "Discard",
                    "variant": ButtonVariant.TEXT,
                    "icon": Icon.CLOSE,
                },
                "action": {
                    "type": "tool",
                    "payload": {
                        "toolName": "discard_note_edit",
                        "params": {"edit_id": edit_id},
                    },
                },
            },
            {
                "type": "button",
                "props": {
                    "label": "Apply changes",
                    "variant": ButtonVariant.PRIMARY,
                    "icon": Icon.CHECK,
                },
                "action": {
                    "type": "tool",
                    "payload": {
                        "toolName": "apply_note_edit",
                        "params": {"edit_id": edit_id},
                    },
                },
            },
        ],
    })
    
    # Build root props
    root_props = {
        "gap": Spacing.SECTION_GAP,
        "padding": 24,
        "fullWidth": True,
        "crossAxisAlignment": "stretch",
    }
    if Layout.MAX_WIDTH_DETAIL is not None:
        root_props["maxWidth"] = Layout.MAX_WIDTH_DETAIL
    
    return {
        "type": "column",
        "props": root_props,
        "children": children,
    }


def _render_diff_hunk(hunk: "DiffHunk") -> Dict[str, Any] | None:
    """Render a single diff hunk as a Remote DOM node."""
    if hunk.kind == "unchanged":
        if not hunk.original:
            return None
        return {
            "type": "container",
            "props": {
                "padding": 12,
                "color": Color.SURFACE_CONTAINER_LOW,
                "borderRadius": 8,
            },
            "children": [
                text_node(hunk.original, TextStyle.BODY_SMALL, Color.ON_SURFACE_VARIANT),
            ],
        }
    
    elif hunk.kind == "removed":
        return {
            "type": "container",
            "props": {
                "padding": 12,
                "color": Color.ERROR_CONTAINER,
                "borderRadius": 8,
            },
            "children": [
                {
                    "type": "column",
                    "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "stretch"},
                    "children": [
                        {
                            "type": "row",
                            "props": {"gap": Spacing.GAP_XS, "crossAxisAlignment": "center"},
                            "children": [
                                {"type": "icon", "props": {"icon": "remove_circle", "size": 16, "color": Color.ON_ERROR_CONTAINER}},
                                text_node("Removed", TextStyle.LABEL_MEDIUM, Color.ON_ERROR_CONTAINER),
                            ],
                        },
                        text_node(hunk.original, TextStyle.BODY_SMALL, Color.ON_ERROR_CONTAINER),
                    ],
                },
            ],
        }
    
    elif hunk.kind == "added":
        return {
            "type": "container",
            "props": {
                "padding": 12,
                "color": Color.PRIMARY_CONTAINER,
                "borderRadius": 8,
            },
            "children": [
                {
                    "type": "column",
                    "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "stretch"},
                    "children": [
                        {
                            "type": "row",
                            "props": {"gap": Spacing.GAP_XS, "crossAxisAlignment": "center"},
                            "children": [
                                {"type": "icon", "props": {"icon": "add_circle", "size": 16, "color": Color.ON_PRIMARY_CONTAINER}},
                                text_node("Added", TextStyle.LABEL_MEDIUM, Color.ON_PRIMARY_CONTAINER),
                            ],
                        },
                        text_node(hunk.proposed, TextStyle.BODY_SMALL, Color.ON_PRIMARY_CONTAINER),
                    ],
                },
            ],
        }
    
    elif hunk.kind == "modified":
        # Two stacked containers for original and proposed
        children: List[Dict[str, Any]] = []
        
        if hunk.original:
            children.append({
                "type": "container",
                "props": {
                    "padding": 12,
                    "color": Color.ERROR_CONTAINER,
                    "borderRadius": 8,
                },
                "children": [
                    {
                        "type": "column",
                        "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "stretch"},
                        "children": [
                            {
                                "type": "row",
                                "props": {"gap": Spacing.GAP_XS, "crossAxisAlignment": "center"},
                                "children": [
                                    {"type": "icon", "props": {"icon": "remove_circle", "size": 16, "color": Color.ON_ERROR_CONTAINER}},
                                    text_node("Original", TextStyle.LABEL_MEDIUM, Color.ON_ERROR_CONTAINER),
                                ],
                            },
                            text_node(hunk.original, TextStyle.BODY_SMALL, Color.ON_ERROR_CONTAINER),
                        ],
                    },
                ],
            })
        
        if hunk.proposed:
            children.append({
                "type": "container",
                "props": {
                    "padding": 12,
                    "color": Color.PRIMARY_CONTAINER,
                    "borderRadius": 8,
                },
                "children": [
                    {
                        "type": "column",
                        "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "stretch"},
                        "children": [
                            {
                                "type": "row",
                                "props": {"gap": Spacing.GAP_XS, "crossAxisAlignment": "center"},
                                "children": [
                                    {"type": "icon", "props": {"icon": "add_circle", "size": 16, "color": Color.ON_PRIMARY_CONTAINER}},
                                    text_node("Proposed", TextStyle.LABEL_MEDIUM, Color.ON_PRIMARY_CONTAINER),
                                ],
                            },
                            text_node(hunk.proposed, TextStyle.BODY_SMALL, Color.ON_PRIMARY_CONTAINER),
                        ],
                    },
                ],
            })
        
        return {
            "type": "column",
            "props": {"gap": 0, "crossAxisAlignment": "stretch"},
            "children": children,
        }
    
    return None


def render_note_edit_success_dom(
    note: "Note",
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for successful note edit confirmation.
    
    Args:
        note: The updated note after applying changes
        
    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    title = (note.payload.get("title") or "Untitled note").strip()
    
    children: List[Dict[str, Any]] = [
        # Success header
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.CHECK_CIRCLE, "size": 24, "color": Color.PRIMARY}},
                text_node("Changes applied", TextStyle.HEADLINE_MEDIUM),
            ],
        },
        # Note info
        text_node(
            f"{title} updated to v{note.version}",
            TextStyle.BODY_MEDIUM,
            Color.ON_SURFACE,
        ),
    ]
    
    # Build root props
    root_props = {
        "gap": Spacing.GAP_MD,
        "padding": 24,
        "fullWidth": True,
        "crossAxisAlignment": "stretch",
    }
    
    return {
        "type": "column",
        "props": root_props,
        "children": children,
    }


def render_note_edit_discarded_dom(
    title: str,
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for discarded note edit confirmation.
    
    Args:
        title: The note title
        
    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    children: List[Dict[str, Any]] = [
        # Info header
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.CLOSE, "size": 24, "color": Color.ON_SURFACE_VARIANT}},
                text_node("Changes discarded", TextStyle.HEADLINE_MEDIUM),
            ],
        },
        text_node(
            f"Pending edits for '{title}' have been discarded.",
            TextStyle.BODY_MEDIUM,
            Color.ON_SURFACE_VARIANT,
        ),
    ]
    
    # Build root props
    root_props = {
        "gap": Spacing.GAP_MD,
        "padding": 24,
        "fullWidth": True,
        "crossAxisAlignment": "stretch",
    }
    
    return {
        "type": "column",
        "props": root_props,
        "children": children,
    }


def render_note_edit_error_dom(
    error_message: str,
    note_uid: str | None = None,
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for note edit error.
    
    Args:
        error_message: The error message to display
        note_uid: Optional note UID for retry suggestion
        
    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    children: List[Dict[str, Any]] = [
        # Error header
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.ERROR, "size": 24, "color": Color.ERROR}},
                text_node("Failed to apply changes", TextStyle.HEADLINE_MEDIUM),
            ],
        },
        text_node(error_message, TextStyle.BODY_MEDIUM, Color.ON_ERROR_CONTAINER),
    ]
    
    if note_uid:
        children.append(
            text_node(
                "The note may have been modified. Please re-run edit_note_ui to create a fresh diff.",
                TextStyle.BODY_SMALL,
                Color.ON_SURFACE_VARIANT,
            )
        )
    
    # Build root props
    root_props = {
        "gap": Spacing.GAP_MD,
        "padding": 24,
        "fullWidth": True,
        "crossAxisAlignment": "stretch",
        "color": Color.ERROR_CONTAINER,
        "borderRadius": 12,
    }
    
    return {
        "type": "column",
        "props": root_props,
        "children": children,
    }
