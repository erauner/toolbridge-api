"""
Unit tests for diff utilities.

Tests compute_line_diff, annotate_hunks_with_ids, apply_hunk_decisions,
and related helpers.
"""

import pytest
from toolbridge_mcp.utils.diff import (
    DiffHunk,
    HunkDecision,
    compute_line_diff,
    annotate_hunks_with_ids,
    apply_hunk_decisions,
    count_changes,
)


class TestComputeLineDiff:
    """Tests for compute_line_diff function."""

    def test_empty_inputs_returns_empty_list(self):
        """Test that empty original and proposed returns empty list."""
        result = compute_line_diff("", "")
        assert result == []

    def test_all_new_content_returns_single_added_hunk(self):
        """Test that all new content returns a single 'added' hunk."""
        result = compute_line_diff("", "new content")
        assert len(result) == 1
        assert result[0].kind == "added"
        assert result[0].original == ""
        assert result[0].proposed == "new content"

    def test_all_removed_content_returns_single_removed_hunk(self):
        """Test that removing all content returns a single 'removed' hunk."""
        result = compute_line_diff("old content", "")
        assert len(result) == 1
        assert result[0].kind == "removed"
        assert result[0].original == "old content"
        assert result[0].proposed == ""

    def test_identical_content_returns_unchanged_hunk(self):
        """Test that identical content returns 'unchanged' hunk."""
        result = compute_line_diff("same", "same")
        assert len(result) == 1
        assert result[0].kind == "unchanged"
        assert result[0].original == "same"
        assert result[0].proposed == "same"

    def test_modified_line_returns_modified_hunk(self):
        """Test that a modified line returns 'modified' hunk."""
        result = compute_line_diff("old line", "new line")
        assert len(result) == 1
        assert result[0].kind == "modified"
        assert result[0].original == "old line"
        assert result[0].proposed == "new line"

    def test_multiline_diff_creates_multiple_hunks(self):
        """Test that multiline changes create appropriate hunks."""
        original = "line 1\nline 2\nline 3"
        proposed = "line 1\nmodified line 2\nline 3"
        result = compute_line_diff(original, proposed)

        # Should have unchanged, modified, unchanged
        kinds = [h.kind for h in result]
        assert "unchanged" in kinds
        assert "modified" in kinds

    def test_preserves_whitespace(self):
        """Test that whitespace is preserved in hunks."""
        original = "  indented content\n\ttabbed"
        proposed = "  indented content\n\ttabbed"
        result = compute_line_diff(original, proposed)

        assert len(result) == 1
        assert result[0].kind == "unchanged"
        assert "  indented" in result[0].original
        assert "\ttabbed" in result[0].original

    def test_truncate_unchanged_true_truncates_long_sections(self):
        """Test that long unchanged sections are truncated by default."""
        # Create content with more than 5 unchanged lines (default max_unchanged_lines)
        original = "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8"
        proposed = original  # Same content
        result = compute_line_diff(original, proposed, truncate_unchanged=True)

        assert len(result) == 1
        assert result[0].kind == "unchanged"
        # Should contain truncation marker
        assert "... (" in result[0].original
        assert "lines unchanged" in result[0].original

    def test_truncate_unchanged_false_preserves_full_content(self):
        """Test that truncate_unchanged=False preserves all content."""
        # Create content with more than 5 unchanged lines
        original = "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8"
        proposed = original
        result = compute_line_diff(original, proposed, truncate_unchanged=False)

        assert len(result) == 1
        assert result[0].kind == "unchanged"
        # Should NOT contain truncation marker
        assert "... (" not in result[0].original
        # Should contain all lines
        assert result[0].original == original

    def test_preserves_multiple_blank_lines_between_changes(self):
        """Test that multiple consecutive blank lines are preserved."""
        original = "A\n\n\nB"  # A, 2 blank lines, B
        proposed = "A\n\n\nC"  # A, 2 blank lines, C
        result = compute_line_diff(original, proposed, truncate_unchanged=False)

        # Should have unchanged (A + blank lines) and modified (B -> C)
        assert len(result) == 2
        assert result[0].kind == "unchanged"
        assert result[1].kind == "modified"

        # Unchanged section should preserve the blank lines
        # The text is "A\n\n" (A followed by 2 newlines, minus the trailing one)
        assert result[0].original == "A\n\n"
        assert result[0].proposed == "A\n\n"

        # Round-trip: applying all accepted should reproduce proposed
        annotated = annotate_hunks_with_ids(result)
        decisions = {h.id: HunkDecision(status="accepted") for h in annotated}
        final = apply_hunk_decisions(annotated, decisions)
        assert final == proposed


