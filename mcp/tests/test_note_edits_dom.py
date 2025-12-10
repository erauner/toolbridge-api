"""
Unit tests for note edit Remote DOM rendering.

Tests render_note_edit_diff_dom, render_note_edit_success_dom,
render_note_edit_discarded_dom, and render_note_edit_error_dom.
"""

import pytest
from unittest.mock import MagicMock

from toolbridge_mcp.ui.remote_dom.note_edits import (
    render_note_edit_diff_dom,
    render_note_edit_success_dom,
    render_note_edit_discarded_dom,
    render_note_edit_error_dom,
    _render_hunk_block,
    _render_diff_content,
    _render_diff_line,
    STATUS_BG,
    STATUS_BORDER,
)
from toolbridge_mcp.note_edit_sessions import NoteEditHunkState


def make_mock_note(
    uid: str = "note-123",
    version: int = 1,
    title: str = "Test Note",
    content: str = "test content",
    tags: list = None,
):
    """Create a mock Note object for testing."""
    note = MagicMock()
    note.uid = uid
    note.version = version
    note.payload = {
        "title": title,
        "content": content,
        "tags": tags or [],
    }
    return note


def make_hunk(
    id: str = "h1",
    kind: str = "modified",
    original: str = "old",
    proposed: str = "new",
    status: str = "pending",
    revised_text: str = None,
    orig_start: int = None,
    orig_end: int = None,
    new_start: int = None,
    new_end: int = None,
):
    """Create a NoteEditHunkState for testing."""
    return NoteEditHunkState(
        id=id,
        kind=kind,
        original=original,
        proposed=proposed,
        status=status,
        revised_text=revised_text,
        orig_start=orig_start,
        orig_end=orig_end,
        new_start=new_start,
        new_end=new_end,
    )


class TestRenderNoteEditDiffDom:
    """Tests for render_note_edit_diff_dom function."""

    def test_returns_column_root_node(self):
        """Test that the function returns a column as root."""
        note = make_mock_note()
        hunks = [make_hunk()]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        assert result["type"] == "column"
        assert "props" in result
        assert "children" in result

    def test_includes_header_with_icon(self):
        """Test that the result includes a header row with icon."""
        note = make_mock_note()
        hunks = [make_hunk()]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        # First child should be header row
        header_row = result["children"][0]
        assert header_row["type"] == "row"

        # Should contain an icon and text
        icons = [c for c in header_row["children"] if c.get("type") == "icon"]
        assert len(icons) >= 1

    def test_includes_note_title_and_version(self):
        """Test that note title and version are displayed."""
        note = make_mock_note(title="My Note", version=3)
        hunks = [make_hunk()]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        # Flatten to text content
        result_str = str(result)
        assert "My Note" in result_str
        assert "v3" in result_str

    def test_includes_summary_when_provided(self):
        """Test that summary is included when provided."""
        note = make_mock_note()
        hunks = [make_hunk()]

        result = render_note_edit_diff_dom(note, hunks, "edit-123", summary="Fixed typo")

        result_str = str(result)
        assert "Fixed typo" in result_str

    def test_shows_status_counts_for_changed_hunks(self):
        """Test that status counts are displayed."""
        note = make_mock_note()
        hunks = [
            make_hunk(id="h1", kind="modified", status="pending"),
            make_hunk(id="h2", kind="added", status="accepted"),
            make_hunk(id="h3", kind="removed", status="rejected"),
        ]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        result_str = str(result)
        assert "1 pending" in result_str
        assert "1 accepted" in result_str
        assert "1 rejected" in result_str

    def test_excludes_unchanged_from_status_counts(self):
        """Test that unchanged hunks are not counted in status."""
        note = make_mock_note()
        hunks = [
            make_hunk(id="h1", kind="unchanged", status="accepted"),
            make_hunk(id="h2", kind="modified", status="pending"),
        ]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        result_str = str(result)
        # Only the pending modified hunk should be counted
        assert "1 pending" in result_str
        # The unchanged hunk should not contribute to accepted count
        assert "accepted" not in result_str or "0 accepted" in result_str

    def test_includes_apply_and_discard_buttons(self):
        """Test that Apply and Discard buttons are present."""
        note = make_mock_note()
        hunks = [make_hunk()]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        result_str = str(result)
        assert "apply_note_edit" in result_str
        assert "discard_note_edit" in result_str

    def test_apply_button_disabled_when_hunks_pending(self):
        """Test that Apply button is disabled when hunks are pending."""
        note = make_mock_note()
        hunks = [make_hunk(status="pending")]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        # Find the apply button
        def find_button(node, tool_name):
            if isinstance(node, dict):
                if node.get("action", {}).get("payload", {}).get("toolName") == tool_name:
                    return node
                for child in node.get("children", []):
                    found = find_button(child, tool_name)
                    if found:
                        return found
            return None

        apply_button = find_button(result, "apply_note_edit")
        assert apply_button is not None
        assert apply_button["props"].get("enabled") is False

    def test_apply_button_enabled_when_all_resolved(self):
        """Test that Apply button is enabled when all hunks are resolved."""
        note = make_mock_note()
        hunks = [make_hunk(status="accepted")]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        def find_button(node, tool_name):
            if isinstance(node, dict):
                if node.get("action", {}).get("payload", {}).get("toolName") == tool_name:
                    return node
                for child in node.get("children", []):
                    found = find_button(child, tool_name)
                    if found:
                        return found
            return None

        apply_button = find_button(result, "apply_note_edit")
        assert apply_button is not None
        # enabled should be True (or not set, which means enabled)
        assert apply_button["props"].get("enabled") is not False

    def test_includes_edit_id_in_button_actions(self):
        """Test that edit_id is included in button action payloads."""
        note = make_mock_note()
        hunks = [make_hunk()]

        result = render_note_edit_diff_dom(note, hunks, "my-edit-id")

        result_str = str(result)
        assert "my-edit-id" in result_str

    def test_renders_each_hunk(self):
        """Test that each hunk gets a rendered block."""
        note = make_mock_note()
        hunks = [
            make_hunk(id="h1", kind="modified"),
            make_hunk(id="h2", kind="added"),
            make_hunk(id="h3", kind="removed"),
        ]

        result = render_note_edit_diff_dom(note, hunks, "edit-123")

        result_str = str(result)
        # All three hunk IDs should appear in action payloads
        assert "h1" in result_str
        assert "h2" in result_str
        assert "h3" in result_str


