"""
Line-level diff computation for note editing.

Provides server-side diff computation that produces hunks suitable
for Remote DOM rendering. Uses Python's difflib for line-based comparison.
"""

import difflib
from dataclasses import dataclass
from typing import List, Literal


@dataclass
class DiffHunk:
    """
    A single hunk of a diff.

    Attributes:
        kind: Type of change - 'unchanged', 'added', 'removed', or 'modified'
        original: Original text (empty for 'added')
        proposed: Proposed text (empty for 'removed')
        id: Stable identifier for per-hunk operations (e.g., 'h1', 'h2')
        orig_start: 1-based start line in original text (None for pure inserts)
        orig_end: 1-based end line in original text (None for pure inserts)
        new_start: 1-based start line in proposed text (None for pure deletes)
        new_end: 1-based end line in proposed text (None for pure deletes)
        _orig_line_count: Internal field for accurate line counting (set by compute_line_diff)
        _new_line_count: Internal field for accurate line counting (set by compute_line_diff)
    """
    kind: Literal["unchanged", "added", "removed", "modified"]
    original: str
    proposed: str
    id: str | None = None
    orig_start: int | None = None
    orig_end: int | None = None
    new_start: int | None = None
    new_end: int | None = None
    # Internal fields for accurate line counting (avoids splitlines issues with trailing newlines)
    _orig_line_count: int | None = None
    _new_line_count: int | None = None


@dataclass
class HunkDecision:
    """
    User decision for a single diff hunk.
    
    Attributes:
        status: Decision status - 'pending', 'accepted', 'rejected', or 'revised'
        revised_text: Replacement text when status is 'revised'
    """
    status: Literal["pending", "accepted", "rejected", "revised"]
    revised_text: str | None = None


def compute_line_diff(
    original: str,
    proposed: str,
    context_lines: int = 3,
    max_unchanged_lines: int = 5,
    truncate_unchanged: bool = True,
) -> List[DiffHunk]:
    """
    Compute line-level diff between original and proposed content.

    Args:
        original: Original text content
        proposed: Proposed text content
        context_lines: Number of context lines around changes (unused for now)
        max_unchanged_lines: Maximum lines to show in unchanged hunks
        truncate_unchanged: If True, truncate long unchanged sections for display.
            Set to False when computing diffs for content reconstruction.

    Returns:
        List of DiffHunk objects representing the changes
    """
    orig_lines = original.splitlines(keepends=True)
    new_lines = proposed.splitlines(keepends=True)
    
    # Handle empty inputs
    if not orig_lines and not new_lines:
        return []
    
    if not orig_lines:
        # All new content - preserve whitespace for accurate preview
        return [DiffHunk(
            kind="added",
            original="",
            proposed=proposed,
        )]

    if not new_lines:
        # All removed - preserve whitespace for accurate preview
        return [DiffHunk(
            kind="removed",
            original=original,
            proposed="",
        )]
    
    matcher = difflib.SequenceMatcher(a=orig_lines, b=new_lines)
    hunks: List[DiffHunk] = []
    
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        # Join lines preserving their endings exactly.
        # We keep trailing newlines intact and concatenate segments directly in _join_segments,
        # which preserves the original file structure (including trailing newline or lack thereof).
        orig_text = "".join(orig_lines[i1:i2])
        new_text = "".join(new_lines[j1:j2])
        
        # Compute accurate line counts from difflib indices (not from stripped text)
        orig_line_count = i2 - i1
        new_line_count = j2 - j1

        if tag == "equal":
            # Unchanged section - optionally truncate if too long (for display only)
            # Always emit the hunk, even for blank-line-only sections (orig_text == "")
            # to preserve blank lines when applying decisions.
            display_text = orig_text
            if truncate_unchanged and orig_text:
                lines = orig_text.split("\n")
                if len(lines) > max_unchanged_lines:
                    # Show first and last few lines
                    half = max_unchanged_lines // 2
                    display_text = (
                        "\n".join(lines[:half]) +
                        f"\n... ({len(lines) - max_unchanged_lines} lines unchanged) ...\n" +
                        "\n".join(lines[-half:])
                    )
            hunks.append(DiffHunk(
                kind="unchanged",
                original=display_text,
                proposed=display_text,
                _orig_line_count=orig_line_count,
                _new_line_count=new_line_count,
            ))

        elif tag == "replace":
            # Modified section
            hunks.append(DiffHunk(
                kind="modified",
                original=orig_text,
                proposed=new_text,
                _orig_line_count=orig_line_count,
                _new_line_count=new_line_count,
            ))

        elif tag == "delete":
            # Removed section
            hunks.append(DiffHunk(
                kind="removed",
                original=orig_text,
                proposed="",
                _orig_line_count=orig_line_count,
                _new_line_count=0,
            ))

        elif tag == "insert":
            # Added section
            hunks.append(DiffHunk(
                kind="added",
                original="",
                proposed=new_text,
                _orig_line_count=0,
                _new_line_count=new_line_count,
            ))
    
    # Merge consecutive hunks of the same kind to reduce noise
    return _merge_consecutive_hunks(hunks)


def _merge_consecutive_hunks(hunks: List[DiffHunk]) -> List[DiffHunk]:
    """Merge consecutive hunks of the same kind."""
    if not hunks:
        return []

    merged: List[DiffHunk] = []
    current = hunks[0]

    for hunk in hunks[1:]:
        if hunk.kind == current.kind:
            # Merge into current, summing line counts
            current = DiffHunk(
                kind=current.kind,
                original=_join_texts(current.original, hunk.original),
                proposed=_join_texts(current.proposed, hunk.proposed),
                _orig_line_count=(current._orig_line_count or 0) + (hunk._orig_line_count or 0),
                _new_line_count=(current._new_line_count or 0) + (hunk._new_line_count or 0),
            )
        else:
            merged.append(current)
            current = hunk

    merged.append(current)
    return merged


