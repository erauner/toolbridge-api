"""
Unit tests for note edit session management.

Tests create_session, get_session, set_hunk_status, and related helpers.
"""

import pytest
from datetime import datetime, timedelta
from unittest.mock import MagicMock

from toolbridge_mcp.note_edit_sessions import (
    NoteEditHunkState,
    NoteEditSession,
    create_session,
    get_session,
    discard_session,
    cleanup_expired_sessions,
    get_session_count,
    set_hunk_status,
    get_pending_hunks,
    get_hunk_counts,
    _SESSIONS,
)
from toolbridge_mcp.utils.diff import DiffHunk


@pytest.fixture(autouse=True)
def clear_sessions():
    """Clear all sessions before and after each test."""
    _SESSIONS.clear()
    yield
    _SESSIONS.clear()


def make_mock_note(uid: str = "note-123", version: int = 1, content: str = "test content"):
    """Create a mock Note object for testing."""
    note = MagicMock()
    note.uid = uid
    note.version = version
    note.payload = {"title": "Test Note", "content": content}
    return note


class TestCreateSession:
    """Tests for create_session function."""

    def test_creates_session_with_unique_id(self):
        """Test that create_session generates a unique session ID."""
        note = make_mock_note()
        session = create_session(note, "new content")

        assert session.id is not None
        assert len(session.id) == 32  # UUID4 hex is 32 chars

    def test_stores_session_in_module_cache(self):
        """Test that created session is stored in _SESSIONS."""
        note = make_mock_note()
        session = create_session(note, "new content")

        assert session.id in _SESSIONS
        assert _SESSIONS[session.id] is session

    def test_captures_note_metadata(self):
        """Test that session captures note UID, version, and title."""
        note = make_mock_note(uid="note-456", version=3)
        note.payload["title"] = "My Important Note"

        session = create_session(note, "new content")

        assert session.note_uid == "note-456"
        assert session.base_version == 3
        assert session.title == "My Important Note"

    def test_stores_original_and_proposed_content(self):
        """Test that session stores both original and proposed content."""
        note = make_mock_note(content="original text")
        session = create_session(note, "proposed text")

        assert session.original_content == "original text"
        assert session.proposed_content == "proposed text"

    def test_stores_summary_and_user_id(self):
        """Test that optional summary and user_id are stored."""
        note = make_mock_note()
        session = create_session(
            note,
            "new content",
            summary="Fixed typo",
            user_id="user-123",
        )

        assert session.summary == "Fixed typo"
        assert session.created_by == "user-123"

    def test_handles_empty_title(self):
        """Test that empty title defaults to 'Untitled note'."""
        note = make_mock_note()
        note.payload["title"] = ""
        session = create_session(note, "content")

        assert session.title == "Untitled note"

    def test_handles_none_title(self):
        """Test that None title defaults to 'Untitled note'."""
        note = make_mock_note()
        note.payload["title"] = None
        session = create_session(note, "content")

        assert session.title == "Untitled note"

    def test_creates_hunk_states_from_diff_hunks(self):
        """Test that DiffHunks are converted to NoteEditHunkState."""
        note = make_mock_note()
        hunks = [
            DiffHunk(kind="unchanged", original="a", proposed="a", id="h1"),
            DiffHunk(kind="modified", original="b", proposed="c", id="h2"),
        ]

        session = create_session(note, "content", hunks=hunks)

        assert len(session.hunks) == 2
        assert session.hunks[0].id == "h1"
        assert session.hunks[0].kind == "unchanged"
        assert session.hunks[1].id == "h2"
        assert session.hunks[1].kind == "modified"

    def test_unchanged_hunks_are_accepted(self):
        """Test that unchanged hunks start with 'accepted' status."""
        note = make_mock_note()
        hunks = [DiffHunk(kind="unchanged", original="a", proposed="a", id="h1")]

        session = create_session(note, "content", hunks=hunks)

        assert session.hunks[0].status == "accepted"

    def test_changed_hunks_are_pending(self):
        """Test that changed hunks start with 'pending' status."""
        note = make_mock_note()
        hunks = [
            DiffHunk(kind="added", original="", proposed="new", id="h1"),
            DiffHunk(kind="removed", original="old", proposed="", id="h2"),
            DiffHunk(kind="modified", original="a", proposed="b", id="h3"),
        ]

        session = create_session(note, "content", hunks=hunks)

        assert session.hunks[0].status == "pending"
        assert session.hunks[1].status == "pending"
        assert session.hunks[2].status == "pending"

    def test_copies_line_ranges_from_diff_hunks(self):
        """Test that line ranges are copied from DiffHunks."""
        note = make_mock_note()
        hunks = [
            DiffHunk(
                kind="modified",
                original="line",
                proposed="changed",
                id="h1",
                orig_start=10,
                orig_end=15,
                new_start=10,
                new_end=12,
            )
        ]

        session = create_session(note, "content", hunks=hunks)

        assert session.hunks[0].orig_start == 10
        assert session.hunks[0].orig_end == 15
        assert session.hunks[0].new_start == 10
        assert session.hunks[0].new_end == 12


