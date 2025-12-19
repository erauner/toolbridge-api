"""
MCP-UI tools for Note display.

Provides UI-enhanced versions of note tools that return both text fallback
and interactive HTML/Remote DOM for MCP-UI compatible hosts.
"""

from typing import Annotated, List, Union

from pydantic import Field
from loguru import logger
from mcp.types import TextContent, EmbeddedResource

from toolbridge_mcp.mcp_instance import mcp
from toolbridge_mcp.tools.notes import (
    list_notes as list_notes_tool,
    get_note as get_note_tool,
    delete_note as delete_note_tool,
    update_note as update_note_tool,
    Note,
    NotesListResponse,
)

# Access the underlying async functions from FunctionTool wrappers.
# The @mcp.tool() decorator wraps functions in FunctionTool objects,
# so we need to use .fn to call the original function directly.
_list_notes = list_notes_tool.fn
_get_note = get_note_tool.fn
_delete_note = delete_note_tool.fn
_update_note = update_note_tool.fn
from toolbridge_mcp.ui.resources import build_ui_with_text_and_dom, UIContent, UIFormat
from toolbridge_mcp.ui.templates import notes as notes_templates
from toolbridge_mcp.ui.templates import note_edits as note_edits_templates
from toolbridge_mcp.ui.remote_dom import notes as notes_dom_templates
from toolbridge_mcp.ui.remote_dom import note_edits as note_edits_dom
from toolbridge_mcp.ui.remote_dom.design import Layout, get_chat_metadata
from toolbridge_mcp.note_edit_sessions import (
    create_session,
    get_session,
    discard_session,
    set_hunk_status,
    get_hunk_counts,
    NoteEditHunkState,
)
from toolbridge_mcp.utils.diff import compute_line_diff, annotate_hunks_with_ids, DiffHunk, HunkDecision, apply_hunk_decisions
from fastmcp.server.dependencies import get_access_token
import httpx


# Default max lines to show in unchanged sections for display
MAX_UNCHANGED_LINES_DISPLAY = 5


def _truncate_unchanged_for_display(
    hunks: List[DiffHunk],
    max_lines: int = MAX_UNCHANGED_LINES_DISPLAY,
) -> List[DiffHunk]:
    """
    Truncate long unchanged sections for display purposes.

    Called AFTER annotate_hunks_with_ids so line ranges are accurate.
    Only modifies the original/proposed text for display, not the line ranges.

    Args:
        hunks: Annotated hunks with correct line ranges
        max_lines: Maximum lines to show in unchanged sections

    Returns:
        List of hunks with truncated unchanged content for display
    """
    result = []
    for h in hunks:
        if h.kind == "unchanged" and h.original:
            lines = h.original.split("\n")
            if len(lines) > max_lines:
                half = max_lines // 2
                truncated = (
                    "\n".join(lines[:half]) +
                    f"\n... ({len(lines) - max_lines} lines unchanged) ...\n" +
                    "\n".join(lines[-half:])
                )
                result.append(DiffHunk(
                    kind=h.kind,
                    original=truncated,
                    proposed=truncated,
                    id=h.id,
                    orig_start=h.orig_start,
                    orig_end=h.orig_end,
                    new_start=h.new_start,
                    new_end=h.new_end,
                ))
            else:
                result.append(h)
        else:
            result.append(h)
    return result


