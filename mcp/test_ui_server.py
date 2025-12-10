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
from toolbridge_mcp.ui.remote_dom import note_edits as note_edits_dom
from toolbridge_mcp.ui.remote_dom.design import Layout, get_chat_metadata
from toolbridge_mcp.tools.notes import Note
from toolbridge_mcp.tools.tasks import Task
from toolbridge_mcp.utils.diff import compute_line_diff, annotate_hunks_with_ids, DiffHunk
from toolbridge_mcp.note_edit_sessions import NoteEditHunkState
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional
import uuid

# Simple in-memory session storage for edit previews
@dataclass
class MockEditSession:
    """Lightweight edit session for test server."""
    id: str
    note_uid: str
    base_version: int
    title: str
    original_content: str
    proposed_content: str
    summary: str | None = None
    created_at: datetime = field(default_factory=datetime.utcnow)
    hunks: list[NoteEditHunkState] = field(default_factory=list)

    def get_hunk(self, hunk_id: str) -> Optional[NoteEditHunkState]:
        """Find a hunk by ID."""
        return next((h for h in self.hunks if h.id == hunk_id), None)

    def set_hunk_status(
        self,
        hunk_id: str,
        status: str,
        revised_text: Optional[str] = None,
    ) -> bool:
        """Update a hunk's status. Returns True if found."""
        hunk = self.get_hunk(hunk_id)
        if hunk is None:
            return False
        hunk.status = status
        hunk.revised_text = revised_text
        return True

mock_edit_sessions: dict[str, MockEditSession] = {}

MAX_UNCHANGED_LINES_DISPLAY = 5


def _truncate_unchanged_for_display(
    hunks: list[DiffHunk],
    max_lines: int = MAX_UNCHANGED_LINES_DISPLAY,
) -> list[DiffHunk]:
    """
    Truncate long unchanged sections for display purposes.

    Called AFTER annotate_hunks_with_ids so line ranges are accurate.
    """
    result = []
    for h in hunks:
        if h.kind == "unchanged" and h.original:
            lines = h.original.split("\n")
            if len(lines) > max_lines:
                half = max_lines // 2
                truncated = (
                    "\n".join(lines[:half])
                    + f"\n... ({len(lines) - max_lines} lines unchanged) ...\n"
                    + "\n".join(lines[-half:])
                )
                result.append(
                    DiffHunk(
                        kind=h.kind,
                        original=truncated,
                        proposed=truncated,
                        id=h.id,
                        orig_start=h.orig_start,
                        orig_end=h.orig_end,
                        new_start=h.new_start,
                        new_end=h.new_end,
                    )
                )
            else:
                result.append(h)
        else:
            result.append(h)
    return result


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


# ============================================================
# Note Edit tools (for Editor Chat testing)
# ============================================================

