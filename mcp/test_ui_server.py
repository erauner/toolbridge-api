#!/usr/bin/env python3
"""
Minimal test server for MCP-UI without OAuth authentication.

This server exposes only the UI tools for testing MCP-UI rendering
without requiring WorkOS AuthKit authentication.

Supports both HTML and Remote DOM formats via the ui_format parameter.

Run: python test_ui_server.py
Connect: http://localhost:8099/mcp
"""

from fastmcp import FastMCP
from loguru import logger
import sys
from typing import List, Union
from mcp.types import TextContent, EmbeddedResource

# Configure logging
logger.remove()
logger.add(sys.stderr, level="INFO", format="<level>{message}</level>", colorize=True)

# Create MCP server WITHOUT authentication
mcp = FastMCP(name="ToolBridge-UI-Test")

# Mock data for testing (mutable state for interactive testing)
# This simulates a database that persists across tool calls
mock_state = {
    "notes": [
        {
            "uid": "note-123",
            "version": 1,
            "updatedAt": "2025-01-01T00:00:00Z",
            "deletedAt": None,
            "payload": {
                "title": "Test Note from Mock API",
                "content": "This is test content from the mocked Go API",
                "tags": ["test", "mock"]
            }
        },
        {
            "uid": "note-456",
            "version": 2,
            "updatedAt": "2025-01-02T00:00:00Z",
            "deletedAt": None,
            "payload": {
                "title": "Second Test Note",
                "content": "Another test note with some content",
            }
        }
    ],
    "tasks": [
        {
            "uid": "task-789",
            "version": 1,
            "updatedAt": "2025-01-01T00:00:00Z",
            "deletedAt": None,
            "payload": {
                "title": "Test Task from Mock API",
                "description": "This is a test task description",
                "status": "in_progress",
                "priority": "high"
            }
        },
        {
            "uid": "task-101",
            "version": 1,
            "updatedAt": "2025-01-03T00:00:00Z",
            "deletedAt": None,
            "payload": {
                "title": "Completed Task Example",
                "description": "A task that's already done",
                "status": "done",
                "priority": "low"
            }
        }
    ]
}

# Import UI helper from the actual codebase
from toolbridge_mcp.ui.resources import build_ui_with_text_and_dom, UIFormat
from toolbridge_mcp.ui.templates import notes as notes_templates
from toolbridge_mcp.ui.templates import tasks as tasks_templates
from toolbridge_mcp.ui.remote_dom import notes as notes_dom_templates
from toolbridge_mcp.ui.remote_dom import tasks as tasks_dom_templates
from toolbridge_mcp.tools.notes import Note
from toolbridge_mcp.tools.tasks import Task


def get_mock_notes():
    """Convert mock note dicts to Pydantic models (only non-deleted)."""
    return [Note(**n) for n in mock_state["notes"] if n.get("deletedAt") is None]


def get_mock_tasks():
    """Convert mock task dicts to Pydantic models (only non-archived)."""
    return [Task(**t) for t in mock_state["tasks"] if t.get("deletedAt") is None]


def validate_ui_format(ui_format: str) -> UIFormat:
    """Validate and convert ui_format string to UIFormat enum.

    Returns UIFormat.HTML as default for invalid values to keep test server resilient.
    """
    try:
        return UIFormat(ui_format)
    except ValueError:
        logger.warning(f"Invalid ui_format '{ui_format}', defaulting to 'html'")
        return UIFormat.HTML


@mcp.tool()
async def list_notes_ui(
    limit: int = 20,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """List notes with rich HTML or Remote DOM rendering for MCP-UI hosts.

    Args:
        limit: Maximum number of notes to display
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns both text fallback and interactive UI.
    """
    notes = get_mock_notes()[:limit]
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = notes_templates.render_notes_list_html(notes)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = notes_dom_templates.render_notes_list_dom(
            notes,
            limit=limit,
            include_deleted=False,
            ui_format=fmt.value,
        )

    # Build informative text summary with actual note titles
    note_titles = [n.payload.get("title", "Untitled") for n in notes]
    titles_text = ", ".join(note_titles) if note_titles else "none"
    text_summary = f"Displaying {len(notes)} note(s): {titles_text}"

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/notes/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=fmt,
    )


