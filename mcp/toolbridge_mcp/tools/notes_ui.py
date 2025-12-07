"""
MCP-UI tools for Note display.

Provides UI-enhanced versions of note tools that return both text fallback
and interactive HTML for MCP-UI compatible hosts.
"""

from typing import Annotated, List, Union

from pydantic import Field
from loguru import logger
from mcp.types import TextContent, EmbeddedResource

from toolbridge_mcp.mcp_instance import mcp
from toolbridge_mcp.tools.notes import list_notes, get_note, delete_note, Note, NotesListResponse
from toolbridge_mcp.ui.resources import build_ui_with_text, UIContent
from toolbridge_mcp.ui.templates import notes as notes_templates


@mcp.tool()
async def list_notes_ui(
    limit: Annotated[int, Field(ge=1, le=100, description="Max notes to display")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted notes")] = False,
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Display notes with interactive UI (MCP-UI).

    This tool returns both a text summary (for non-UI hosts) and an interactive
    HTML view (for MCP-UI compatible hosts like Goose, Nanobot, or LibreChat).

    The UI view shows a styled list of notes with:
    - Note titles and content previews
    - Metadata (UID, version)
    - Visual styling for easy scanning

    Args:
        limit: Maximum number of notes to display (1-100, default 20)
        include_deleted: Whether to include soft-deleted notes (default False)

    Returns:
        List containing TextContent (summary) and UIResource (HTML view)

    Examples:
        # Show recent notes with UI
        >>> await list_notes_ui(limit=10)

        # Include deleted notes in UI
        >>> await list_notes_ui(include_deleted=True)
    """
    logger.info(f"Rendering notes UI: limit={limit}, include_deleted={include_deleted}")

    # Reuse existing data tool to fetch notes
    notes_response: NotesListResponse = await list_notes(
        limit=limit,
        cursor=None,
        include_deleted=include_deleted,
    )

    # Generate HTML using templates (pass context for action button tool calls)
    html = notes_templates.render_notes_list_html(
        notes_response.items,
        limit=limit,
        include_deleted=include_deleted,
    )

    # Human-readable summary (shown even if host ignores UIResource)
    count = len(notes_response.items)
    summary = f"Displaying {count} note(s) (limit={limit}, include_deleted={include_deleted})"

    if notes_response.next_cursor:
        summary += f"\nMore notes available (cursor: {notes_response.next_cursor[:20]}...)"

    ui_uri = "ui://toolbridge/notes/list"

    return build_ui_with_text(
        uri=ui_uri,
        html=html,
        text_summary=summary,
    )


@mcp.tool()
async def show_note_ui(
    uid: Annotated[str, Field(description="UID of the note to display")],
    include_deleted: Annotated[bool, Field(description="Allow deleted notes")] = False,
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Display a single note with interactive UI (MCP-UI).

    Shows a detailed view of a note including:
    - Full title and content
    - Tags and status badges
    - Version and timestamp metadata

    Args:
        uid: Unique identifier of the note (UUID format)
        include_deleted: Whether to allow viewing soft-deleted notes (default False)

    Returns:
        List containing TextContent (summary) and UIResource (HTML detail view)

    Examples:
        # Show a specific note
        >>> await show_note_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Show a deleted note
        >>> await show_note_ui("c1d9b7dc-...", include_deleted=True)
    """
    logger.info(f"Rendering note UI: uid={uid}, include_deleted={include_deleted}")

    # Fetch the note using existing data tool
    note: Note = await get_note(uid=uid, include_deleted=include_deleted)

    # Generate HTML using templates
    html = notes_templates.render_note_detail_html(note)

    # Human-readable summary
    title = note.payload.get("title", "Untitled note")
    content_preview = note.payload.get("content", "")[:100]
    if len(note.payload.get("content", "")) > 100:
        content_preview += "..."

    summary = f"Note: {title}\n\n{content_preview}\n\n(UID: {uid}, version: {note.version})"

    ui_uri = f"ui://toolbridge/notes/{uid}"

    return build_ui_with_text(
        uri=ui_uri,
        html=html,
        text_summary=summary,
    )


@mcp.tool()
async def delete_note_ui(
    uid: Annotated[str, Field(description="UID of the note to delete")],
    limit: Annotated[int, Field(ge=1, le=100, description="Max notes to display in refreshed list")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted notes in refreshed list")] = False,
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Delete a note and return updated UI (MCP-UI).

    Soft deletes the note and returns an updated notes list with interactive HTML.

    Args:
        uid: Unique identifier of the note to delete
        limit: Maximum notes to display in refreshed list (preserves list context)
        include_deleted: Whether to include deleted notes (preserves list context)

    Returns:
        List containing TextContent (summary) and UIResource (updated HTML list)

    Examples:
        >>> await delete_note_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Delete with custom list context
        >>> await delete_note_ui("c1d9b7dc-...", limit=50, include_deleted=True)
    """
    logger.info(f"Deleting note UI: uid={uid}, limit={limit}, include_deleted={include_deleted}")

    # Perform the delete using the underlying tool
    deleted_note: Note = await delete_note(uid=uid)
    note_title = deleted_note.payload.get("title", "Note")

    # Fetch updated notes list with preserved context and render UI
    notes_response: NotesListResponse = await list_notes(limit=limit, include_deleted=include_deleted)
    html = notes_templates.render_notes_list_html(
        notes_response.items,
        limit=limit,
        include_deleted=include_deleted,
    )

    summary = f"🗑️ Deleted '{note_title}' - {len(notes_response.items)} note(s) remaining"

    return build_ui_with_text(
        uri="ui://toolbridge/notes/list",
        html=html,
        text_summary=summary,
    )
