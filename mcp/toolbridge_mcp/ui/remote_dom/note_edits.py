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
    from toolbridge_mcp.note_edit_sessions import NoteEditHunkState


# Status colors for hunk backgrounds
STATUS_BG = {
    "pending": None,  # Use default
    "accepted": "#1c4428",  # Dark green
    "rejected": "#5c1a1b",  # Dark red
    "revised": "#2d3a4d",   # Dark blue
}

STATUS_BORDER = {
    "pending": "#6e7681",   # Gray
    "accepted": "#3fb950",  # Green
    "rejected": "#f85149",  # Red
    "revised": "#58a6ff",   # Blue
}


def render_note_edit_diff_dom(
    note: "Note",
    hunks: List["NoteEditHunkState"],
    edit_id: str,
    summary: str | None = None,
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for the note edit diff preview with per-hunk actions.
    
    Args:
        note: The current note being edited
        hunks: List of NoteEditHunkState from the session
        edit_id: The edit session ID for action payloads
        summary: Optional summary of the changes
        
    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    title = (note.payload.get("title") or "Untitled note").strip()
    
    # Calculate status counts (excluding unchanged)
    status_counts = {"pending": 0, "accepted": 0, "rejected": 0, "revised": 0}
    for h in hunks:
        if h.kind != "unchanged":
            status_counts[h.status] = status_counts.get(h.status, 0) + 1
    
    total_changes = sum(status_counts.values())
    has_pending = status_counts["pending"] > 0
    
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
    
    # Status counts row
    if total_changes > 0:
        status_chips: List[Dict[str, Any]] = []
        if status_counts["pending"] > 0:
            status_chips.append({
                "type": "chip",
                "props": {
                    "label": f"{status_counts['pending']} pending",
                    "variant": "outlined",
                },
            })
        if status_counts["accepted"] > 0:
            status_chips.append({
                "type": "chip",
                "props": {
                    "label": f"{status_counts['accepted']} accepted",
                    "variant": "assist",
                    "icon": Icon.CHECK,
                },
            })
        if status_counts["rejected"] > 0:
            status_chips.append({
                "type": "chip",
                "props": {
                    "label": f"{status_counts['rejected']} rejected",
                    "variant": "assist",
                    "icon": Icon.CLOSE,
                },
            })
        if status_counts["revised"] > 0:
            status_chips.append({
                "type": "chip",
                "props": {
                    "label": f"{status_counts['revised']} revised",
                    "variant": "assist",
                    "icon": Icon.EDIT,
                },
            })
        
        if status_chips:
            children.append({
                "type": "wrap",
                "props": {"gap": Spacing.GAP_XS, "runSpacing": Spacing.GAP_XS},
                "children": status_chips,
            })
    
    # Render each hunk as a separate block
    for hunk in hunks:
        hunk_node = _render_hunk_block(edit_id, hunk)
        if hunk_node:
            children.append(hunk_node)
    
    # Action row (Apply / Discard)
    apply_label = "Apply changes" if not has_pending else f"Resolve {status_counts['pending']} pending to apply"
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
                    "label": "Discard all",
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
                    "label": apply_label,
                    "variant": ButtonVariant.PRIMARY,
                    "icon": Icon.CHECK,
                    "enabled": not has_pending,
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


def _render_hunk_block(edit_id: str, hunk: "NoteEditHunkState") -> Dict[str, Any] | None:
    """
    Render a single hunk as a card with status indicator and per-hunk actions.
    
    Args:
        edit_id: The edit session ID for action payloads
        hunk: The hunk state to render
        
    Returns:
        A container node for the hunk, or None if nothing to render
    """
    if hunk.kind == "unchanged":
        # For unchanged hunks, show abbreviated context
        if not hunk.original:
            return None
        
        lines = hunk.original.split('\n')
        if len(lines) > 3:
            context_text = f"... ({len(lines)} unchanged lines) ..."
        else:
            context_text = hunk.original
        
        return {
            "type": "container",
            "props": {
                "padding": 8,
                "color": DIFF_CONTEXT_BG,
                "borderRadius": 4,
            },
            "children": [
                text_node(context_text, TextStyle.BODY_SMALL, DIFF_CONTEXT_TEXT),
            ],
        }
    
    # Changed hunk - build card with header, diff, and actions
    children: List[Dict[str, Any]] = []
    
    # Header row: status chip + line range
    header_children: List[Dict[str, Any]] = []
    
    # Status chip
    status_label = hunk.status.capitalize()
    status_icon = {
        "pending": Icon.PENDING,
        "accepted": Icon.CHECK,
        "rejected": Icon.CLOSE,
        "revised": Icon.EDIT,
    }.get(hunk.status, Icon.PENDING)
    
    header_children.append({
        "type": "chip",
        "props": {
            "label": status_label,
            "variant": "outlined" if hunk.status == "pending" else "filled",
            "icon": status_icon,
        },
    })
    
    # Kind + line range
    kind_labels = {
        "added": "Added",
        "removed": "Removed",
        "modified": "Modified",
    }
    kind_label = kind_labels.get(hunk.kind, hunk.kind.capitalize())
    
    line_info = ""
    if hunk.orig_start is not None and hunk.orig_end is not None:
        if hunk.orig_start == hunk.orig_end:
            line_info = f"line {hunk.orig_start}"
        else:
            line_info = f"lines {hunk.orig_start}-{hunk.orig_end}"
    elif hunk.new_start is not None and hunk.new_end is not None:
        if hunk.new_start == hunk.new_end:
            line_info = f"line {hunk.new_start}"
        else:
            line_info = f"lines {hunk.new_start}-{hunk.new_end}"
    
    header_text = f"{kind_label}"
    if line_info:
        header_text += f" ({line_info})"
    
    header_children.append(
        text_node(header_text, TextStyle.BODY_MEDIUM, Color.ON_SURFACE_VARIANT)
    )
    
    children.append({
        "type": "row",
        "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
        "children": header_children,
    })
    
    # Diff content
    diff_content = _render_diff_content(hunk.kind, hunk.original, hunk.proposed, hunk.revised_text)
    if diff_content:
        children.append(diff_content)
    
    # Action buttons (only for pending hunks)
    if hunk.status == "pending":
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
                        "label": "Reject",
                        "variant": ButtonVariant.TEXT,
                        "icon": Icon.CLOSE,
                    },
                    "action": {
                        "type": "tool",
                        "payload": {
                            "toolName": "reject_note_edit_hunk",
                            "params": {"edit_id": edit_id, "hunk_id": hunk.id},
                        },
                    },
                },
                {
                    "type": "button",
                    "props": {
                        "label": "Revise...",
                        "variant": ButtonVariant.SECONDARY,
                        "icon": Icon.EDIT,
                    },
                    "action": {
                        "type": "tool",
                        "payload": {
                            "toolName": "revise_note_edit_hunk",
                            "params": {
                                "edit_id": edit_id,
                                "hunk_id": hunk.id,
                                "needsInput": True,
                                "prompt": "Enter replacement text for this change",
                            },
                        },
                    },
                },
                {
                    "type": "button",
                    "props": {
                        "label": "Accept",
                        "variant": ButtonVariant.PRIMARY,
                        "icon": Icon.CHECK,
                    },
                    "action": {
                        "type": "tool",
                        "payload": {
                            "toolName": "accept_note_edit_hunk",
                            "params": {"edit_id": edit_id, "hunk_id": hunk.id},
                        },
                    },
                },
            ],
        })
    
    # Determine card background based on status
    card_props: Dict[str, Any] = {
        "padding": 16,
        "borderRadius": 8,
    }
    
    bg_color = STATUS_BG.get(hunk.status)
    if bg_color:
        card_props["color"] = bg_color
    
    border_color = STATUS_BORDER.get(hunk.status)
    if border_color:
        card_props["borderColor"] = border_color
        card_props["borderWidth"] = 1
    
    return {
        "type": "container",
        "props": card_props,
        "children": [
            {
                "type": "column",
                "props": {
                    "gap": Spacing.GAP_SM,
                    "crossAxisAlignment": "stretch",
                },
                "children": children,
            }
        ],
    }