class TestRenderHunkBlock:
    """Tests for _render_hunk_block helper function."""

    def test_unchanged_hunk_returns_context_block(self):
        """Test that unchanged hunks render as abbreviated context."""
        hunk = make_hunk(kind="unchanged", original="unchanged line", status="accepted")

        result = _render_hunk_block("edit-123", hunk)

        assert result is not None
        assert result["type"] == "container"

    def test_unchanged_hunk_abbreviates_many_lines(self):
        """Test that many unchanged lines are abbreviated."""
        hunk = make_hunk(
            kind="unchanged",
            original="line 1\nline 2\nline 3\nline 4\nline 5",
            status="accepted",
        )

        result = _render_hunk_block("edit-123", hunk)

        result_str = str(result)
        assert "5 unchanged lines" in result_str

    def test_unchanged_hunk_with_empty_original_returns_none(self):
        """Test that empty unchanged hunks return None."""
        hunk = make_hunk(kind="unchanged", original="", status="accepted")

        result = _render_hunk_block("edit-123", hunk)

        assert result is None

    def test_pending_hunk_includes_action_buttons(self):
        """Test that pending hunks have Accept/Reject/Revise buttons."""
        hunk = make_hunk(kind="modified", status="pending")

        result = _render_hunk_block("edit-123", hunk)

        result_str = str(result)
        assert "accept_note_edit_hunk" in result_str
        assert "reject_note_edit_hunk" in result_str
        assert "revise_note_edit_hunk" in result_str

    def test_resolved_hunk_no_action_buttons(self):
        """Test that resolved hunks don't have action buttons."""
        hunk = make_hunk(kind="modified", status="accepted")

        result = _render_hunk_block("edit-123", hunk)

        result_str = str(result)
        # Should not have per-hunk action buttons
        assert "accept_note_edit_hunk" not in result_str

    def test_hunk_includes_status_chip(self):
        """Test that hunks include a status chip."""
        hunk = make_hunk(kind="modified", status="accepted")

        result = _render_hunk_block("edit-123", hunk)

        result_str = str(result)
        assert "Accepted" in result_str

    def test_hunk_includes_line_range(self):
        """Test that hunks display line range info."""
        hunk = make_hunk(
            kind="modified",
            orig_start=10,
            orig_end=15,
        )

        result = _render_hunk_block("edit-123", hunk)

        result_str = str(result)
        assert "lines 10-15" in result_str

    def test_single_line_hunk_shows_singular(self):
        """Test that single line shows 'line' not 'lines'."""
        hunk = make_hunk(
            kind="modified",
            orig_start=5,
            orig_end=5,
        )

        result = _render_hunk_block("edit-123", hunk)

        result_str = str(result)
        assert "line 5" in result_str
        assert "lines 5" not in result_str

    def test_hunk_has_status_colored_border(self):
        """Test that hunks have colored borders based on status."""
        hunk = make_hunk(kind="modified", status="accepted")

        result = _render_hunk_block("edit-123", hunk)

        assert result["props"].get("borderColor") == STATUS_BORDER["accepted"]

    def test_hunk_has_status_colored_background(self):
        """Test that resolved hunks have colored backgrounds."""
        hunk = make_hunk(kind="modified", status="accepted")

        result = _render_hunk_block("edit-123", hunk)

        assert result["props"].get("color") == STATUS_BG["accepted"]


