"""
MCP-UI tools for Task display.

Provides UI-enhanced versions of task tools that return both text fallback
and interactive HTML/Remote DOM for MCP-UI compatible hosts.

Also supports ChatGPT Apps SDK integration via:
- Tool _meta: openai/outputTemplate, openai/widgetAccessible, etc.
- structuredContent: Data payload for Apps SDK widgets
"""

from typing import Annotated, List, Union

from pydantic import Field
from loguru import logger
from mcp.types import TextContent, EmbeddedResource

from toolbridge_mcp.mcp_instance import mcp
from toolbridge_mcp.tools.tasks import list_tasks, get_task, process_task, archive_task, Task, TasksListResponse
from toolbridge_mcp.ui.resources import build_ui_with_text_and_dom, UIContent, UIFormat
from toolbridge_mcp.ui.templates import tasks as tasks_templates
from toolbridge_mcp.ui.remote_dom import tasks as tasks_dom_templates

# Apps SDK resource URIs for tool meta
from toolbridge_mcp.ui.apps_resources import (
    APPS_TASKS_LIST_URI,
    APPS_TASK_DETAIL_URI,
)


@mcp.tool(
    meta={
        "openai/outputTemplate": APPS_TASKS_LIST_URI,
        "openai/toolInvocation/invoking": "Loading your tasks...",
        "openai/toolInvocation/invoked": "Tasks ready",
        "openai/widgetAccessible": True,
    }
)
async def list_tasks_ui(
    limit: Annotated[int, Field(ge=1, le=100, description="Max tasks to display")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted tasks")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Display tasks with interactive UI (MCP-UI).

    This tool returns both a text summary (for non-UI hosts) and an interactive
    HTML or Remote DOM view (for MCP-UI compatible hosts like Goose, Nanobot, or LibreChat).

    The UI view shows a styled list of tasks with:
    - Status icons (todo, in_progress, done)
    - Priority badges (high, medium, low)
    - Due dates and descriptions
    - Visual styling for easy scanning

    Args:
        limit: Maximum number of tasks to display (1-100, default 20)
        include_deleted: Whether to include soft-deleted tasks (default False)
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (HTML and/or Remote DOM)

    Examples:
        # Show recent tasks with HTML UI (default)
        >>> await list_tasks_ui(limit=10)

        # Include deleted tasks with Remote DOM UI
        >>> await list_tasks_ui(include_deleted=True, ui_format="remote-dom")

        # Return both HTML and Remote DOM
        >>> await list_tasks_ui(ui_format="both")
    """
    logger.info(f"Rendering tasks UI: limit={limit}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Reuse existing data tool to fetch tasks
    tasks_response: TasksListResponse = await list_tasks(
        limit=limit,
        cursor=None,
        include_deleted=include_deleted,
    )

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = tasks_templates.render_tasks_list_html(
            tasks_response.items,
            limit=limit,
            include_deleted=include_deleted,
        )

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = tasks_dom_templates.render_tasks_list_dom(
            tasks_response.items,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=ui_format,
        )

    # Human-readable summary (shown even if host ignores UIResource)
    count = len(tasks_response.items)
    summary = f"Displaying {count} task(s) (limit={limit}, include_deleted={include_deleted})"

    if tasks_response.next_cursor:
        summary += f"\nMore tasks available (cursor: {tasks_response.next_cursor[:20]}...)"

    ui_uri = "ui://toolbridge/tasks/list"

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool(
    meta={
        "openai/outputTemplate": APPS_TASK_DETAIL_URI,
        "openai/toolInvocation/invoking": "Loading task...",
        "openai/toolInvocation/invoked": "Task ready",
        "openai/widgetAccessible": True,
    }
)
async def show_task_ui(
    uid: Annotated[str, Field(description="UID of the task to display")],
    include_deleted: Annotated[bool, Field(description="Allow deleted tasks")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Display a single task with interactive UI (MCP-UI).

    Shows a detailed view of a task including:
    - Status icon and title
    - Priority badge
    - Full description
    - Due date and tags
    - Version and timestamp metadata

    Args:
        uid: Unique identifier of the task (UUID format)
        include_deleted: Whether to allow viewing soft-deleted tasks (default False)
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (HTML and/or Remote DOM detail view)

    Examples:
        # Show a specific task with HTML UI
        >>> await show_task_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Show a deleted task with Remote DOM UI
        >>> await show_task_ui("c1d9b7dc-...", include_deleted=True, ui_format="remote-dom")
    """
    logger.info(f"Rendering task UI: uid={uid}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Fetch the task using existing data tool
    task: Task = await get_task(uid=uid, include_deleted=include_deleted)

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = tasks_templates.render_task_detail_html(task)

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = tasks_dom_templates.render_task_detail_dom(task, ui_format=ui_format)

    # Human-readable summary (guard against null values)
    title = task.payload.get("title") or "Untitled task"
    status = task.payload.get("status") or "unknown"
    priority = task.payload.get("priority") or ""
    description_raw = task.payload.get("description") or ""
    description = description_raw[:100]
    if len(description_raw) > 100:
        description += "..."

    summary = f"Task: {title}\nStatus: {status}"
    if priority:
        summary += f" | Priority: {priority}"
    if description:
        summary += f"\n\n{description}"
    summary += f"\n\n(UID: {uid}, version: {task.version})"

    ui_uri = f"ui://toolbridge/tasks/{uid}"

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool(
    meta={
        "openai/outputTemplate": APPS_TASKS_LIST_URI,
        "openai/toolInvocation/invoking": "Processing task...",
        "openai/toolInvocation/invoked": "Task updated",
        "openai/widgetAccessible": True,
    }
)
async def process_task_ui(
    uid: Annotated[str, Field(description="UID of the task to process")],
    action: Annotated[str, Field(description="Action to perform (start, complete, reopen)")],
    limit: Annotated[int, Field(ge=1, le=100, description="Max tasks to display in refreshed list")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted tasks in refreshed list")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Process a task action and return updated UI (MCP-UI).

    Performs the action and returns an updated task list with interactive HTML or Remote DOM.
    Supported actions: start, complete, reopen.

    Args:
        uid: Unique identifier of the task
        action: Action to perform (start, complete, reopen)
        limit: Maximum tasks to display in refreshed list (preserves list context)
        include_deleted: Whether to include deleted tasks (preserves list context)
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (updated HTML and/or Remote DOM list)

    Examples:
        # Complete a task with HTML UI
        >>> await process_task_ui("c1d9b7dc-...", "complete")

        # Start a task with custom list context and Remote DOM
        >>> await process_task_ui("c1d9b7dc-...", "start", limit=50, ui_format="remote-dom")
    """
    logger.info(f"Processing task UI: uid={uid}, action={action}, limit={limit}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Perform the action using the underlying tool
    updated_task: Task = await process_task(uid=uid, action=action)
    task_title = updated_task.payload.get("title", "Task")

    # Fetch updated task list with preserved context
    tasks_response: TasksListResponse = await list_tasks(limit=limit, include_deleted=include_deleted)

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = tasks_templates.render_tasks_list_html(
            tasks_response.items,
            limit=limit,
            include_deleted=include_deleted,
        )

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = tasks_dom_templates.render_tasks_list_dom(
            tasks_response.items,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=ui_format,
        )

    action_emoji = {"complete": "Done", "start": "Started", "reopen": "Reopened"}.get(action, action.capitalize())
    summary = f"{action_emoji} '{task_title}' - {len(tasks_response.items)} task(s) total"

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/tasks/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool(
    meta={
        "openai/outputTemplate": APPS_TASKS_LIST_URI,
        "openai/toolInvocation/invoking": "Archiving task...",
        "openai/toolInvocation/invoked": "Task archived",
        "openai/widgetAccessible": True,
    }
)
async def archive_task_ui(
    uid: Annotated[str, Field(description="UID of the task to archive")],
    limit: Annotated[int, Field(ge=1, le=100, description="Max tasks to display in refreshed list")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted tasks in refreshed list")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Archive a task and return updated UI (MCP-UI).

    Archives the task and returns an updated task list with interactive HTML or Remote DOM.

    Args:
        uid: Unique identifier of the task to archive
        limit: Maximum tasks to display in refreshed list (preserves list context)
        include_deleted: Whether to include deleted tasks (preserves list context)
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (updated HTML and/or Remote DOM list)

    Examples:
        >>> await archive_task_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Archive with custom list context and Remote DOM
        >>> await archive_task_ui("c1d9b7dc-...", limit=50, include_deleted=True, ui_format="remote-dom")
    """
    logger.info(f"Archiving task UI: uid={uid}, limit={limit}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Perform the archive using the underlying tool
    archived_task: Task = await archive_task(uid=uid)
    task_title = archived_task.payload.get("title", "Task")

    # Fetch updated task list with preserved context
    tasks_response: TasksListResponse = await list_tasks(limit=limit, include_deleted=include_deleted)

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = tasks_templates.render_tasks_list_html(
            tasks_response.items,
            limit=limit,
            include_deleted=include_deleted,
        )

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = tasks_dom_templates.render_tasks_list_dom(
            tasks_response.items,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=ui_format,
        )

    summary = f"Archived '{task_title}' - {len(tasks_response.items)} task(s) remaining"

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/tasks/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )
