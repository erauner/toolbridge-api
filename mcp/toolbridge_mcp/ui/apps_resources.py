"""
ChatGPT Apps SDK resource registration.

Registers MCP resources for Apps SDK widget templates with:
- MIME type: text/html+skybridge (required for Apps SDK)
- Widget metadata: descriptions, CSP hints, border preferences

These resources are referenced by tool descriptors via openai/outputTemplate
and fetched by ChatGPT to render interactive widgets.
"""

from loguru import logger

from toolbridge_mcp.mcp_instance import mcp
from toolbridge_mcp.ui.apps_templates import (
    notes_list_template_html,
    note_detail_template_html,
    note_edit_template_html,
    tasks_list_template_html,
    task_detail_template_html,
)


# Apps SDK MIME type - required for ChatGPT to inject the widget runtime
APPS_SDK_MIME_TYPE = "text/html+skybridge"

# Resource URI namespace for Apps SDK templates (separate from embedded MCP-UI resources)
APPS_RESOURCE_PREFIX = "ui://toolbridge/apps"


# ============================================================================
# Notes Resources
# ============================================================================

@mcp.resource(
    f"{APPS_RESOURCE_PREFIX}/notes/list",
    mime_type=APPS_SDK_MIME_TYPE,
    description="Interactive notes list widget for ChatGPT Apps",
)
async def apps_notes_list_resource() -> str:
    """
    Apps SDK template resource for notes list.

    Returns HTML widget that renders notes from window.openai.toolOutput
    and allows users to view, delete, and manage notes via callTool.
    """
    logger.debug("Serving Apps SDK notes list template")
    return notes_list_template_html()


@mcp.resource(
    f"{APPS_RESOURCE_PREFIX}/notes/detail",
    mime_type=APPS_SDK_MIME_TYPE,
    description="Interactive note detail widget for ChatGPT Apps",
)
async def apps_note_detail_resource() -> str:
    """
    Apps SDK template resource for note detail view.

    Returns HTML widget that renders a single note from window.openai.toolOutput
    with actions to delete or navigate back to the list.
    """
    logger.debug("Serving Apps SDK note detail template")
    return note_detail_template_html()


@mcp.resource(
    f"{APPS_RESOURCE_PREFIX}/notes/edit",
    mime_type=APPS_SDK_MIME_TYPE,
    description="Interactive note edit diff widget for ChatGPT Apps",
)
async def apps_note_edit_resource() -> str:
    """
    Apps SDK template resource for note edit diff view.

    Returns HTML widget that renders diff hunks from window.openai.toolOutput
    with actions to accept, reject, or revise individual changes.
    """
    logger.debug("Serving Apps SDK note edit template")
    return note_edit_template_html()


# ============================================================================
# Tasks Resources
# ============================================================================

@mcp.resource(
    f"{APPS_RESOURCE_PREFIX}/tasks/list",
    mime_type=APPS_SDK_MIME_TYPE,
    description="Interactive tasks list widget for ChatGPT Apps",
)
async def apps_tasks_list_resource() -> str:
    """
    Apps SDK template resource for tasks list.

    Returns HTML widget that renders tasks from window.openai.toolOutput
    and allows users to view, complete, and archive tasks via callTool.
    """
    logger.debug("Serving Apps SDK tasks list template")
    return tasks_list_template_html()


@mcp.resource(
    f"{APPS_RESOURCE_PREFIX}/tasks/detail",
    mime_type=APPS_SDK_MIME_TYPE,
    description="Interactive task detail widget for ChatGPT Apps",
)
async def apps_task_detail_resource() -> str:
    """
    Apps SDK template resource for task detail view.

    Returns HTML widget that renders a single task from window.openai.toolOutput
    with actions to navigate back to the list.
    """
    logger.debug("Serving Apps SDK task detail template")
    return task_detail_template_html()


# ============================================================================
# Resource URI Constants (for use in tool _meta)
# ============================================================================

# Notes template URIs
APPS_NOTES_LIST_URI = f"{APPS_RESOURCE_PREFIX}/notes/list"
APPS_NOTE_DETAIL_URI = f"{APPS_RESOURCE_PREFIX}/notes/detail"
APPS_NOTE_EDIT_URI = f"{APPS_RESOURCE_PREFIX}/notes/edit"

# Tasks template URIs
APPS_TASKS_LIST_URI = f"{APPS_RESOURCE_PREFIX}/tasks/list"
APPS_TASK_DETAIL_URI = f"{APPS_RESOURCE_PREFIX}/tasks/detail"


logger.info(f"✓ Apps SDK resources registered: {APPS_RESOURCE_PREFIX}/notes/*, {APPS_RESOURCE_PREFIX}/tasks/*")