class TestGetSession:
    """Tests for get_session function."""

    def test_returns_session_by_id(self):
        """Test that get_session returns the correct session."""
        note = make_mock_note()
        created = create_session(note, "content")

        retrieved = get_session(created.id)

        assert retrieved is created

    def test_returns_none_for_unknown_id(self):
        """Test that get_session returns None for unknown ID."""
        result = get_session("nonexistent-id")

        assert result is None


class TestDiscardSession:
    """Tests for discard_session function."""

    def test_removes_and_returns_session(self):
        """Test that discard_session removes and returns the session."""
        note = make_mock_note()
        session = create_session(note, "content")
        session_id = session.id

        discarded = discard_session(session_id)

        assert discarded is session
        assert session_id not in _SESSIONS

    def test_returns_none_for_unknown_id(self):
        """Test that discard_session returns None for unknown ID."""
        result = discard_session("nonexistent-id")

        assert result is None


class TestSetHunkStatus:
    """Tests for set_hunk_status function."""

    def test_updates_hunk_to_accepted(self):
        """Test setting a hunk status to accepted."""
        note = make_mock_note()
        hunks = [DiffHunk(kind="modified", original="a", proposed="b", id="h1")]
        session = create_session(note, "b", hunks=hunks)

        result = set_hunk_status(session.id, "h1", "accepted")

        assert result is session
        assert session.hunks[0].status == "accepted"

    def test_updates_hunk_to_rejected(self):
        """Test setting a hunk status to rejected."""
        note = make_mock_note()
        hunks = [DiffHunk(kind="modified", original="a", proposed="b", id="h1")]
        session = create_session(note, "b", hunks=hunks)

        set_hunk_status(session.id, "h1", "rejected")

        assert session.hunks[0].status == "rejected"

    def test_updates_hunk_to_revised_with_text(self):
        """Test setting a hunk status to revised with custom text."""
        note = make_mock_note()
        hunks = [DiffHunk(kind="modified", original="a", proposed="b", id="h1")]
        session = create_session(note, "b", hunks=hunks)

        set_hunk_status(session.id, "h1", "revised", revised_text="custom")

        assert session.hunks[0].status == "revised"
        assert session.hunks[0].revised_text == "custom"

    def test_clears_revised_text_when_not_revised(self):
        """Test that revised_text is cleared when status is not 'revised'."""
        note = make_mock_note()
        hunks = [DiffHunk(kind="modified", original="a", proposed="b", id="h1")]
        session = create_session(note, "b", hunks=hunks)

        # First set to revised with text
        set_hunk_status(session.id, "h1", "revised", revised_text="custom")
        # Then change to accepted
        set_hunk_status(session.id, "h1", "accepted")

        assert session.hunks[0].status == "accepted"
        assert session.hunks[0].revised_text is None

    def test_returns_none_for_unknown_session(self):
        """Test that set_hunk_status returns None for unknown session."""
        result = set_hunk_status("nonexistent", "h1", "accepted")

        assert result is None

    def test_ignores_unknown_hunk_id(self):
        """Test that unknown hunk ID does not cause error."""
        note = make_mock_note()
        hunks = [DiffHunk(kind="modified", original="a", proposed="b", id="h1")]
        session = create_session(note, "b", hunks=hunks)

        # This should not raise, just leave hunks unchanged
        result = set_hunk_status(session.id, "h999", "accepted")

        assert result is session
        assert session.hunks[0].status == "pending"

    def test_computes_current_content_when_all_resolved(self):
        """Test that current_content is computed when all hunks are resolved."""
        note = make_mock_note(content="line 1\nline 2")
        hunks = [
            DiffHunk(kind="unchanged", original="line 1", proposed="line 1", id="h1"),
            DiffHunk(kind="modified", original="line 2", proposed="line 2 modified", id="h2"),
        ]
        session = create_session(note, "line 1\nline 2 modified", hunks=hunks)

        # Current content should be None while h2 is pending
        assert session.current_content is None

        # Accept h2
        set_hunk_status(session.id, "h2", "accepted")

        # Now current_content should be computed
        assert session.current_content is not None
        assert "line 1" in session.current_content
        assert "line 2 modified" in session.current_content

    def test_current_content_none_while_hunks_pending(self):
        """Test that current_content stays None while any hunk is pending."""
        note = make_mock_note(content="a\nb\nc")
        hunks = [
            DiffHunk(kind="modified", original="a", proposed="A", id="h1"),
            DiffHunk(kind="modified", original="b", proposed="B", id="h2"),
        ]
        session = create_session(note, "A\nB\nc", hunks=hunks)

        # Accept only h1
        set_hunk_status(session.id, "h1", "accepted")

        # h2 still pending, so current_content should be None
        assert session.current_content is None


