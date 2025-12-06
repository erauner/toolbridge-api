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
from toolbridge_mcp.tools.tasks import list_tasks, get_task, Task, TasksListResponse
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

    # Generate HTML using templates
    html = tasks_templates.render_tasks_list_html(tasks_response.items)

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