def _render_diff_content(
    kind: str,
    original: str,
    proposed: str,
    revised_text: str | None = None,
) -> Dict[str, Any] | None:
    """
    Render the diff content for a hunk (removed/added lines).
    
    Args:
        kind: The hunk kind ('added', 'removed', 'modified')
        original: Original text
        proposed: Proposed text
        revised_text: Optional revised text if status is 'revised'
        
    Returns:
        A column node with diff lines, or None if nothing to render
    """
    children: List[Dict[str, Any]] = []
    
    # Use revised_text if available
    display_proposed = revised_text if revised_text is not None else proposed
    
    if kind == "removed":
        # Only show removed lines
        for line in original.split('\n'):
            children.append(_render_diff_line(line, is_added=False))
    
    elif kind == "added":
        # Only show added lines
        for line in display_proposed.split('\n'):
            children.append(_render_diff_line(line, is_added=True))
    
    elif kind == "modified":
        # Show removed then added
        if original:
            for line in original.split('\n'):
                children.append(_render_diff_line(line, is_added=False))
        if display_proposed:
            for line in display_proposed.split('\n'):
                children.append(_render_diff_line(line, is_added=True))
    
    if not children:
        return None
    
    return {
        "type": "column",
        "props": {"gap": 0, "crossAxisAlignment": "stretch"},
        "children": children,
    }


