"""Remote DOM templates for Tasks UI.

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
    from toolbridge_mcp.tools.tasks import Task


def _get_status_chip(status: str) -> Dict[str, Any]:
    """Get a chip node for task status with appropriate styling."""
    status_label = status.replace("_", " ").title()
    
    # Use different chip variants based on status
    if status == "done":
        return chip_node(status_label, ChipVariant.FILLED, Icon.CHECK_CIRCLE)
    elif status == "in_progress":
        return chip_node(status_label, ChipVariant.OUTLINED, Icon.PENDING)
    else:
        return chip_node(status_label, ChipVariant.ASSIST)


def _get_priority_chip(priority: str) -> Dict[str, Any]:
    """Get a chip node for task priority."""
    return chip_node(f"Priority: {priority}", ChipVariant.OUTLINED, Icon.FLAG)


def render_tasks_list_dom(
    tasks: Iterable["Task"],
    limit: int = 20,
    include_deleted: bool = False,
    ui_format: str = "remote-dom",
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for the tasks list view.

    Args:
        tasks: Iterable of Task models to render
        limit: Current limit value (passed to action buttons)
        include_deleted: Current include_deleted value (passed to action buttons)
        ui_format: Current ui_format value (passed to action buttons)

    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    tasks_list = list(tasks)

    header: List[Dict[str, Any]] = [
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.TASK, "size": 24, "color": Color.PRIMARY}},
                text_node("Tasks", TextStyle.HEADLINE_MEDIUM),
            ],
        },
        text_node(
            f"Showing {len(tasks_list)} task(s)",
            TextStyle.BODY_SMALL,
            Color.ON_SURFACE,
        ),
    ]

    cards: List[Dict[str, Any]] = []
    for task in tasks_list:
        title = (task.payload.get("title") or "Untitled task").strip()
        desc_raw = (task.payload.get("description") or "").strip()
        description = desc_raw[:80] + ("..." if len(desc_raw) > 80 else "")
        status = task.payload.get("status") or "todo"
        priority = task.payload.get("priority") or ""
        due_date = task.payload.get("dueDate") or ""

        # Build card children
        card_children: List[Dict[str, Any]] = [
            text_node(title, TextStyle.TITLE_LARGE),
        ]

        # Add description if present
        if description:
            card_children.append(
                text_node(description, TextStyle.BODY_MEDIUM, max_lines=3, overflow="ellipsis")
            )

        # Build metadata chips
        meta_chips: List[Dict[str, Any]] = [_get_status_chip(status)]

        if priority:
            meta_chips.append(_get_priority_chip(priority))

        if due_date:
            meta_chips.append(chip_node(f"Due: {due_date[:10]}", ChipVariant.ASSIST, Icon.CALENDAR))

        # Add metadata chips in a wrap layout
        card_children.append(wrap_node(meta_chips, Spacing.GAP_SM, Spacing.GAP_XS))

        # Choose button label based on status
        if status == "done":
            primary_button = {
                "label": "Archive",
                "toolName": "archive_task_ui",
                "params": {
                    "uid": task.uid,
                    "limit": limit,
                    "include_deleted": include_deleted,
                    "ui_format": ui_format,
                },
                "icon": Icon.ARCHIVE,
            }
        else:
            primary_button = {
                "label": "Complete",
                "toolName": "process_task_ui",
                "params": {
                    "uid": task.uid,
                    "action": "complete",
                    "limit": limit,
                    "include_deleted": include_deleted,
                    "ui_format": ui_format,
                },
                "icon": Icon.TASK_ALT,
            }

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
                                "toolName": "show_task_ui",
                                "params": {
                                    "uid": task.uid,
                                    "include_deleted": include_deleted,
                                    "ui_format": ui_format,
                                },
                            },
                        },
                    },
                    {
                        "type": "button",
                        "props": {
                            "label": primary_button["label"],
                            "variant": ButtonVariant.TEXT,
                            "icon": primary_button["icon"],
                        },
                        "action": {
                            "type": "tool",
                            "payload": {
                                "toolName": primary_button["toolName"],
                                "params": primary_button["params"],
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
        "children": header + cards,
    }


def render_task_detail_dom(
    task: "Task",
    ui_format: str = "remote-dom",
) -> Dict[str, Any]:
    """
    Build Remote DOM tree for the task detail view.

    Args:
        task: Task model to render
        ui_format: Current ui_format value (for consistency)

    Returns:
        Root node dict compatible with RemoteDomNode.fromJson
    """
    title = (task.payload.get("title") or "Untitled task").strip()
    description = (task.payload.get("description") or "No description").strip()
    status = task.payload.get("status") or "todo"
    priority = task.payload.get("priority") or ""
    due_date = task.payload.get("dueDate") or ""
    tags = task.payload.get("tags") or []
    updated_at = task.updated_at

    children: List[Dict[str, Any]] = [
        # Header with icon and title
        {
            "type": "row",
            "props": {"gap": Spacing.GAP_SM, "crossAxisAlignment": "center"},
            "children": [
                {"type": "icon", "props": {"icon": Icon.TASK, "size": 24, "color": Color.PRIMARY}},
                text_node(title, TextStyle.HEADLINE_MEDIUM),
            ],
        },
    ]

    # Build status/priority/due date chips
    meta_chips: List[Dict[str, Any]] = [_get_status_chip(status)]

    if priority:
        meta_chips.append(_get_priority_chip(priority))

    if due_date:
        meta_chips.append(chip_node(f"Due: {due_date}", ChipVariant.ASSIST, Icon.CALENDAR))

    children.append(wrap_node(meta_chips, Spacing.GAP_SM, Spacing.GAP_XS))

    # Add tags as chips in a wrap layout
    if tags:
        tag_chips = [chip_node(str(tag), ChipVariant.OUTLINED, Icon.TAG) for tag in tags]
        children.append(wrap_node(tag_chips, Spacing.GAP_SM, Spacing.GAP_XS))

    # Description card - expanded for editor-like feel
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
                        # Description label
                        text_node("Description", TextStyle.LABEL_MEDIUM, Color.ON_SURFACE_VARIANT),
                        # Description text with room to breathe
                        {
                            "type": "container",
                            "props": {
                                "padding": {"top": 8, "bottom": 16},
                            },
                            "children": [
                                text_node(description, TextStyle.BODY_LARGE),
                            ],
                        },
                    ],
                },
            ],
        }
    )

    # Metadata footer
    meta_parts = [f"UID: {task.uid}", f"v{task.version}"]
    if updated_at:
        meta_parts.append(f"Updated: {str(updated_at)[:19]}")
    
    children.append(
        text_node("  |  ".join(meta_parts), TextStyle.CAPTION, Color.ON_SURFACE_VARIANT)
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