@mcp.tool()
async def edit_note_ui(
    uid: str,
    new_content: str,
    summary: str | None = None,
    ui_format: str = "remote-dom",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Propose changes to a note and display a diff preview (MCP-UI).

    Creates an edit session with the proposed changes and returns
    a visual diff preview with Accept/Discard buttons.

    Args:
        uid: UID of the note to edit
        new_content: The complete rewritten note content
        summary: Optional short description of what changed
        ui_format: UI format - only 'remote-dom' supported (HTML not implemented)

    Returns:
        List containing TextContent (summary) and Remote DOM diff preview
    """
    logger.info(f"Creating note edit session: uid={uid}")

    # Only remote-dom supported - HTML templates not implemented for diff views
    if ui_format not in ("remote-dom",):
        raise ValueError(f"ui_format must be 'remote-dom', got '{ui_format}'")

    # Find note by uid
    note_dict = next((n for n in mock_state["notes"] if n["uid"] == uid), None)
    if not note_dict:
        note_dict = mock_state["notes"][0]  # Fallback to first note

    note = Note(**note_dict)
    # Preserve whitespace - important for markdown/code formatting
    original_content = note.payload.get("content") or ""
    title = (note.payload.get("title") or "Untitled note").strip()

    # Compute diff hunks with full content, annotate line ranges, then truncate for display
    diff_hunks = compute_line_diff(original_content, new_content, truncate_unchanged=False)
    diff_hunks = annotate_hunks_with_ids(diff_hunks)
    diff_hunks = _truncate_unchanged_for_display(diff_hunks)

    # Build per-hunk state (unchanged = accepted, others = pending)
    hunk_states = [
        NoteEditHunkState(
            id=h.id or "",
            kind=h.kind,
            original=h.original,
            proposed=h.proposed,
            status="accepted" if h.kind == "unchanged" else "pending",
            orig_start=h.orig_start,
            orig_end=h.orig_end,
            new_start=h.new_start,
            new_end=h.new_end,
        )
        for h in diff_hunks
    ]

    # Create edit session with hunks
    session_id = uuid.uuid4().hex
    session = MockEditSession(
        id=session_id,
        note_uid=uid,
        base_version=note.version,
        title=title,
        original_content=original_content,
        proposed_content=new_content,
        summary=summary,
        hunks=hunk_states,
    )
    mock_edit_sessions[session_id] = session

    # Render Remote DOM diff preview with per-hunk actions
    remote_dom = note_edits_dom.render_note_edit_diff_dom(
        note=note,
        hunks=hunk_states,
        edit_id=session_id,
        summary=summary,
    )

    # Build fallback text summary
    text_summary = summary or f"Proposed changes to '{title}' (v{note.version})"

    ui_uri = f"ui://toolbridge/notes/{uid}/edit/{session_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    )

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=None,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat.REMOTE_DOM,
        remote_dom_ui_metadata=ui_metadata,
        remote_dom_metadata={
            "note_uid": uid,
            "edit_id": session_id,
        },
    )


@mcp.tool()
async def apply_note_edit(
    edit_id: str,
    ui_format: str = "remote-dom",
) -> List[Union[TextContent, EmbeddedResource]]:
    """Apply a pending note edit session.

    Called when the user clicks "Accept" on a diff preview.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call
        ui_format: UI format - only 'remote-dom' supported (HTML not implemented)

    Returns:
        List containing TextContent (summary) and Remote DOM confirmation
    """
    logger.info(f"Applying note edit: edit_id={edit_id}")

    # Only remote-dom supported - HTML templates not implemented for diff views
    if ui_format not in ("remote-dom",):
        raise ValueError(f"ui_format must be 'remote-dom', got '{ui_format}'")

    session = mock_edit_sessions.get(edit_id)
    if session is None:
        error_msg = f"Edit session '{edit_id}' not found or expired"
        logger.warning(error_msg)

        remote_dom = note_edits_dom.render_note_edit_error_dom(error_msg)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error_msg,
            ui_format=UIFormat.REMOTE_DOM,
        )

    # Apply the change to mock data
    for note in mock_state["notes"]:
        if note["uid"] == session.note_uid:
            note["payload"]["content"] = session.proposed_content
            note["version"] += 1
            note["updatedAt"] = datetime.now().isoformat()
            break

    # Build updated note model
    note_dict = next((n for n in mock_state["notes"] if n["uid"] == session.note_uid), None)
    if note_dict:
        updated_note = Note(**note_dict)
    else:
        updated_note = Note(uid=session.note_uid, version=1, updatedAt="", payload={"title": session.title, "content": session.proposed_content})

    # Remove session
    del mock_edit_sessions[edit_id]

    text_summary = f"Applied note edit to '{session.title}'. New version: v{updated_note.version}."
    remote_dom = note_edits_dom.render_note_edit_success_dom(updated_note)
    ui_uri = f"ui://toolbridge/notes/{updated_note.uid}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    )

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=None,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat.REMOTE_DOM,
        remote_dom_ui_metadata=ui_metadata,
    )


@mcp.tool()
async def discard_note_edit(
    edit_id: str,
) -> List[Union[TextContent, EmbeddedResource]]:
    """Discard a pending note edit session.

    Called when the user clicks "Discard" on a diff preview.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call

    Returns:
        List containing TextContent (summary) and Remote DOM confirmation
    """
    logger.info(f"Discarding note edit: edit_id={edit_id}")

    session = mock_edit_sessions.pop(edit_id, None)

    if session is None:
        title = "note"
        text_summary = f"Edit session '{edit_id}' was already discarded or expired."
    else:
        title = session.title
        text_summary = f"Discarded pending edit session for '{title}'."

    remote_dom = note_edits_dom.render_note_edit_discarded_dom(title)
    ui_uri = f"ui://toolbridge/notes/edit/{edit_id}/discarded"

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=None,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat.REMOTE_DOM,
    )


# ============================================================
# Per-hunk tools (for granular accept/reject/revise)
# ============================================================

def _get_session_and_note(edit_id: str) -> tuple[MockEditSession | None, Note | None, str | None]:
    """Helper to get session and note, or return error message."""
    session = mock_edit_sessions.get(edit_id)
    if session is None:
        return None, None, f"Edit session '{edit_id}' not found or expired"

    note_dict = next((n for n in mock_state["notes"] if n["uid"] == session.note_uid), None)
    if note_dict is None:
        return session, None, f"Note '{session.note_uid}' not found"

    return session, Note(**note_dict), None


def _render_updated_diff(session: MockEditSession, note: Note) -> dict:
    """Re-render the diff preview with current hunk states."""
    return note_edits_dom.render_note_edit_diff_dom(
        note=note,
        hunks=session.hunks,
        edit_id=session.id,
        summary=session.summary,
    )


@mcp.tool()
async def accept_note_edit_hunk(
    edit_id: str,
    hunk_id: str,
) -> List[Union[TextContent, EmbeddedResource]]:
    """Accept a specific hunk in a note edit session.

    Called when user clicks "Accept" on a single hunk.

    Args:
        edit_id: The edit session ID
        hunk_id: The hunk ID to accept (e.g., "h1", "h2")

    Returns:
        Updated diff preview UI
    """
    logger.info(f"Accepting hunk: edit_id={edit_id}, hunk_id={hunk_id}")

    session, note, error = _get_session_and_note(edit_id)
    if error:
        remote_dom = note_edits_dom.render_note_edit_error_dom(error)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error,
            ui_format=UIFormat.REMOTE_DOM,
        )

    if not session.set_hunk_status(hunk_id, "accepted"):
        error = f"Hunk '{hunk_id}' not found in session"
        remote_dom = note_edits_dom.render_note_edit_error_dom(error)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error,
            ui_format=UIFormat.REMOTE_DOM,
        )

    remote_dom = _render_updated_diff(session, note)
    ui_uri = f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    )

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=None,
        remote_dom=remote_dom,
        text_summary=f"Accepted hunk {hunk_id}",
        ui_format=UIFormat.REMOTE_DOM,
        remote_dom_ui_metadata=ui_metadata,
    )


@mcp.tool()
async def reject_note_edit_hunk(
    edit_id: str,
    hunk_id: str,
) -> List[Union[TextContent, EmbeddedResource]]:
    """Reject a specific hunk in a note edit session.

    Called when user clicks "Reject" on a single hunk.

    Args:
        edit_id: The edit session ID
        hunk_id: The hunk ID to reject (e.g., "h1", "h2")

    Returns:
        Updated diff preview UI
    """
    logger.info(f"Rejecting hunk: edit_id={edit_id}, hunk_id={hunk_id}")

    session, note, error = _get_session_and_note(edit_id)
    if error:
        remote_dom = note_edits_dom.render_note_edit_error_dom(error)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error,
            ui_format=UIFormat.REMOTE_DOM,
        )

    if not session.set_hunk_status(hunk_id, "rejected"):
        error = f"Hunk '{hunk_id}' not found in session"
        remote_dom = note_edits_dom.render_note_edit_error_dom(error)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error,
            ui_format=UIFormat.REMOTE_DOM,
        )

    remote_dom = _render_updated_diff(session, note)
    ui_uri = f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    )

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=None,
        remote_dom=remote_dom,
        text_summary=f"Rejected hunk {hunk_id}",
        ui_format=UIFormat.REMOTE_DOM,
        remote_dom_ui_metadata=ui_metadata,
    )


@mcp.tool()
async def revise_note_edit_hunk(
    edit_id: str,
    hunk_id: str,
    revised_text: str,
) -> List[Union[TextContent, EmbeddedResource]]:
    """Revise a specific hunk with custom text.

    Called when user provides custom text for a hunk.

    Args:
        edit_id: The edit session ID
        hunk_id: The hunk ID to revise (e.g., "h1", "h2")
        revised_text: The custom text to use instead

    Returns:
        Updated diff preview UI
    """
    logger.info(f"Revising hunk: edit_id={edit_id}, hunk_id={hunk_id}")

    session, note, error = _get_session_and_note(edit_id)
    if error:
        remote_dom = note_edits_dom.render_note_edit_error_dom(error)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error,
            ui_format=UIFormat.REMOTE_DOM,
        )

    if not session.set_hunk_status(hunk_id, "revised", revised_text=revised_text):
        error = f"Hunk '{hunk_id}' not found in session"
        remote_dom = note_edits_dom.render_note_edit_error_dom(error)
        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=None,
            remote_dom=remote_dom,
            text_summary=error,
            ui_format=UIFormat.REMOTE_DOM,
        )

    remote_dom = _render_updated_diff(session, note)
    ui_uri = f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    )

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=None,
        remote_dom=remote_dom,
        text_summary=f"Revised hunk {hunk_id}",
        ui_format=UIFormat.REMOTE_DOM,
        remote_dom_ui_metadata=ui_metadata,
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
    logger.info("This server has 13 UI tools with mock data for testing MCP-UI.")
    logger.info("Supports both HTML and Remote DOM formats via ui_format parameter.")
    logger.info("")
    logger.info("Tools available:")
    logger.info("  - list_notes_ui: List notes with HTML/Remote DOM rendering")
    logger.info("  - show_note_ui: Show single note with HTML/Remote DOM rendering")
    logger.info("  - delete_note_ui: Delete note and return updated list")
    logger.info("  - edit_note_ui: Propose note edits with diff preview")
    logger.info("  - apply_note_edit: Apply a pending note edit")
    logger.info("  - discard_note_edit: Discard a pending note edit")
    logger.info("  - accept_note_edit_hunk: Accept a single hunk in diff")
    logger.info("  - reject_note_edit_hunk: Reject a single hunk in diff")
    logger.info("  - revise_note_edit_hunk: Revise a single hunk with custom text")
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
