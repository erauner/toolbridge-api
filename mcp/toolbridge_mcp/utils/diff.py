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
    """
    kind: Literal["unchanged", "added", "removed", "modified"]
    original: str
    proposed: str


def compute_line_diff(
    original: str,
    proposed: str,
    context_lines: int = 3,
    max_unchanged_lines: int = 5,
) -> List[DiffHunk]:
    """
    Compute line-level diff between original and proposed content.
    
    Args:
        original: Original text content
        proposed: Proposed text content
        context_lines: Number of context lines around changes (unused for now)
        max_unchanged_lines: Maximum lines to show in unchanged hunks
        
    Returns:
        List of DiffHunk objects representing the changes
    """
    orig_lines = original.splitlines(keepends=True)
    new_lines = proposed.splitlines(keepends=True)
    
    # Handle empty inputs
    if not orig_lines and not new_lines:
        return []
    
    if not orig_lines:
        # All new content
        return [DiffHunk(
            kind="added",
            original="",
            proposed=proposed.strip(),
        )]
    
    if not new_lines:
        # All removed
        return [DiffHunk(
            kind="removed",
            original=original.strip(),
            proposed="",
        )]
    
    matcher = difflib.SequenceMatcher(a=orig_lines, b=new_lines)
    hunks: List[DiffHunk] = []
    
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        orig_text = "".join(orig_lines[i1:i2]).strip()
        new_text = "".join(new_lines[j1:j2]).strip()
        
        if tag == "equal":
            # Unchanged section - truncate if too long
            if orig_text:
                lines = orig_text.split("\n")
                if len(lines) > max_unchanged_lines:
                    # Show first and last few lines
                    half = max_unchanged_lines // 2
                    truncated = (
                        "\n".join(lines[:half]) +
                        f"\n... ({len(lines) - max_unchanged_lines} lines unchanged) ...\n" +
                        "\n".join(lines[-half:])
                    )
                    hunks.append(DiffHunk(
                        kind="unchanged",
                        original=truncated,
                        proposed=truncated,
                    ))
                else:
                    hunks.append(DiffHunk(
                        kind="unchanged",
                        original=orig_text,
                        proposed=orig_text,
                    ))
        
        elif tag == "replace":
            # Modified section
            hunks.append(DiffHunk(
                kind="modified",
                original=orig_text,
                proposed=new_text,
            ))
        
        elif tag == "delete":
            # Removed section
            hunks.append(DiffHunk(
                kind="removed",
                original=orig_text,
                proposed="",
            ))
        
        elif tag == "insert":
            # Added section
            hunks.append(DiffHunk(
                kind="added",
                original="",
                proposed=new_text,
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
            # Merge into current
            current = DiffHunk(
                kind=current.kind,
                original=_join_texts(current.original, hunk.original),
                proposed=_join_texts(current.proposed, hunk.proposed),
            )
        else:
            merged.append(current)
            current = hunk
    
    merged.append(current)
    return merged


def _join_texts(a: str, b: str) -> str:
    """Join two text strings with a newline if both non-empty."""
    if not a:
        return b
    if not b:
        return a
    return f"{a}\n{b}"


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