@mcp.tool()
async def show_note_ui(
    uid: str,
    include_deleted: bool = False,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Show a single note with rich HTML or Remote DOM rendering for MCP-UI hosts.

    Args:
        uid: UID of the note to display
        include_deleted: Whether to include deleted notes
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns both text fallback and interactive UI.
    """
    # Find note by uid and convert to Pydantic model
    notes = get_mock_notes()
    note = next((n for n in notes if n.uid == uid), notes[0])
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = notes_templates.render_note_detail_html(note)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = notes_dom_templates.render_note_detail_dom(note, ui_format=fmt.value)

    title = note.payload.get("title", "Note")
    return build_ui_with_text_and_dom(
        uri=f"ui://toolbridge/notes/{note.uid}",
        html=html,
        remote_dom=remote_dom,
        text_summary=f"Note: {title}",
        ui_format=fmt,
    )


@mcp.tool()
async def list_tasks_ui(
    limit: int = 20,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """List tasks with rich HTML or Remote DOM rendering for MCP-UI hosts.

    Args:
        limit: Maximum number of tasks to display
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns both text fallback and interactive UI with status icons.
    """
    tasks = get_mock_tasks()[:limit]
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = tasks_templates.render_tasks_list_html(tasks)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = tasks_dom_templates.render_tasks_list_dom(
            tasks,
            limit=limit,
            include_deleted=False,
            ui_format=fmt.value,
        )

    # Build informative text summary with actual task titles
    task_titles = [t.payload.get("title", "Untitled") for t in tasks]
    titles_text = ", ".join(task_titles) if task_titles else "none"
    text_summary = f"Displaying {len(tasks)} task(s): {titles_text}"

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/tasks/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=fmt,
    )


@mcp.tool()
async def show_task_ui(
    uid: str,
    include_deleted: bool = False,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Show a single task with rich HTML or Remote DOM rendering for MCP-UI hosts.

    Args:
        uid: UID of the task to display
        include_deleted: Whether to include deleted tasks
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns both text fallback and interactive UI.
    """
    # Find task by uid and convert to Pydantic model
    tasks = get_mock_tasks()
    task = next((t for t in tasks if t.uid == uid), tasks[0])
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = tasks_templates.render_task_detail_html(task)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = tasks_dom_templates.render_task_detail_dom(task, ui_format=fmt.value)

    title = task.payload.get("title", "Task")
    return build_ui_with_text_and_dom(
        uri=f"ui://toolbridge/tasks/{task.uid}",
        html=html,
        remote_dom=remote_dom,
        text_summary=f"Task: {title}",
        ui_format=fmt,
    )


# ============================================================
# Action tools (stubs for testing button interactions)
# ============================================================

@mcp.tool()
async def delete_note_ui(
    uid: str,
    limit: int = 20,
    include_deleted: bool = False,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Delete a note and return updated UI list (MCP-UI).

    Args:
        uid: UID of the note to delete
        limit: Maximum notes to display in refreshed list
        include_deleted: Whether to include deleted notes
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Marks the note as deleted and returns the updated notes list with interactive UI.
    """
    from datetime import datetime

    # Find and mark note as deleted
    note_title = "Unknown"
    for note in mock_state["notes"]:
        if note["uid"] == uid:
            note["deletedAt"] = datetime.now().isoformat()
            note_title = note["payload"].get("title", "Note")
            break

    # Return updated notes list
    notes = get_mock_notes()[:limit]
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = notes_templates.render_notes_list_html(notes, limit=limit, include_deleted=include_deleted)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = notes_dom_templates.render_notes_list_dom(
            notes,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=fmt.value,
        )

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/notes/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=f"Deleted '{note_title}' - {len(notes)} note(s) remaining",
        ui_format=fmt,
    )


@mcp.tool()
async def process_task_ui(
    uid: str,
    action: str,
    limit: int = 20,
    include_deleted: bool = False,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Process a task action and return updated UI (MCP-UI).

    Args:
        uid: UID of the task to process
        action: Action to perform (start, complete, reopen)
        limit: Maximum tasks to display in refreshed list
        include_deleted: Whether to include deleted tasks
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Supported actions: start, complete, reopen.
    Returns the updated tasks list with interactive UI.
    """
    # Find and process the task
    task_title = "Unknown"
    for task in mock_state["tasks"]:
        if task["uid"] == uid:
            task_title = task["payload"].get("title", "Task")
            if action == "complete":
                task["payload"]["status"] = "done"
            elif action == "start":
                task["payload"]["status"] = "in_progress"
            elif action == "reopen":
                task["payload"]["status"] = "todo"
            break

    # Return updated tasks list
    tasks = get_mock_tasks()[:limit]
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = tasks_templates.render_tasks_list_html(tasks, limit=limit, include_deleted=include_deleted)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = tasks_dom_templates.render_tasks_list_dom(
            tasks,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=fmt.value,
        )

    action_text = {"complete": "Done", "start": "Started", "reopen": "Reopened"}.get(action, action.capitalize())
    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/tasks/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=f"{action_text} '{task_title}' - {len(tasks)} task(s) total",
        ui_format=fmt,
    )


@mcp.tool()
async def archive_task_ui(
    uid: str,
    limit: int = 20,
    include_deleted: bool = False,
    ui_format: str = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Archive a task and return updated UI (MCP-UI).

    Args:
        uid: UID of the task to archive
        limit: Maximum tasks to display in refreshed list
        include_deleted: Whether to include deleted tasks
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Marks the task as archived (deleted) and returns the updated tasks list with interactive UI.
    """
    from datetime import datetime

    # Find and mark task as archived
    task_title = "Unknown"
    for task in mock_state["tasks"]:
        if task["uid"] == uid:
            task["deletedAt"] = datetime.now().isoformat()
            task_title = task["payload"].get("title", "Task")
            break

    # Return updated tasks list
    tasks = get_mock_tasks()[:limit]
    fmt = validate_ui_format(ui_format)

    html = None
    remote_dom = None

    if fmt in (UIFormat.HTML, UIFormat.BOTH):
        html = tasks_templates.render_tasks_list_html(tasks, limit=limit, include_deleted=include_deleted)

    if fmt in (UIFormat.REMOTE_DOM, UIFormat.BOTH):
        remote_dom = tasks_dom_templates.render_tasks_list_dom(
            tasks,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=fmt.value,
        )

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/tasks/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=f"Archived '{task_title}' - {len(tasks)} task(s) remaining",
        ui_format=fmt,
    )


if __name__ == "__main__":
    import uvicorn
    from starlette.applications import Starlette
    from starlette.middleware import Middleware
    from starlette.middleware.cors import CORSMiddleware
    from starlette.routing import Mount

    logger.info("=" * 60)
    logger.info("  MCP-UI Test Server (No Authentication)")
    logger.info("=" * 60)
    logger.info("")
    logger.info("This server has 7 UI tools with mock data for testing MCP-UI.")
    logger.info("Supports both HTML and Remote DOM formats via ui_format parameter.")
    logger.info("")
    logger.info("Tools available:")
    logger.info("  - list_notes_ui: List notes with HTML/Remote DOM rendering")
    logger.info("  - show_note_ui: Show single note with HTML/Remote DOM rendering")
    logger.info("  - delete_note_ui: Delete note and return updated list")
    logger.info("  - list_tasks_ui: List tasks with HTML/Remote DOM rendering")
    logger.info("  - show_task_ui: Show single task with HTML/Remote DOM rendering")
    logger.info("  - process_task_ui: Process task action (start/complete/reopen)")
    logger.info("  - archive_task_ui: Archive task and return updated list")
    logger.info("")
    logger.info("UI format options: 'html' (default), 'remote-dom', 'both'")
    logger.info("")
    logger.info("Starting server on http://localhost:8099/mcp")
    logger.info("")
    logger.info("To test with MCP Inspector:")
    logger.info("  1. Open http://localhost:6274/")
    logger.info("  2. Select 'Streamable HTTP' transport")
    logger.info("  3. Enter URL: http://localhost:8099/mcp")
    logger.info("  4. Select 'Direct' connection type")
    logger.info("  5. Click Connect")
    logger.info("  6. Go to Tools tab and call list_notes_ui or list_tasks_ui")
    logger.info("  7. Try ui_format='remote-dom' to get native Flutter UI data")
    logger.info("")

    # Get the FastMCP app
    mcp_app = mcp.http_app()

    # Wrap with Starlette app that has CORS middleware
    # IMPORTANT: Must pass lifespan from mcp_app for proper initialization
    app = Starlette(
        routes=[Mount("/", app=mcp_app)],
        middleware=[
            Middleware(
                CORSMiddleware,
                allow_origins=["*"],
                allow_credentials=True,
                allow_methods=["*"],
                allow_headers=["*"],
                expose_headers=["mcp-session-id"],
            )
        ],
        lifespan=mcp_app.lifespan,
    )

    uvicorn.run(app, host="0.0.0.0", port=8099, log_level="info")