@mcp.tool()
async def list_notes_ui(
    limit: Annotated[int, Field(ge=1, le=100, description="Max notes to display")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted notes")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Display notes with interactive UI (MCP-UI).

    This tool returns both a text summary (for non-UI hosts) and an interactive
    HTML or Remote DOM view (for MCP-UI compatible hosts like Goose, Nanobot, or LibreChat).

    The UI view shows a styled list of notes with:
    - Note titles and content previews
    - Metadata (UID, version)
    - Visual styling for easy scanning

    Args:
        limit: Maximum number of notes to display (1-100, default 20)
        include_deleted: Whether to include soft-deleted notes (default False)
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (HTML and/or Remote DOM)

    Examples:
        # Show recent notes with HTML UI (default)
        >>> await list_notes_ui(limit=10)

        # Include deleted notes with Remote DOM UI
        >>> await list_notes_ui(include_deleted=True, ui_format="remote-dom")

        # Return both HTML and Remote DOM
        >>> await list_notes_ui(ui_format="both")
    """
    logger.info(f"Rendering notes UI: limit={limit}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Reuse existing data tool to fetch notes
    notes_response: NotesListResponse = await _list_notes(
        limit=limit,
        cursor=None,
        include_deleted=include_deleted,
    )

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = notes_templates.render_notes_list_html(
            notes_response.items,
            limit=limit,
            include_deleted=include_deleted,
        )

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = notes_dom_templates.render_notes_list_dom(
            notes_response.items,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=ui_format,
        )

    # Human-readable summary (shown even if host ignores UIResource)
    count = len(notes_response.items)
    summary = f"Displaying {count} note(s) (limit={limit}, include_deleted={include_deleted})"

    if notes_response.next_cursor:
        summary += f"\nMore notes available (cursor: {notes_response.next_cursor[:20]}...)"

    ui_uri = "ui://toolbridge/notes/list"

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool()
async def show_note_ui(
    uid: Annotated[str, Field(description="UID of the note to display")],
    include_deleted: Annotated[bool, Field(description="Allow deleted notes")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
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
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (HTML and/or Remote DOM detail view)

    Examples:
        # Show a specific note with HTML UI
        >>> await show_note_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Show a deleted note with Remote DOM UI
        >>> await show_note_ui("c1d9b7dc-...", include_deleted=True, ui_format="remote-dom")
    """
    logger.info(f"Rendering note UI: uid={uid}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Fetch the note using existing data tool
    note: Note = await _get_note(uid=uid, include_deleted=include_deleted)

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = notes_templates.render_note_detail_html(note)

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = notes_dom_templates.render_note_detail_dom(note, ui_format=ui_format)

    # Human-readable summary (guard against null values)
    title = note.payload.get("title") or "Untitled note"
    content = note.payload.get("content") or ""
    content_preview = content[:100]
    if len(content) > 100:
        content_preview += "..."

    summary = f"Note: {title}\n\n{content_preview}\n\n(UID: {uid}, version: {note.version})"

    ui_uri = f"ui://toolbridge/notes/{uid}"

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool()
async def delete_note_ui(
    uid: Annotated[str, Field(description="UID of the note to delete")],
    limit: Annotated[int, Field(ge=1, le=100, description="Max notes to display in refreshed list")] = 20,
    include_deleted: Annotated[bool, Field(description="Include deleted notes in refreshed list")] = False,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Delete a note and return updated UI (MCP-UI).

    Soft deletes the note and returns an updated notes list with interactive HTML or Remote DOM.

    Args:
        uid: Unique identifier of the note to delete
        limit: Maximum notes to display in refreshed list (preserves list context)
        include_deleted: Whether to include deleted notes (preserves list context)
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and UIResource(s) (updated HTML and/or Remote DOM list)

    Examples:
        >>> await delete_note_ui("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Delete with custom list context and Remote DOM
        >>> await delete_note_ui("c1d9b7dc-...", limit=50, include_deleted=True, ui_format="remote-dom")
    """
    logger.info(f"Deleting note UI: uid={uid}, limit={limit}, include_deleted={include_deleted}, ui_format={ui_format}")

    # Perform the delete using the underlying tool
    deleted_note: Note = await _delete_note(uid=uid)
    note_title = deleted_note.payload.get("title", "Note")

    # Fetch updated notes list with preserved context
    notes_response: NotesListResponse = await _list_notes(limit=limit, include_deleted=include_deleted)

    html: str | None = None
    remote_dom: dict | None = None

    # Only render HTML when needed (html or both)
    if ui_format in ("html", "both"):
        html = notes_templates.render_notes_list_html(
            notes_response.items,
            limit=limit,
            include_deleted=include_deleted,
        )

    # Only render Remote DOM when needed (remote-dom or both)
    if ui_format in ("remote-dom", "both"):
        remote_dom = notes_dom_templates.render_notes_list_dom(
            notes_response.items,
            limit=limit,
            include_deleted=include_deleted,
            ui_format=ui_format,
        )

    summary = f"Deleted '{note_title}' - {len(notes_response.items)} note(s) remaining"

    return build_ui_with_text_and_dom(
        uri="ui://toolbridge/notes/list",
        html=html,
        remote_dom=remote_dom,
        text_summary=summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool()
async def edit_note_ui(
    uid: Annotated[str, Field(description="UID of the note to edit")],
    new_content: Annotated[
        str,
        Field(description="Proposed full rewritten note content (markdown)"),
    ],
    summary: Annotated[
        str | None,
        Field(description="Short human summary of the change, optional"),
    ] = None,
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Propose changes to a note and display a diff preview (MCP-UI).

    This tool creates an edit session with the proposed changes and returns
    a visual diff preview with Accept/Discard buttons. The user must click
    Accept to apply the changes via the apply_note_edit tool.

    Use this tool when the user asks you to rewrite, refactor, or modify
    the content of a note in Editor Chat mode.

    Args:
        uid: Unique identifier of the note (UUID format)
        new_content: The complete rewritten note content with changes applied
        summary: Optional short human-readable description of what changed
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and HTML/Remote DOM diff preview
        with Accept/Discard action buttons

    Examples:
        # Propose a rewrite with summary (HTML default)
        >>> await edit_note_ui(
        ...     uid="c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
        ...     new_content="# Updated Title\\n\\nNew paragraph content...",
        ...     summary="Converted to heading format and simplified content"
        ... )

        # Propose a rewrite with Remote DOM format
        >>> await edit_note_ui(
        ...     uid="c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
        ...     new_content="# Updated Title\\n\\nNew paragraph content...",
        ...     summary="Converted to heading format",
        ...     ui_format="remote-dom"
        ... )
    """
    logger.info(f"Creating note edit session: uid={uid}, ui_format={ui_format}")

    # Get user ID for session tracking (optional)
    user_id: str | None = None
    try:
        token = get_access_token()
        user_id = token.claims.get("sub")
    except Exception:
        pass

    # Fetch the current note
    note: Note = await _get_note(uid=uid, include_deleted=False)
    title = (note.payload.get("title") or "Untitled note").strip()

    # Compute diff hunks before creating session
    # Use truncate_unchanged=False for accurate line ranges in annotation,
    # then truncate display text afterwards to avoid misleading line numbers.
    original_content = note.payload.get("content") or ""
    diff_hunks = compute_line_diff(original_content, new_content, truncate_unchanged=False)
    diff_hunks = annotate_hunks_with_ids(diff_hunks)
    # Now truncate long unchanged sections for display (line ranges already computed)
    diff_hunks = _truncate_unchanged_for_display(diff_hunks)

    # Create edit session with annotated hunks
    session = create_session(
        note=note,
        proposed_content=new_content,
        summary=summary,
        user_id=user_id,
        hunks=diff_hunks,
    )

    # Build HTML and/or Remote DOM depending on ui_format
    html: str | None = None
    remote_dom: dict | None = None

    if ui_format in ("html", "both"):
        html = note_edits_templates.render_note_edit_diff_html(
            note=note,
            hunks=session.hunks,
            edit_id=session.id,
            summary=summary,
        )

    if ui_format in ("remote-dom", "both"):
        remote_dom = note_edits_dom.render_note_edit_diff_dom(
            note=note,
            hunks=session.hunks,
            edit_id=session.id,
            summary=summary,
        )

    # Build fallback text summary
    text_summary = summary or f"Proposed changes to '{title}' (v{note.version})"

    # Unique URI per session to avoid dedup collisions
    ui_uri = f"ui://toolbridge/notes/{uid}/edit/{session.id}"

    # Chat framing metadata (only for Remote DOM)
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    ) if ui_format in ("remote-dom", "both") else None

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat(ui_format),
        remote_dom_ui_metadata=ui_metadata,
        remote_dom_metadata={
            "note_uid": uid,
            "edit_id": session.id,
        } if ui_format in ("remote-dom", "both") else None,
    )


@mcp.tool()
async def apply_note_edit(
    edit_id: Annotated[str, Field(description="ID of the pending note edit session")],
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Apply a pending note edit session.

    This tool is called when the user clicks "Apply changes" on a diff preview.
    It performs optimistic concurrency checking and updates the note via the REST API.

    **Important**: This tool should NOT be called by the LLM directly. It is invoked
    only by the Flutter UI when the user clicks the Accept button.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and HTML/Remote DOM confirmation

    Raises:
        ValueError: If session not found or expired
        httpx.HTTPStatusError: 409 if note was modified concurrently
    """
    logger.info(f"Applying note edit: edit_id={edit_id}")

    # Helper to build error response with both formats
    def build_error_response(error_msg: str, uri: str, note_uid: str | None = None):
        html = None
        remote_dom = None
        if ui_format in ("html", "both"):
            html = note_edits_templates.render_note_edit_error_html(error_msg, note_uid)
        if ui_format in ("remote-dom", "both"):
            remote_dom = note_edits_dom.render_note_edit_error_dom(error_msg, note_uid)
        return build_ui_with_text_and_dom(
            uri=uri,
            html=html,
            remote_dom=remote_dom,
            text_summary=error_msg,
            ui_format=UIFormat(ui_format),
        )

    # Retrieve session
    session = get_session(edit_id)
    if session is None:
        error_msg = f"Edit session '{edit_id}' not found or expired"
        logger.warning(error_msg)
        return build_error_response(error_msg, f"ui://toolbridge/notes/edit/{edit_id}/error")

    try:
        # Fetch latest note to check version
        current = await _get_note(uid=session.note_uid, include_deleted=False)

        # Version conflict check
        if current.version != session.base_version:
            error_msg = (
                f"Note '{session.title}' was modified since the edit was proposed. "
                f"Expected v{session.base_version}, found v{current.version}. "
                "Please re-run edit_note_ui to create a fresh diff."
            )
            logger.warning(f"Version conflict: {error_msg}")

            # Discard the stale session
            discard_session(edit_id)

            return build_error_response(
                error_msg,
                f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}/conflict",
                session.note_uid,
            )

        # Check for pending hunks - all changed hunks must be resolved
        unresolved = [
            h for h in session.hunks
            if h.kind != "unchanged" and h.status == "pending"
        ]
        if unresolved:
            error_msg = (
                f"There are {len(unresolved)} pending change(s). "
                "Please accept, reject, or revise each change before applying."
            )
            logger.warning(f"Pending hunks: {error_msg}")

            return build_error_response(
                error_msg,
                f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}/pending",
                session.note_uid,
            )

        # Determine content to apply - use merged content from hunk decisions
        if session.current_content is not None:
            merged_content = session.current_content
        else:
            # Defensive fallback: recompute from full content to avoid data loss
            # Session hunks may have truncated unchanged content for display,
            # so we recompute from full original/proposed content.
            full_hunks = compute_line_diff(
                session.original_content,
                session.proposed_content,
                truncate_unchanged=False,
            )
            full_hunks = annotate_hunks_with_ids(full_hunks)
            diff_hunks = [
                DiffHunk(
                    kind=h.kind,
                    original=h.original,
                    proposed=h.proposed,
                    id=h.id,
                    orig_start=h.orig_start,
                    orig_end=h.orig_end,
                    new_start=h.new_start,
                    new_end=h.new_end,
                )
                for h in full_hunks
            ]
            decisions = {
                h.id: HunkDecision(status=h.status, revised_text=h.revised_text)
                for h in session.hunks if h.id
            }
            merged_content = apply_hunk_decisions(diff_hunks, decisions)

        # Prepare additional fields (preserve tags, etc.)
        additional_fields = {
            k: v for k, v in current.payload.items()
            if k not in ("title", "content")
        }

        # Apply the update with optimistic locking using merged content
        updated = await _update_note(
            uid=session.note_uid,
            title=current.payload.get("title") or "",
            content=merged_content,
            if_match=session.base_version,
            additional_fields=additional_fields if additional_fields else None,
        )

        # Discard the session after successful apply
        discard_session(edit_id)

        # Build success response
        text_summary = (
            f"Applied note edit to '{session.title}'. "
            f"New version: v{updated.version}."
        )

        html: str | None = None
        remote_dom: dict | None = None

        if ui_format in ("html", "both"):
            html = note_edits_templates.render_note_edit_success_html(updated)

        if ui_format in ("remote-dom", "both"):
            remote_dom = note_edits_dom.render_note_edit_success_dom(updated)

        ui_uri = f"ui://toolbridge/notes/{updated.uid}"
        ui_metadata = get_chat_metadata(
            frame_style=Layout.CHAT_FRAME_CARD,
            max_width=Layout.MAX_WIDTH_DETAIL,
        ) if ui_format in ("remote-dom", "both") else None

        return build_ui_with_text_and_dom(
            uri=ui_uri,
            html=html,
            remote_dom=remote_dom,
            text_summary=text_summary,
            ui_format=UIFormat(ui_format),
            remote_dom_ui_metadata=ui_metadata,
        )

    except httpx.HTTPStatusError as e:
        error_msg = f"Failed to update note: {e.response.status_code} - {e.response.text}"
        logger.error(f"HTTP error applying note edit: {error_msg}")
        return build_error_response(
            error_msg,
            f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}/error",
            session.note_uid,
        )

    except Exception as e:
        error_msg = f"Unexpected error applying note edit: {str(e)}"
        logger.exception(error_msg)
        return build_error_response(
            error_msg,
            f"ui://toolbridge/notes/edit/{edit_id}/error",
        )


@mcp.tool()
async def discard_note_edit(
    edit_id: Annotated[str, Field(description="ID of the pending note edit session")],
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Discard a pending note edit session.

    This tool is called when the user clicks "Discard" on a diff preview.
    It removes the pending edit session without applying any changes.

    **Important**: This tool should NOT be called by the LLM directly. It is invoked
    only by the Flutter UI when the user clicks the Discard button.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and HTML/Remote DOM confirmation
    """
    logger.info(f"Discarding note edit: edit_id={edit_id}")

    session = discard_session(edit_id)

    if session is None:
        # Already discarded or expired
        title = "note"
        text_summary = f"Edit session '{edit_id}' was already discarded or expired."
    else:
        title = session.title
        text_summary = f"Discarded pending edit session for '{title}'."

    # Build confirmation UI
    html: str | None = None
    remote_dom: dict | None = None

    if ui_format in ("html", "both"):
        html = note_edits_templates.render_note_edit_discarded_html(title)

    if ui_format in ("remote-dom", "both"):
        remote_dom = note_edits_dom.render_note_edit_discarded_dom(title)

    ui_uri = f"ui://toolbridge/notes/edit/{edit_id}/discarded"

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat(ui_format),
    )


@mcp.tool()
async def accept_note_edit_hunk(
    edit_id: Annotated[str, Field(description="ID of the pending note edit session")],
    hunk_id: Annotated[str, Field(description="ID of the diff hunk to accept (e.g., 'h1', 'h2')")],
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Accept a specific diff hunk in a pending note edit session.

    Marks the hunk as accepted, meaning the proposed change will be included
    when the edit is applied.

    **Important**: This tool should NOT be called by the LLM directly. It is invoked
    only by the Flutter UI when the user clicks the Accept button on a specific hunk.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call
        hunk_id: The ID of the hunk to accept (e.g., 'h1', 'h2')
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and updated HTML/Remote DOM diff preview
    """
    logger.info(f"Accepting hunk: edit_id={edit_id}, hunk_id={hunk_id}")

    session = set_hunk_status(edit_id, hunk_id, "accepted")
    if session is None:
        error_msg = f"Edit session '{edit_id}' not found or expired"
        logger.warning(error_msg)

        html = None
        remote_dom = None
        if ui_format in ("html", "both"):
            html = note_edits_templates.render_note_edit_error_html(error_msg)
        if ui_format in ("remote-dom", "both"):
            remote_dom = note_edits_dom.render_note_edit_error_dom(error_msg)

        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=html,
            remote_dom=remote_dom,
            text_summary=error_msg,
            ui_format=UIFormat(ui_format),
        )

    # Get current counts for summary
    counts = get_hunk_counts(edit_id)
    text_summary = (
        f"Accepted hunk {hunk_id}. "
        f"Status: {counts['accepted']} accepted, {counts['rejected']} rejected, "
        f"{counts['revised']} revised, {counts['pending']} pending."
    )

    # Fetch note for rendering
    note = await _get_note(uid=session.note_uid, include_deleted=False)

    # Build HTML and/or Remote DOM
    html: str | None = None
    remote_dom: dict | None = None

    if ui_format in ("html", "both"):
        html = note_edits_templates.render_note_edit_diff_html(
            note=note,
            hunks=session.hunks,
            edit_id=edit_id,
            summary=session.summary,
        )

    if ui_format in ("remote-dom", "both"):
        remote_dom = note_edits_dom.render_note_edit_diff_dom(
            note=note,
            hunks=session.hunks,
            edit_id=edit_id,
            summary=session.summary,
        )

    ui_uri = f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    ) if ui_format in ("remote-dom", "both") else None

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat(ui_format),
        remote_dom_ui_metadata=ui_metadata,
    )


@mcp.tool()
async def reject_note_edit_hunk(
    edit_id: Annotated[str, Field(description="ID of the pending note edit session")],
    hunk_id: Annotated[str, Field(description="ID of the diff hunk to reject (e.g., 'h1', 'h2')")],
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Reject a specific diff hunk in a pending note edit session.

    Marks the hunk as rejected, meaning the proposed change will NOT be included
    when the edit is applied (the original content will be kept).

    **Important**: This tool should NOT be called by the LLM directly. It is invoked
    only by the Flutter UI when the user clicks the Reject button on a specific hunk.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call
        hunk_id: The ID of the hunk to reject (e.g., 'h1', 'h2')
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and updated HTML/Remote DOM diff preview
    """
    logger.info(f"Rejecting hunk: edit_id={edit_id}, hunk_id={hunk_id}")

    session = set_hunk_status(edit_id, hunk_id, "rejected")
    if session is None:
        error_msg = f"Edit session '{edit_id}' not found or expired"
        logger.warning(error_msg)

        html = None
        remote_dom = None
        if ui_format in ("html", "both"):
            html = note_edits_templates.render_note_edit_error_html(error_msg)
        if ui_format in ("remote-dom", "both"):
            remote_dom = note_edits_dom.render_note_edit_error_dom(error_msg)

        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=html,
            remote_dom=remote_dom,
            text_summary=error_msg,
            ui_format=UIFormat(ui_format),
        )

    # Get current counts for summary
    counts = get_hunk_counts(edit_id)
    text_summary = (
        f"Rejected hunk {hunk_id}. "
        f"Status: {counts['accepted']} accepted, {counts['rejected']} rejected, "
        f"{counts['revised']} revised, {counts['pending']} pending."
    )

    # Fetch note for rendering
    note = await _get_note(uid=session.note_uid, include_deleted=False)

    # Build HTML and/or Remote DOM
    html: str | None = None
    remote_dom: dict | None = None

    if ui_format in ("html", "both"):
        html = note_edits_templates.render_note_edit_diff_html(
            note=note,
            hunks=session.hunks,
            edit_id=edit_id,
            summary=session.summary,
        )

    if ui_format in ("remote-dom", "both"):
        remote_dom = note_edits_dom.render_note_edit_diff_dom(
            note=note,
            hunks=session.hunks,
            edit_id=edit_id,
            summary=session.summary,
        )

    ui_uri = f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    ) if ui_format in ("remote-dom", "both") else None

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat(ui_format),
        remote_dom_ui_metadata=ui_metadata,
    )


@mcp.tool()
async def revise_note_edit_hunk(
    edit_id: Annotated[str, Field(description="ID of the pending note edit session")],
    hunk_id: Annotated[str, Field(description="ID of the diff hunk to revise (e.g., 'h1', 'h2')")],
    revised_text: Annotated[str, Field(description="Replacement text for this hunk")],
    ui_format: Annotated[
        str,
        Field(
            description="UI format: 'html' (default), 'remote-dom', or 'both'",
            pattern="^(html|remote-dom|both)$",
        ),
    ] = "html",
) -> List[Union[TextContent, EmbeddedResource]]:
    """
    Revise a specific diff hunk in a pending note edit session.

    Replaces the proposed change with custom text provided by the user.
    The revised text will be used instead of the original proposed change.

    **Important**: This tool should NOT be called by the LLM directly. It is invoked
    only by the Flutter UI when the user clicks the Revise button and provides
    replacement text.

    Args:
        edit_id: The edit session ID from a previous edit_note_ui call
        hunk_id: The ID of the hunk to revise (e.g., 'h1', 'h2')
        revised_text: The replacement text to use instead of the proposed change
        ui_format: UI format to return - 'html' (default), 'remote-dom', or 'both'

    Returns:
        List containing TextContent (summary) and updated HTML/Remote DOM diff preview
    """
    logger.info(f"Revising hunk: edit_id={edit_id}, hunk_id={hunk_id}")

    session = set_hunk_status(edit_id, hunk_id, "revised", revised_text=revised_text)
    if session is None:
        error_msg = f"Edit session '{edit_id}' not found or expired"
        logger.warning(error_msg)

        html = None
        remote_dom = None
        if ui_format in ("html", "both"):
            html = note_edits_templates.render_note_edit_error_html(error_msg)
        if ui_format in ("remote-dom", "both"):
            remote_dom = note_edits_dom.render_note_edit_error_dom(error_msg)

        return build_ui_with_text_and_dom(
            uri=f"ui://toolbridge/notes/edit/{edit_id}/error",
            html=html,
            remote_dom=remote_dom,
            text_summary=error_msg,
            ui_format=UIFormat(ui_format),
        )

    # Get current counts for summary
    counts = get_hunk_counts(edit_id)
    text_summary = (
        f"Revised hunk {hunk_id}. "
        f"Status: {counts['accepted']} accepted, {counts['rejected']} rejected, "
        f"{counts['revised']} revised, {counts['pending']} pending."
    )

    # Fetch note for rendering
    note = await _get_note(uid=session.note_uid, include_deleted=False)

    # Build HTML and/or Remote DOM
    html: str | None = None
    remote_dom: dict | None = None

    if ui_format in ("html", "both"):
        html = note_edits_templates.render_note_edit_diff_html(
            note=note,
            hunks=session.hunks,
            edit_id=edit_id,
            summary=session.summary,
        )

    if ui_format in ("remote-dom", "both"):
        remote_dom = note_edits_dom.render_note_edit_diff_dom(
            note=note,
            hunks=session.hunks,
            edit_id=edit_id,
            summary=session.summary,
        )

    ui_uri = f"ui://toolbridge/notes/{session.note_uid}/edit/{edit_id}"
    ui_metadata = get_chat_metadata(
        frame_style=Layout.CHAT_FRAME_CARD,
        max_width=Layout.MAX_WIDTH_DETAIL,
    ) if ui_format in ("remote-dom", "both") else None

    return build_ui_with_text_and_dom(
        uri=ui_uri,
        html=html,
        remote_dom=remote_dom,
        text_summary=text_summary,
        ui_format=UIFormat(ui_format),
        remote_dom_ui_metadata=ui_metadata,
    )