class TestRenderDiffContent:
    """Tests for _render_diff_content helper function."""

    def test_added_shows_green_lines(self):
        """Test that added content renders with + prefix."""
        result = _render_diff_content("added", "", "new line")

        result_str = str(result)
        assert "+ " in result_str
        assert "new line" in result_str

    def test_removed_shows_red_lines(self):
        """Test that removed content renders with - prefix."""
        result = _render_diff_content("removed", "old line", "")

        result_str = str(result)
        assert "- " in result_str
        assert "old line" in result_str

    def test_modified_shows_both(self):
        """Test that modified shows removed then added lines."""
        result = _render_diff_content("modified", "old", "new")

        result_str = str(result)
        assert "- " in result_str and "old" in result_str
        assert "+ " in result_str and "new" in result_str

    def test_revised_text_overrides_proposed(self):
        """Test that revised_text is used instead of proposed."""
        result = _render_diff_content("modified", "old", "proposed", revised_text="custom")

        result_str = str(result)
        assert "custom" in result_str
        assert "proposed" not in result_str

    def test_removed_with_revised_shows_replacement(self):
        """Test that revised removal shows both deleted and replacement lines."""
        result = _render_diff_content("removed", "deleted line", "", revised_text="replacement")

        result_str = str(result)
        # Should show original as removed (red)
        assert "- " in result_str
        assert "deleted line" in result_str
        # Should also show revised_text as added (green)
        assert "+ " in result_str
        assert "replacement" in result_str

    def test_empty_returns_none(self):
        """Test that empty content returns None."""
        result = _render_diff_content("modified", "", "")

        # With no lines, should still return column (empty)
        # or None depending on implementation
        if result is not None:
            assert result.get("children", []) == []


class TestRenderDiffLine:
    """Tests for _render_diff_line helper function."""

    def test_added_line_has_plus_prefix(self):
        """Test that added lines have + prefix."""
        result = _render_diff_line("content", is_added=True)

        text = result["children"][0]["props"]["text"]
        assert text.startswith("+ ")

    def test_removed_line_has_minus_prefix(self):
        """Test that removed lines have - prefix."""
        result = _render_diff_line("content", is_added=False)

        text = result["children"][0]["props"]["text"]
        assert text.startswith("- ")


class TestRenderNoteEditSuccessDom:
    """Tests for render_note_edit_success_dom function."""

    def test_returns_column_root(self):
        """Test that success DOM is a column."""
        note = make_mock_note()

        result = render_note_edit_success_dom(note)

        assert result["type"] == "column"

    def test_shows_success_header(self):
        """Test that success message is displayed."""
        note = make_mock_note()

        result = render_note_edit_success_dom(note)

        result_str = str(result)
        assert "Changes applied" in result_str

    def test_shows_new_version(self):
        """Test that new version number is displayed."""
        note = make_mock_note(version=5)

        result = render_note_edit_success_dom(note)

        result_str = str(result)
        assert "v5" in result_str

    def test_shows_note_title(self):
        """Test that note title is displayed."""
        note = make_mock_note(title="Updated Note")

        result = render_note_edit_success_dom(note)

        result_str = str(result)
        assert "Updated Note" in result_str

    def test_shows_note_content(self):
        """Test that note content is displayed."""
        note = make_mock_note(content="New content here")

        result = render_note_edit_success_dom(note)

        result_str = str(result)
        assert "New content here" in result_str

    def test_shows_tags_when_present(self):
        """Test that tags are displayed when present."""
        note = make_mock_note(tags=["tag1", "tag2"])

        result = render_note_edit_success_dom(note)

        result_str = str(result)
        assert "tag1" in result_str
        assert "tag2" in result_str


class TestRenderNoteEditDiscardedDom:
    """Tests for render_note_edit_discarded_dom function."""

    def test_returns_column_root(self):
        """Test that discarded DOM is a column."""
        result = render_note_edit_discarded_dom("Test Note")

        assert result["type"] == "column"

    def test_shows_discarded_header(self):
        """Test that discarded message is displayed."""
        result = render_note_edit_discarded_dom("Test Note")

        result_str = str(result)
        assert "Changes discarded" in result_str

    def test_shows_note_title(self):
        """Test that note title is mentioned."""
        result = render_note_edit_discarded_dom("My Important Note")

        result_str = str(result)
        assert "My Important Note" in result_str


class TestRenderNoteEditErrorDom:
    """Tests for render_note_edit_error_dom function."""

    def test_returns_column_root(self):
        """Test that error DOM is a column."""
        result = render_note_edit_error_dom("Something went wrong")

        assert result["type"] == "column"

    def test_shows_error_header(self):
        """Test that error header is displayed."""
        result = render_note_edit_error_dom("Something went wrong")

        result_str = str(result)
        assert "Failed to apply" in result_str

    def test_shows_error_message(self):
        """Test that error message is displayed."""
        result = render_note_edit_error_dom("Version conflict detected")

        result_str = str(result)
        assert "Version conflict detected" in result_str

    def test_shows_retry_hint_when_note_uid_provided(self):
        """Test that retry hint is shown when note_uid is provided."""
        result = render_note_edit_error_dom("Error", note_uid="note-123")

        result_str = str(result)
        assert "edit_note_ui" in result_str

    def test_no_retry_hint_without_note_uid(self):
        """Test that retry hint is not shown without note_uid."""
        result = render_note_edit_error_dom("Error")

        result_str = str(result)
        # Should not have the full retry message
        children_count = len(result.get("children", []))
        assert children_count == 2  # Just header and error message