# GitHub-style diff colors (hex)
# These work well in both light and dark modes
DIFF_ADDED_BG = "#1c4428"      # Dark green background
DIFF_ADDED_TEXT = "#3fb950"    # Bright green text
DIFF_REMOVED_BG = "#5c1a1b"    # Dark red background
DIFF_REMOVED_TEXT = "#f85149"  # Bright red text
DIFF_CONTEXT_BG = "#21262d"    # Dark gray background
DIFF_CONTEXT_TEXT = "#8b949e"  # Gray text


def _render_diff_line(text: str, is_added: bool) -> Dict[str, Any]:
    """Render a single diff line with +/- prefix (GitHub-style)."""
    prefix = "+ " if is_added else "- "
    text_color = DIFF_ADDED_TEXT if is_added else DIFF_REMOVED_TEXT
    bg_color = DIFF_ADDED_BG if is_added else DIFF_REMOVED_BG

    return {
        "type": "container",
        "props": {
            "padding": 8,
            "color": bg_color,
        },
        "children": [
            {
                "type": "text",
                "props": {
                    "text": f"{prefix}{text}",
                    "style": TextStyle.BODY_SMALL,
                    "color": text_color,
                },
            },
        ],
    }


def _render_context_line(text: str) -> Dict[str, Any]:
    """Render an unchanged context line."""
    return {
        "type": "container",
        "props": {
            "padding": 8,
            "color": DIFF_CONTEXT_BG,
        },
        "children": [
            {
                "type": "text",
                "props": {
                    "text": f"  {text}",
                    "style": TextStyle.BODY_SMALL,
                    "color": DIFF_CONTEXT_TEXT,
                },
            },
        ],
    }


def _render_diff_hunk(hunk: "DiffHunk") -> Dict[str, Any] | None:
    """Render a single diff hunk as GitHub-style diff lines."""
    if hunk.kind == "unchanged":
        if not hunk.original:
            return None
        # Show context lines (up to 3 lines for brevity)
        lines = hunk.original.split('\n')
        if len(lines) > 3:
            # Show first line, ellipsis, last line
            children = [
                _render_context_line(lines[0]),
                {
                    "type": "container",
                    "props": {"padding": 4, "color": DIFF_CONTEXT_BG},
                    "children": [
                        text_node(f"  ... ({len(lines) - 2} more lines)", TextStyle.BODY_SMALL, DIFF_CONTEXT_TEXT),
                    ],
                },
                _render_context_line(lines[-1]),
            ]
        else:
            children = [_render_context_line(line) for line in lines]

        return {
            "type": "column",
            "props": {"gap": 0, "crossAxisAlignment": "stretch"},
            "children": children,
        }

    elif hunk.kind == "removed":
        lines = hunk.original.split('\n')
        return {
            "type": "column",
            "props": {"gap": 0, "crossAxisAlignment": "stretch"},
            "children": [_render_diff_line(line, is_added=False) for line in lines],
        }

    elif hunk.kind == "added":
        lines = hunk.proposed.split('\n')
        return {
            "type": "column",
            "props": {"gap": 0, "crossAxisAlignment": "stretch"},
            "children": [_render_diff_line(line, is_added=True) for line in lines],
        }

    elif hunk.kind == "modified":
        children: List[Dict[str, Any]] = []

        # Show removed lines first (red with -)
        if hunk.original:
            for line in hunk.original.split('\n'):
                children.append(_render_diff_line(line, is_added=False))

        # Then added lines (green with +)
        if hunk.proposed:
            for line in hunk.proposed.split('\n'):
                children.append(_render_diff_line(line, is_added=True))

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

    Shows the updated note content after applying changes.

    Args:
        note: The updated note after applying changes

    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    title = (note.payload.get("title") or "Untitled note").strip()
    content = (note.payload.get("content") or "").strip()
    tags = note.payload.get("tags") or []

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
        # Subtitle
        text_node(
            f"Updated to v{note.version}",
            TextStyle.BODY_SMALL,
            Color.ON_SURFACE_VARIANT,
        ),
    ]

    # Updated content card
    content_children: List[Dict[str, Any]] = [
        # Note title
        text_node(title, TextStyle.TITLE_LARGE, Color.ON_SURFACE),
    ]

    # Tags row if present
    if tags:
        tag_children = [
            {
                "type": "chip",
                "props": {"label": tag},
            }
            for tag in tags[:5]  # Limit to 5 tags
        ]
        content_children.append({
            "type": "row",
            "props": {"gap": Spacing.GAP_XS},
            "children": tag_children,
        })

    # Note content
    if content:
        content_children.append({
            "type": "divider",
            "props": {"margin": Spacing.GAP_SM},
        })
        content_children.append(
            text_node(content, TextStyle.BODY_MEDIUM, Color.ON_SURFACE),
        )

    children.append({
        "type": "card",
        "props": {"padding": 20},
        "children": [
            {
                "type": "column",
                "props": {"gap": Spacing.GAP_MD, "crossAxisAlignment": "stretch"},
                "children": content_children,
            }
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