def _join_texts(a: str, b: str) -> str:
    """Join two text strings by direct concatenation.

    Since texts preserve their original line endings, we concatenate directly.
    """
    return a + b


def count_changes(hunks: List[DiffHunk]) -> dict:
    """
    Count the number of changes by type.
    
    Returns:
        Dict with keys: added, removed, modified, unchanged
    """
    counts = {"added": 0, "removed": 0, "modified": 0, "unchanged": 0}
    for hunk in hunks:
        counts[hunk.kind] += 1
    return counts


def annotate_hunks_with_ids(hunks: List[DiffHunk]) -> List[DiffHunk]:
    """
    Annotate hunks with stable IDs and line ranges.

    Assigns sequential IDs ('h1', 'h2', ...) and computes orig_start/orig_end
    and new_start/new_end based on line counts.

    Args:
        hunks: List of DiffHunk objects from compute_line_diff

    Returns:
        New list of DiffHunk objects with id and line range fields populated
    """
    annotated: List[DiffHunk] = []
    orig_line = 1
    new_line = 1

    for i, hunk in enumerate(hunks):
        hunk_id = f"h{i + 1}"

        # Use stored line counts if available (accurate), fall back to splitlines (legacy)
        if hunk._orig_line_count is not None:
            orig_len = hunk._orig_line_count
        else:
            orig_len = len(hunk.original.splitlines()) if hunk.original else 0

        if hunk._new_line_count is not None:
            new_len = hunk._new_line_count
        else:
            new_len = len(hunk.proposed.splitlines()) if hunk.proposed else 0
        
        # For 'added' hunks, no original lines
        if hunk.kind == "added":
            orig_start = None
            orig_end = None
        else:
            orig_start = orig_line if orig_len > 0 else None
            orig_end = orig_line + orig_len - 1 if orig_len > 0 else None
        
        # For 'removed' hunks, no new lines
        if hunk.kind == "removed":
            new_start = None
            new_end = None
        else:
            new_start = new_line if new_len > 0 else None
            new_end = new_line + new_len - 1 if new_len > 0 else None
        
        annotated.append(DiffHunk(
            kind=hunk.kind,
            original=hunk.original,
            proposed=hunk.proposed,
            id=hunk_id,
            orig_start=orig_start,
            orig_end=orig_end,
            new_start=new_start,
            new_end=new_end,
        ))
        
        # Advance line counters
        if orig_len > 0:
            orig_line += orig_len
        if new_len > 0:
            new_line += new_len
    
    return annotated


def apply_hunk_decisions(
    hunks: List[DiffHunk],
    decisions: dict[str, HunkDecision],
) -> str:
    """
    Apply per-hunk decisions to produce the final merged content.
    
    This is a pure, stateless function. Callers must ensure no hunks
    are in 'pending' status before calling.
    
    Args:
        hunks: List of annotated DiffHunk objects (must have IDs)
        decisions: Map from hunk.id to HunkDecision
        
    Returns:
        The merged content string
        
    Raises:
        ValueError: If any changed hunk is still pending
    """
    segments: List[str] = []
    
    for hunk in hunks:
        hunk_id = hunk.id or ""
        decision = decisions.get(hunk_id)
        
        if hunk.kind == "unchanged":
            # Unchanged hunks always use original (same as proposed)
            segments.append(hunk.original)
        
        elif hunk.kind == "added":
            if decision is None or decision.status == "pending":
                raise ValueError(f"Hunk {hunk_id} is pending - cannot apply")
            elif decision.status == "accepted":
                segments.append(hunk.proposed)
            elif decision.status == "rejected":
                # Reject addition = don't include it
                pass
            elif decision.status == "revised":
                if decision.revised_text is not None:
                    segments.append(decision.revised_text)
        
        elif hunk.kind == "removed":
            if decision is None or decision.status == "pending":
                raise ValueError(f"Hunk {hunk_id} is pending - cannot apply")
            elif decision.status == "accepted":
                # Accept removal = don't include original
                pass
            elif decision.status == "rejected":
                # Reject removal = keep original
                segments.append(hunk.original)
            elif decision.status == "revised":
                if decision.revised_text is not None:
                    segments.append(decision.revised_text)
        
        elif hunk.kind == "modified":
            if decision is None or decision.status == "pending":
                raise ValueError(f"Hunk {hunk_id} is pending - cannot apply")
            elif decision.status == "accepted":
                segments.append(hunk.proposed)
            elif decision.status == "rejected":
                segments.append(hunk.original)
            elif decision.status == "revised":
                if decision.revised_text is not None:
                    segments.append(decision.revised_text)
    
    # Join segments with newlines, preserving structure
    return _join_segments(segments)


def _join_segments(segments: List[str]) -> str:
    """
    Join content segments by direct concatenation.

    Each segment preserves its original line endings from the source content.
    We concatenate directly (no separator) to preserve the exact structure,
    including whether the content had a trailing newline or not.

    Empty strings are preserved - they represent intentional blank lines.
    """
    # Filter out None values but keep empty strings (blank lines)
    result_parts = [s for s in segments if s is not None]

    return "".join(result_parts)
