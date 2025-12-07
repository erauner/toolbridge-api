"""
MCP-UI tools for Task display.

Provides UI-enhanced versions of task tools that return both text fallback
and interactive HTML for MCP-UI compatible hosts.
"""

from typing import Annotated, List, Union

from pydantic import Field
from loguru import logger
from mcp.types import TextContent, EmbeddedResource

from toolbridge_mcp.mcp_instance import mcp
from toolbridge_mcp.tools.tasks import list_tasks, get_task, process_task, archive_task, Task, TasksListResponse
from toolbridge_mcp.ui.resources import build_ui_with_text, UIContent
from toolbridge_mcp.ui.templates import tasks as tasks_templates


@mcp.tool()
async def list_tasks_ui(
    limit: Annotated[int, Field(ge=1, le=100, description="Max tasks to display")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted tasks")] = False,
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Display tasks with interactive UI (MCP-UI).

    This tool returns both a text summary (for non-UI hosts) and an interactive
    HTML view (for MCP-UI compatible hosts like Goose, Nanobot, or LibreChat).

    The UI view shows a styled list of tasks with:
    - Status icons (todo, in_progress, done)
    - Priority badges (high, medium, low)
    - Due dates and descriptions
    - Visual styling for easy scanning

    Args:
        limit: Maximum number of tasks to display (1-100, default 20)
        include_deleted: Whether to include soft-deleted tasks (default False)

    Returns:
        List containing TextContent (summary) and UIResource (HTML view)

    Examples:
        # Show recent tasks with UI
        >>> await list_tasks_ui(limit=10)

        # Include deleted tasks in UI
        >>> await list_tasks_ui(include_deleted=True)
    """
    logger.info(f"Rendering tasks UI: limit={limit}, include_deleted={include_deleted}")

    # Reuse existing data tool to fetch tasks
    tasks_response: TasksListResponse = await list_tasks(
        limit=limit,
        cursor=None,
        include_deleted=include_deleted,
    )

    # Generate HTML using templates (pass context for action button tool calls)
    html = tasks_templates.render_tasks_list_html(
        tasks_response.items,
        limit=limit,
        include_deleted=include_deleted,
    )

    # Human-readable summary (shown even if host ignores UIResource)
    count = len(tasks_response.items)
    summary = f"Displaying {count} task(s) (limit={limit}, include_deleted={include_deleted})"

    if tasks_response.next_cursor:
        summary += f"\nMore tasks available (cursor: {tasks_response.next_cursor[:20]}...)"

    ui_uri = "ui://toolbridge/tasks/list"

    return build_ui_with_text(
        uri=ui_uri,
        html=html,
        text_summary=summary,
    )


@mcp.tool()
async def show_task_ui(
    uid: Annotated[str, Field(description="UID of the task to display")],
    include_deleted: Annotated[bool, Field(description="Allow deleted tasks")] = False,
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

    Returns:
        List containing TextContent (summary) and UIResource (HTML detail view)

    Examples:
        # Show a specific task
        >>> await show_task_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Show a deleted task
        >>> await show_task_ui("c1d9b7dc-...", include_deleted=True)
    """
    logger.info(f"Rendering task UI: uid={uid}, include_deleted={include_deleted}")

    # Fetch the task using existing data tool
    task: Task = await get_task(uid=uid, include_deleted=include_deleted)

    # Generate HTML using templates
    html = tasks_templates.render_task_detail_html(task)

    # Human-readable summary
    title = task.payload.get("title", "Untitled task")
    status = task.payload.get("status", "unknown")
    priority = task.payload.get("priority", "")
    description = task.payload.get("description", "")[:100]
    if len(task.payload.get("description", "")) > 100:
        description += "..."

    summary = f"Task: {title}\nStatus: {status}"
    if priority:
        summary += f" | Priority: {priority}"
    if description:
        summary += f"\n\n{description}"
    summary += f"\n\n(UID: {uid}, version: {task.version})"

    ui_uri = f"ui://toolbridge/tasks/{uid}"

    return build_ui_with_text(
        uri=ui_uri,
        html=html,
        text_summary=summary,
    )


@mcp.tool()
async def process_task_ui(
    uid: Annotated[str, Field(description="UID of the task to process")],
    action: Annotated[str, Field(description="Action to perform (start, complete, reopen)")],
    limit: Annotated[int, Field(ge=1, le=100, description="Max tasks to display in refreshed list")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted tasks in refreshed list")] = False,
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Process a task action and return updated UI (MCP-UI).

    Performs the action and returns an updated task list with interactive HTML.
    Supported actions: start, complete, reopen.

    Args:
        uid: Unique identifier of the task
        action: Action to perform (start, complete, reopen)
        limit: Maximum tasks to display in refreshed list (preserves list context)
        include_deleted: Whether to include deleted tasks (preserves list context)

    Returns:
        List containing TextContent (summary) and UIResource (updated HTML list)

    Examples:
        # Complete a task
        >>> await process_task_ui("c1d9b7dc-...", "complete")

        # Start a task with custom list context
        >>> await process_task_ui("c1d9b7dc-...", "start", limit=50)
    """
    logger.info(f"Processing task UI: uid={uid}, action={action}, limit={limit}, include_deleted={include_deleted}")

    # Perform the action using the underlying tool
    updated_task: Task = await process_task(uid=uid, action=action)
    task_title = updated_task.payload.get("title", "Task")

    # Fetch updated task list with preserved context and render UI
    tasks_response: TasksListResponse = await list_tasks(limit=limit, include_deleted=include_deleted)
    html = tasks_templates.render_tasks_list_html(
        tasks_response.items,
        limit=limit,
        include_deleted=include_deleted,
    )

    action_emoji = {"complete": "✅", "start": "🔄", "reopen": "↩️"}.get(action, "✓")
    summary = f"{action_emoji} {action.capitalize()}d '{task_title}' - {len(tasks_response.items)} task(s) total"

    return build_ui_with_text(
        uri="ui://toolbridge/tasks/list",
        html=html,
        text_summary=summary,
    )


@mcp.tool()
async def archive_task_ui(
    uid: Annotated[str, Field(description="UID of the task to archive")],
    limit: Annotated[int, Field(ge=1, le=100, description="Max tasks to display in refreshed list")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted tasks in refreshed list")] = False,
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Archive a task and return updated UI (MCP-UI).

    Archives the task and returns an updated task list with interactive HTML.

    Args:
        uid: Unique identifier of the task to archive
        limit: Maximum tasks to display in refreshed list (preserves list context)
        include_deleted: Whether to include deleted tasks (preserves list context)

    Returns:
        List containing TextContent (summary) and UIResource (updated HTML list)

    Examples:
        >>> await archive_task_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Archive with custom list context
        >>> await archive_task_ui("c1d9b7dc-...", limit=50, include_deleted=True)
    """
    logger.info(f"Archiving task UI: uid={uid}, limit={limit}, include_deleted={include_deleted}")

    # Perform the archive using the underlying tool
    archived_task: Task = await archive_task(uid=uid)
    task_title = archived_task.payload.get("title", "Task")

    # Fetch updated task list with preserved context and render UI
    tasks_response: TasksListResponse = await list_tasks(limit=limit, include_deleted=include_deleted)
    html = tasks_templates.render_tasks_list_html(
        tasks_response.items,
        limit=limit,
        include_deleted=include_deleted,
    )

    summary = f"📦 Archived '{task_title}' - {len(tasks_response.items)} task(s) remaining"

    return build_ui_with_text(
        uri="ui://toolbridge/tasks/list",
        html=html,
        text_summary=summary,
    )
