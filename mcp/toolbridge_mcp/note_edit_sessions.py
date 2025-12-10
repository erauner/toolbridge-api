"""
In-memory note edit session storage.

Maintains short-lived "pending edits" so that:
- `edit_note_ui` can create a session (original + proposed),
- `apply_note_edit` / `discard_note_edit` can refer to that session by ID,
- We can do optimistic concurrency checks (version at create vs version at apply).

Note: This is per-process storage. Multi-instance deployments will need
a shared store (Redis/DB) in the future.
"""

from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Dict, List, Literal, Optional
import uuid

from toolbridge_mcp.tools.notes import Note
from toolbridge_mcp.utils.diff import (
    DiffHunk,
    HunkDecision,
    apply_hunk_decisions,
    compute_line_diff,
    annotate_hunks_with_ids,
)


@dataclass
class NoteEditHunkState:
    """
    Per-hunk state within a note edit session.
    
    Combines DiffHunk data with user decision status.
    """
    id: str                     # Same as DiffHunk.id
    kind: Literal["unchanged", "added", "removed", "modified"]
    original: str
    proposed: str
    status: Literal["pending", "accepted", "rejected", "revised"]
    revised_text: Optional[str] = None
    orig_start: Optional[int] = None
    orig_end: Optional[int] = None
    new_start: Optional[int] = None
    new_end: Optional[int] = None


@dataclass
class NoteEditSession:
    """A pending note edit awaiting user approval."""
    
    id: str                     # UUID4 hex
    note_uid: str               # Note UID
    base_version: int           # note.version at session creation
    title: str                  # Note title for display
    original_content: str       # Content before changes
    proposed_content: str       # Content after changes
    summary: Optional[str]      # Human-readable change description
    created_at: datetime = field(default_factory=datetime.utcnow)
    created_by: Optional[str] = None  # User ID from access token
    hunks: List[NoteEditHunkState] = field(default_factory=list)
    current_content: Optional[str] = None  # Merged content based on decisions


# Module-level in-memory storage
_SESSIONS: Dict[str, NoteEditSession] = {}


def create_session(
    note: Note,
    proposed_content: str,
    summary: Optional[str] = None,
    user_id: Optional[str] = None,
    hunks: Optional[List[DiffHunk]] = None,
) -> NoteEditSession:
    """
    Create a new note edit session.
    
    Args:
        note: The current note from the API
        proposed_content: The proposed new content
        summary: Optional human-readable change description
        user_id: Optional user ID from access token
        hunks: Optional list of annotated DiffHunks (with IDs and line ranges)
        
    Returns:
        The created NoteEditSession
    """
    session_id = uuid.uuid4().hex
    
    # Build per-hunk state from DiffHunks
    hunk_states: List[NoteEditHunkState] = []
    if hunks:
        for h in hunks:
            # Unchanged hunks are implicitly accepted; changed hunks start pending
            status: Literal["pending", "accepted", "rejected", "revised"] = (
                "accepted" if h.kind == "unchanged" else "pending"
            )
            hunk_states.append(
                NoteEditHunkState(
                    id=h.id or "",
                    kind=h.kind,
                    original=h.original,
                    proposed=h.proposed,
                    status=status,
                    orig_start=h.orig_start,
                    orig_end=h.orig_end,
                    new_start=h.new_start,
                    new_end=h.new_end,
                )
            )
    
    session = NoteEditSession(
        id=session_id,
        note_uid=note.uid,
        base_version=note.version,
        title=(note.payload.get("title") or "Untitled note").strip(),
        # Preserve whitespace verbatim - important for markdown/code formatting
        original_content=note.payload.get("content") or "",
        proposed_content=proposed_content,
        summary=summary,
        created_by=user_id,
        hunks=hunk_states,
        current_content=None,  # Will be computed when all hunks are resolved
    )
    
    _SESSIONS[session_id] = session
    return session


def get_session(edit_id: str) -> Optional[NoteEditSession]:
    """
    Retrieve a session by ID.
    
    Args:
        edit_id: The session ID
        
    Returns:
        The session if found, None otherwise
    """
    return _SESSIONS.get(edit_id)