class TestGetPendingHunks:
    """Tests for get_pending_hunks function."""

    def test_returns_only_pending_changed_hunks(self):
        """Test that only pending changed hunks are returned."""
        note = make_mock_note()
        hunks = [
            DiffHunk(kind="unchanged", original="a", proposed="a", id="h1"),
            DiffHunk(kind="modified", original="b", proposed="c", id="h2"),
            DiffHunk(kind="added", original="", proposed="d", id="h3"),
        ]
        session = create_session(note, "content", hunks=hunks)

        pending = get_pending_hunks(session.id)

        assert len(pending) == 2
        ids = [h.id for h in pending]
        assert "h2" in ids
        assert "h3" in ids
        assert "h1" not in ids  # unchanged

    def test_excludes_resolved_hunks(self):
        """Test that accepted/rejected hunks are excluded."""
        note = make_mock_note()
        hunks = [
            DiffHunk(kind="modified", original="a", proposed="b", id="h1"),
            DiffHunk(kind="modified", original="c", proposed="d", id="h2"),
        ]
        session = create_session(note, "content", hunks=hunks)

        # Accept h1
        set_hunk_status(session.id, "h1", "accepted")

        pending = get_pending_hunks(session.id)

        assert len(pending) == 1
        assert pending[0].id == "h2"

    def test_returns_empty_for_unknown_session(self):
        """Test that unknown session returns empty list."""
        pending = get_pending_hunks("nonexistent")

        assert pending == []


class TestGetHunkCounts:
    """Tests for get_hunk_counts function."""

    def test_counts_all_statuses(self):
        """Test that all status types are counted correctly."""
        note = make_mock_note()
        hunks = [
            DiffHunk(kind="modified", original="a", proposed="b", id="h1"),
            DiffHunk(kind="modified", original="c", proposed="d", id="h2"),
            DiffHunk(kind="added", original="", proposed="e", id="h3"),
            DiffHunk(kind="removed", original="f", proposed="", id="h4"),
        ]
        session = create_session(note, "content", hunks=hunks)

        # Set various statuses
        set_hunk_status(session.id, "h1", "accepted")
        set_hunk_status(session.id, "h2", "rejected")
        set_hunk_status(session.id, "h3", "revised", revised_text="custom")
        # h4 remains pending

        counts = get_hunk_counts(session.id)

        assert counts["accepted"] == 1
        assert counts["rejected"] == 1
        assert counts["revised"] == 1
        assert counts["pending"] == 1

    def test_excludes_unchanged_hunks_from_count(self):
        """Test that unchanged hunks are not counted."""
        note = make_mock_note()
        hunks = [
            DiffHunk(kind="unchanged", original="a", proposed="a", id="h1"),
            DiffHunk(kind="unchanged", original="b", proposed="b", id="h2"),
            DiffHunk(kind="modified", original="c", proposed="d", id="h3"),
        ]
        session = create_session(note, "content", hunks=hunks)

        counts = get_hunk_counts(session.id)

        # Only h3 should be counted (as pending)
        assert counts["pending"] == 1
        assert counts["accepted"] == 0

    def test_returns_zeros_for_unknown_session(self):
        """Test that unknown session returns all zeros."""
        counts = get_hunk_counts("nonexistent")

        assert counts == {"pending": 0, "accepted": 0, "rejected": 0, "revised": 0}


class TestCleanupExpiredSessions:
    """Tests for cleanup_expired_sessions function."""

    def test_removes_expired_sessions(self):
        """Test that sessions older than max_age are removed."""
        note = make_mock_note()
        session = create_session(note, "content")

        # Manually set created_at to 2 hours ago
        session.created_at = datetime.utcnow() - timedelta(hours=2)

        removed = cleanup_expired_sessions(max_age=timedelta(hours=1))

        assert removed == 1
        assert session.id not in _SESSIONS

    def test_keeps_recent_sessions(self):
        """Test that recent sessions are not removed."""
        note = make_mock_note()
        session = create_session(note, "content")
        # created_at is now, so should not be expired

        removed = cleanup_expired_sessions(max_age=timedelta(hours=1))

        assert removed == 0
        assert session.id in _SESSIONS

    def test_returns_count_of_removed_sessions(self):
        """Test that the correct count is returned."""
        note = make_mock_note()
        s1 = create_session(note, "content1")
        s2 = create_session(note, "content2")
        s3 = create_session(note, "content3")

        # Make s1 and s2 expired
        s1.created_at = datetime.utcnow() - timedelta(hours=2)
        s2.created_at = datetime.utcnow() - timedelta(hours=2)
        # s3 is recent

        removed = cleanup_expired_sessions(max_age=timedelta(hours=1))

        assert removed == 2
        assert s1.id not in _SESSIONS
        assert s2.id not in _SESSIONS
        assert s3.id in _SESSIONS


class TestGetSessionCount:
    """Tests for get_session_count function."""

    def test_returns_zero_when_empty(self):
        """Test that count is zero when no sessions exist."""
        assert get_session_count() == 0

    def test_returns_correct_count(self):
        """Test that count reflects number of sessions."""
        note = make_mock_note()
        create_session(note, "content1")
        create_session(note, "content2")
        create_session(note, "content3")

        assert get_session_count() == 3