class TestAnnotateHunksWithIds:
    """Tests for annotate_hunks_with_ids function."""

    def test_assigns_sequential_ids(self):
        """Test that hunks get sequential IDs (h1, h2, h3, ...)."""
        hunks = [
            DiffHunk(kind="unchanged", original="a", proposed="a"),
            DiffHunk(kind="modified", original="b", proposed="c"),
            DiffHunk(kind="added", original="", proposed="d"),
        ]
        result = annotate_hunks_with_ids(hunks)

        assert result[0].id == "h1"
        assert result[1].id == "h2"
        assert result[2].id == "h3"

    def test_computes_line_ranges_for_unchanged(self):
        """Test line ranges for unchanged hunks."""
        hunks = [DiffHunk(kind="unchanged", original="line 1\nline 2", proposed="line 1\nline 2")]
        result = annotate_hunks_with_ids(hunks)

        assert result[0].orig_start == 1
        assert result[0].orig_end == 2
        assert result[0].new_start == 1
        assert result[0].new_end == 2

    def test_added_hunk_has_no_orig_range(self):
        """Test that added hunks have None for orig_start/orig_end."""
        hunks = [DiffHunk(kind="added", original="", proposed="new line")]
        result = annotate_hunks_with_ids(hunks)

        assert result[0].orig_start is None
        assert result[0].orig_end is None
        assert result[0].new_start == 1
        assert result[0].new_end == 1

    def test_removed_hunk_has_no_new_range(self):
        """Test that removed hunks have None for new_start/new_end."""
        hunks = [DiffHunk(kind="removed", original="old line", proposed="")]
        result = annotate_hunks_with_ids(hunks)

        assert result[0].orig_start == 1
        assert result[0].orig_end == 1
        assert result[0].new_start is None
        assert result[0].new_end is None

    def test_line_counters_accumulate_correctly(self):
        """Test that line counters accumulate across hunks."""
        hunks = [
            DiffHunk(kind="unchanged", original="line 1\nline 2", proposed="line 1\nline 2"),
            DiffHunk(kind="added", original="", proposed="new line 3"),
            DiffHunk(kind="unchanged", original="line 3", proposed="line 3"),
        ]
        result = annotate_hunks_with_ids(hunks)

        # First hunk: lines 1-2
        assert result[0].orig_start == 1
        assert result[0].orig_end == 2
        assert result[0].new_start == 1
        assert result[0].new_end == 2

        # Second hunk (added): no orig, new line 3
        assert result[1].orig_start is None
        assert result[1].new_start == 3
        assert result[1].new_end == 3

        # Third hunk: orig line 3, new line 4
        assert result[2].orig_start == 3
        assert result[2].orig_end == 3
        assert result[2].new_start == 4
        assert result[2].new_end == 4

    def test_empty_list_returns_empty_list(self):
        """Test that empty input returns empty output."""
        result = annotate_hunks_with_ids([])
        assert result == []

    def test_line_ranges_accurate_with_trailing_blank_lines(self):
        """Test that line ranges are accurate when hunks end with blank lines.

        Regression test: splitlines() undercounts lines when text ends with newlines.
        The _orig_line_count/_new_line_count fields from compute_line_diff fix this.
        """
        # A followed by 2 blank lines, then B->C change
        original = "A\n\n\nB"
        proposed = "A\n\n\nC"

        hunks = compute_line_diff(original, proposed, truncate_unchanged=False)
        result = annotate_hunks_with_ids(hunks)

        # Unchanged section: lines 1-3 (A on line 1, blank on line 2, blank on line 3)
        assert result[0].kind == "unchanged"
        assert result[0].orig_start == 1
        assert result[0].orig_end == 3
        assert result[0].new_start == 1
        assert result[0].new_end == 3

        # Modified section: line 4 (B -> C)
        assert result[1].kind == "modified"
        assert result[1].orig_start == 4
        assert result[1].orig_end == 4
        assert result[1].new_start == 4
        assert result[1].new_end == 4