def discard_session(edit_id: str) -> Optional[NoteEditSession]:
    """
    Remove and return a session.
    
    Args:
        edit_id: The session ID
        
    Returns:
        The removed session if found, None otherwise
    """
    return _SESSIONS.pop(edit_id, None)


def cleanup_expired_sessions(max_age: timedelta = timedelta(hours=1)) -> int:
    """
    Remove sessions older than max_age.
    
    Args:
        max_age: Maximum session age (default 1 hour)
        
    Returns:
        Number of sessions removed
    """
    now = datetime.utcnow()
    expired = [
        session_id
        for session_id, session in _SESSIONS.items()
        if now - session.created_at > max_age
    ]
    
    for session_id in expired:
        del _SESSIONS[session_id]
    
    return len(expired)


def get_session_count() -> int:
    """Return the current number of active sessions."""
    return len(_SESSIONS)


def set_hunk_status(
    edit_id: str,
    hunk_id: str,
    status: Literal["pending", "accepted", "rejected", "revised"],
    revised_text: Optional[str] = None,
) -> Optional[NoteEditSession]:
    """
    Update the status of a specific hunk in a session.
    
    Args:
        edit_id: The session ID
        hunk_id: The hunk ID (e.g., 'h1', 'h2')
        status: New status for the hunk
        revised_text: Replacement text if status is 'revised'
        
    Returns:
        The updated session, or None if not found
    """
    session = _SESSIONS.get(edit_id)
    if session is None:
        return None
    
    # Find and update the hunk
    for hunk in session.hunks:
        if hunk.id == hunk_id:
            hunk.status = status
            hunk.revised_text = revised_text if status == "revised" else None
            break
    
    # Recompute current_content if all changed hunks are resolved
    _recompute_current_content(session)
    
    return session


def _recompute_current_content(session: NoteEditSession) -> None:
    """
    Recompute session.current_content based on hunk statuses.

    Only computes if all changed hunks are non-pending.

    IMPORTANT: Uses full original/proposed content to avoid data loss from
    truncated unchanged regions in display hunks.
    """
    # Check if any changed hunk is still pending
    any_pending = any(
        h.status == "pending" and h.kind != "unchanged"
        for h in session.hunks
    )

    if any_pending:
        session.current_content = None
        return

    # Build decisions map from session hunks
    decisions: Dict[str, HunkDecision] = {}
    for h in session.hunks:
        if h.id:
            decisions[h.id] = HunkDecision(
                status=h.status,
                revised_text=h.revised_text,
            )

    # Recompute diff from full content (no truncation) to avoid data loss
    # The session hunks may have truncated unchanged content for display,
    # but we need full content for reconstruction.
    full_hunks = compute_line_diff(
        session.original_content,
        session.proposed_content,
        truncate_unchanged=False,
    )
    full_hunks = annotate_hunks_with_ids(full_hunks)

    # Convert to DiffHunk objects for apply_hunk_decisions
    diff_hunks: List[DiffHunk] = [
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

    try:
        session.current_content = apply_hunk_decisions(diff_hunks, decisions)
    except ValueError:
        # Should not happen if any_pending check is correct
        session.current_content = None


def get_pending_hunks(edit_id: str) -> List[NoteEditHunkState]:
    """
    Get all pending (non-unchanged) hunks for a session.
    
    Args:
        edit_id: The session ID
        
    Returns:
        List of pending hunk states, or empty list if session not found
    """
    session = _SESSIONS.get(edit_id)
    if session is None:
        return []
    
    return [
        h for h in session.hunks
        if h.kind != "unchanged" and h.status == "pending"
    ]


def get_hunk_counts(edit_id: str) -> Dict[str, int]:
    """
    Get counts of hunks by status for a session.
    
    Args:
        edit_id: The session ID
        
    Returns:
        Dict with keys: pending, accepted, rejected, revised
        Returns zeros if session not found
    """
    counts = {"pending": 0, "accepted": 0, "rejected": 0, "revised": 0}
    
    session = _SESSIONS.get(edit_id)
    if session is None:
        return counts
    
    for h in session.hunks:
        # Only count changed hunks (exclude unchanged)
        if h.kind != "unchanged":
            counts[h.status] = counts.get(h.status, 0) + 1
    
    return counts
