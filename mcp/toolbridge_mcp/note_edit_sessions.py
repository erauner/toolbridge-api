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
from typing import Dict, Optional
import uuid

from toolbridge_mcp.tools.notes import Note


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


# Module-level in-memory storage
_SESSIONS: Dict[str, NoteEditSession] = {}


def create_session(
    note: Note,
    proposed_content: str,
    summary: Optional[str] = None,
    user_id: Optional[str] = None,
) -> NoteEditSession:
    """
    Create a new note edit session.
    
    Args:
        note: The current note from the API
        proposed_content: The proposed new content
        summary: Optional human-readable change description
        user_id: Optional user ID from access token
        
    Returns:
        The created NoteEditSession
    """
    session_id = uuid.uuid4().hex
    
    session = NoteEditSession(
        id=session_id,
        note_uid=note.uid,
        base_version=note.version,
        title=(note.payload.get("title") or "Untitled note").strip(),
        original_content=(note.payload.get("content") or "").strip(),
        proposed_content=proposed_content.strip(),
        summary=summary,
        created_by=user_id,
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