class TestApplyHunkDecisions:
    """Tests for apply_hunk_decisions function."""

    def test_all_accepted_returns_proposed_content(self):
        """Test that accepting all hunks returns the proposed content."""
        hunks = [
            DiffHunk(kind="unchanged", original="line 1", proposed="line 1", id="h1"),
            DiffHunk(kind="modified", original="old", proposed="new", id="h2"),
        ]
        decisions = {
            "h1": HunkDecision(status="accepted"),
            "h2": HunkDecision(status="accepted"),
        }

        result = apply_hunk_decisions(hunks, decisions)
        assert "line 1" in result
        assert "new" in result
        assert "old" not in result

    def test_rejected_modified_keeps_original(self):
        """Test that rejecting a modified hunk keeps original content."""
        hunks = [
            DiffHunk(kind="modified", original="keep me", proposed="replace me", id="h1"),
        ]
        decisions = {"h1": HunkDecision(status="rejected")}

        result = apply_hunk_decisions(hunks, decisions)
        assert result == "keep me"

    def test_rejected_added_omits_content(self):
        """Test that rejecting an added hunk omits the content."""
        hunks = [
            DiffHunk(kind="unchanged", original="existing", proposed="existing", id="h1"),
            DiffHunk(kind="added", original="", proposed="should not appear", id="h2"),
        ]
        decisions = {
            "h1": HunkDecision(status="accepted"),
            "h2": HunkDecision(status="rejected"),
        }

        result = apply_hunk_decisions(hunks, decisions)
        assert result == "existing"
        assert "should not appear" not in result

    def test_rejected_removed_keeps_original(self):
        """Test that rejecting a removal keeps the original content."""
        hunks = [
            DiffHunk(kind="removed", original="keep this", proposed="", id="h1"),
        ]
        decisions = {"h1": HunkDecision(status="rejected")}

        result = apply_hunk_decisions(hunks, decisions)
        assert result == "keep this"

    def test_accepted_removed_omits_content(self):
        """Test that accepting a removal omits the content."""
        hunks = [
            DiffHunk(kind="unchanged", original="before", proposed="before", id="h1"),
            DiffHunk(kind="removed", original="delete me", proposed="", id="h2"),
            DiffHunk(kind="unchanged", original="after", proposed="after", id="h3"),
        ]
        decisions = {
            "h1": HunkDecision(status="accepted"),
            "h2": HunkDecision(status="accepted"),
            "h3": HunkDecision(status="accepted"),
        }

        result = apply_hunk_decisions(hunks, decisions)
        assert "before" in result
        assert "after" in result
        assert "delete me" not in result

    def test_revised_uses_revised_text(self):
        """Test that revised hunks use the revised_text."""
        hunks = [
            DiffHunk(kind="modified", original="old", proposed="new", id="h1"),
        ]
        decisions = {
            "h1": HunkDecision(status="revised", revised_text="custom replacement"),
        }

        result = apply_hunk_decisions(hunks, decisions)
        assert result == "custom replacement"

    def test_pending_hunk_raises_error(self):
        """Test that pending hunks raise ValueError."""
        hunks = [
            DiffHunk(kind="modified", original="old", proposed="new", id="h1"),
        ]
        decisions = {"h1": HunkDecision(status="pending")}

        with pytest.raises(ValueError, match="pending"):
            apply_hunk_decisions(hunks, decisions)

    def test_missing_decision_for_changed_hunk_raises_error(self):
        """Test that missing decision for changed hunk raises ValueError."""
        hunks = [
            DiffHunk(kind="modified", original="old", proposed="new", id="h1"),
        ]
        decisions = {}  # No decision provided

        with pytest.raises(ValueError, match="pending"):
            apply_hunk_decisions(hunks, decisions)

    def test_unchanged_hunks_always_use_original(self):
        """Test that unchanged hunks always use original content."""
        hunks = [
            DiffHunk(kind="unchanged", original="constant", proposed="constant", id="h1"),
        ]
        # No decision needed for unchanged
        decisions = {}

        result = apply_hunk_decisions(hunks, decisions)
        assert result == "constant"

    def test_preserves_blank_line_hunks(self):
        """Test that blank-line-only hunks are preserved when accepted."""
        hunks = [
            DiffHunk(kind="unchanged", original="line 1", proposed="line 1", id="h1"),
            DiffHunk(kind="added", original="", proposed="", id="h2"),  # Blank line
            DiffHunk(kind="unchanged", original="line 3", proposed="line 3", id="h3"),
        ]
        decisions = {
            "h2": HunkDecision(status="accepted"),
        }

        result = apply_hunk_decisions(hunks, decisions)
        # Should have blank line between line 1 and line 3
        assert result == "line 1\n\nline 3"

    def test_rejected_blank_line_removal_keeps_blank(self):
        """Test that rejecting removal of blank line keeps the blank."""
        hunks = [
            DiffHunk(kind="unchanged", original="line 1", proposed="line 1", id="h1"),
            DiffHunk(kind="removed", original="", proposed="", id="h2"),  # Blank line being removed
            DiffHunk(kind="unchanged", original="line 3", proposed="line 3", id="h3"),
        ]
        decisions = {
            "h2": HunkDecision(status="rejected"),  # Keep the blank line
        }

        result = apply_hunk_decisions(hunks, decisions)
        # Should have blank line preserved
        assert result == "line 1\n\nline 3"


class TestCountChanges:
    """Tests for count_changes function."""

    def test_counts_all_types(self):
        """Test that all hunk types are counted correctly."""
        hunks = [
            DiffHunk(kind="unchanged", original="a", proposed="a"),
            DiffHunk(kind="unchanged", original="b", proposed="b"),
            DiffHunk(kind="added", original="", proposed="c"),
            DiffHunk(kind="removed", original="d", proposed=""),
            DiffHunk(kind="modified", original="e", proposed="f"),
            DiffHunk(kind="modified", original="g", proposed="h"),
        ]

        result = count_changes(hunks)

        assert result["unchanged"] == 2
        assert result["added"] == 1
        assert result["removed"] == 1
        assert result["modified"] == 2

    def test_empty_list_returns_zeros(self):
        """Test that empty list returns all zeros."""
        result = count_changes([])

        assert result["unchanged"] == 0
        assert result["added"] == 0
        assert result["removed"] == 0
        assert result["modified"] == 0
