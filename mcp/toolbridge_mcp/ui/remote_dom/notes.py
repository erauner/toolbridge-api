"""Remote DOM templates for Notes UI.

Builds Remote DOM tree structures for native Flutter rendering via RemoteDomView.
Uses design tokens for consistent styling with native ToolBridge UI.
"""

from typing import Iterable, TYPE_CHECKING, Dict, Any, List

from toolbridge_mcp.ui.remote_dom.design import (
    TextStyle,
    Spacing,
    Layout,
    Color,
    Icon,
    ChipVariant,
    ButtonVariant,
    chip_node,
    wrap_node,
    text_node,
)

if TYPE_CHECKING:
    from toolbridge_mcp.tools.notes import Note


def render_notes_list_dom(
    notes: Iterable["Note"],
    limit: int = 20,
    include_deleted: bool = False,
    ui_format: str = "remote-dom",
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for the notes list view.

    Args:
        notes: Iterable of Note models to render
        limit: Current limit value (passed to action buttons)
        include_deleted: Current include_deleted value (passed to action buttons)
        ui_format: Current ui_format value (passed to action buttons)

    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    notes_list = list(notes)

    header_children: List[Dict[str, Any]] = [
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.NOTES, "size": 24, "color": Color.PRIMARY}},
                text_node("Notes", TextStyle.HEADLINE_MEDIUM),
            ],
        },
        text_node(
            f"Showing {len(notes_list)} note(s)",
            TextStyle.BODY_SMALL,
            Color.ON_SURFACE,
        ),
    ]

    cards: List[Dict[str, Any]] = []
    for note in notes_list:
        title = (note.payload.get("title") or "Untitled").strip()
        content_raw = (note.payload.get("content") or "").strip()
        preview = content_raw[:100] + ("..." if len(content_raw) > 100 else "")
        tags = note.payload.get("tags") or []
        updated_at = note.updated_at

        card_children: List[Dict[str, Any]] = [
            text_node(title, TextStyle.TITLE_LARGE),
            text_node(preview, TextStyle.BODY_MEDIUM, max_lines=3, overflow="ellipsis"),
        ]

        # Add metadata line (updated timestamp)
        if updated_at:
            updated_str = str(updated_at)[:10] if updated_at else ""
            card_children.append(
                text_node(f"Updated: {updated_str}", TextStyle.CAPTION, Color.ON_SURFACE_VARIANT)
            )

        # Add tags as chips in a wrap layout
        if tags:
            tag_chips = [chip_node(str(tag), ChipVariant.ASSIST) for tag in tags]
            card_children.append(wrap_node(tag_chips, Spacing.GAP_SM, Spacing.GAP_XS))

        # Action buttons
        card_children.append(
            {
                "type": "row",
                "props": {"gap": Spacing.GAP_SM},
                "children": [
                    {
                        "type": "button",
                        "props": {
                            "label": "View",
                            "variant": ButtonVariant.TEXT,
                            "icon": Icon.VISIBILITY,
                        },
                        "action": {
                            "type": "tool",
                            "payload": {
                                "toolName": "show_note_ui",
                                "params": {
                                    "uid": note.uid,
                                    "include_deleted": include_deleted,
                                    "ui_format": ui_format,
                                },
                            },
                        },
                    },
                    {
                        "type": "button",
                        "props": {
                            "label": "Delete",
                            "variant": ButtonVariant.TEXT,
                            "icon": Icon.DELETE,
                        },
                        "action": {
                            "type": "tool",
                            "payload": {
                                "toolName": "delete_note_ui",
                                "params": {
                                    "uid": note.uid,
                                    "limit": limit,
                                    "include_deleted": include_deleted,
                                    "ui_format": ui_format,
                                },
                            },
                        },
                    },
                ],
            }
        )

        cards.append(
            {
                "type": "card",
                "props": {
                    "padding": 20,  # More generous card padding
                },
                "children": [
                    {
                        "type": "column",
                        "props": {
                            "gap": Spacing.GAP_MD,
                            "crossAxisAlignment": "stretch",
                        },
                        "children": card_children,
                    }
                ],
            }
        )

    # Build root props for list view - full width, generous spacing
    root_props = {
        "gap": Spacing.SECTION_GAP,  # Larger gap between cards
        "padding": 24,  # More generous outer padding
        "fullWidth": True,
        "crossAxisAlignment": "stretch",
    }
    if Layout.MAX_WIDTH_LIST is not None:
        root_props["maxWidth"] = Layout.MAX_WIDTH_LIST

    return {
        "type": "column",
        "props": root_props,
        "children": header_children + cards,
    }


def render_note_detail_dom(
    note: "Note",
    ui_format: str = "remote-dom",
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for the note detail view.

    Args:
        note: Note model to render
        ui_format: Current ui_format value (for consistency)

    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    title = (note.payload.get("title") or "Untitled note").strip()
    content = (note.payload.get("content") or "No content").strip()
    tags = note.payload.get("tags") or []
    updated_at = note.updated_at

    children: List[Dict[str, Any]] = [
        # Header with icon and title
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.NOTE, "size": 24, "color": Color.PRIMARY}},
                text_node(title, TextStyle.HEADLINE_MEDIUM),
            ],
        },
        # Metadata line
        text_node(f"UID: {note.uid}  |  v{note.version}", TextStyle.CAPTION, Color.ON_SURFACE_VARIANT),
    ]

    # Add updated timestamp
    if updated_at:
        updated_str = str(updated_at)[:19] if updated_at else ""
        children.append(
            text_node(f"Updated: {updated_str}", TextStyle.CAPTION, Color.ON_SURFACE_VARIANT)
        )

    # Add tags as chips in a wrap layout
    if tags:
        tag_chips = [chip_node(str(tag), ChipVariant.ASSIST) for tag in tags]
        children.append(wrap_node(tag_chips, Spacing.GAP_SM, Spacing.GAP_XS))

    # Content card - expanded for editor-like feel
    children.append(
        {
            "type": "card",
            "props": {
                "padding": 24,  # More generous padding for content area
            },
            "children": [
                {
                    "type": "column",
                    "props": {
                        "gap": Spacing.GAP_MD,
                        "crossAxisAlignment": "stretch",
                    },
                    "children": [
                        # Content label
                        text_node("Content", TextStyle.LABEL_MEDIUM, Color.ON_SURFACE_VARIANT),
                        # Content text with room to breathe
                        {
                            "type": "container",
                            "props": {
                                "padding": {"top": 8, "bottom": 16},
                            },
                            "children": [
                                text_node(content, TextStyle.BODY_LARGE),
                            ],
                        },
                    ],
                },
            ],
        }
    )

    # Build root props for detail view - full width, generous padding
    root_props = {
        "gap": Spacing.SECTION_GAP,  # Larger gap for detail sections
        "padding": 24,  # More generous outer padding
        "fullWidth": True,  # Expand to fill available space
        "crossAxisAlignment": "stretch",  # Stretch children to full width
    }
    if Layout.MAX_WIDTH_DETAIL is not None:
        root_props["maxWidth"] = Layout.MAX_WIDTH_DETAIL

    return {
        "type": "column",
        "props": root_props,
        "children": children,
    }
