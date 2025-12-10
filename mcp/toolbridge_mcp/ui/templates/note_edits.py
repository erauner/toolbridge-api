"""
HTML templates for Note Edit UI resources.

Provides HTML rendering for diff preview, success, discarded, and error states
matching the Remote DOM templates in ui/remote_dom/note_edits.py.
"""

from typing import Iterable, TYPE_CHECKING
from html import escape

if TYPE_CHECKING:
    from toolbridge_mcp.tools.notes import Note
    from toolbridge_mcp.note_edit_sessions import NoteEditHunkState


# Status colors matching remote_dom/note_edits.py
STATUS_BG = {
    "pending": "transparent",
    "accepted": "#1c4428",  # Dark green
    "rejected": "#5c1a1b",  # Dark red
    "revised": "#2d3a4d",   # Dark blue
}

STATUS_BORDER = {
    "pending": "#6e7681",   # Gray
    "accepted": "#3fb950",  # Green
    "rejected": "#f85149",  # Red
    "revised": "#58a6ff",   # Blue
}

# GitHub-style diff colors
DIFF_ADDED_BG = "#1c4428"      # Dark green background
DIFF_ADDED_TEXT = "#3fb950"    # Bright green text
DIFF_REMOVED_BG = "#5c1a1b"    # Dark red background
DIFF_REMOVED_TEXT = "#f85149"  # Bright red text
DIFF_CONTEXT_BG = "#21262d"    # Dark gray background
DIFF_CONTEXT_TEXT = "#8b949e"  # Gray text


def render_note_edit_diff_html(
    note: "Note",
    hunks: Iterable["NoteEditHunkState"],
    edit_id: str,
    summary: str | None = None,
) -> str:
    """
    Render HTML for note edit diff preview with per-hunk actions.

    Args:
        note: The current note being edited
        hunks: List of NoteEditHunkState from the session
        edit_id: The edit session ID for action payloads
        summary: Optional summary of the changes

    Returns:
        HTML string with the diff preview UI
    """
    hunks_list = list(hunks)
    title = escape((note.payload.get("title") or "Untitled note").strip())
    edit_id_escaped = escape(edit_id)

    # Calculate status counts (excluding unchanged)
    status_counts = {"pending": 0, "accepted": 0, "rejected": 0, "revised": 0}
    for h in hunks_list:
        if h.kind != "unchanged":
            status_counts[h.status] = status_counts.get(h.status, 0) + 1

    total_changes = sum(status_counts.values())
    has_pending = status_counts["pending"] > 0

    # Build status chips HTML
    status_chips_html = ""
    if total_changes > 0:
        chips = []
        if status_counts["pending"] > 0:
            chips.append(f'<span class="status-chip status-pending">{status_counts["pending"]} pending</span>')
        if status_counts["accepted"] > 0:
            chips.append(f'<span class="status-chip status-accepted">✓ {status_counts["accepted"]} accepted</span>')
        if status_counts["rejected"] > 0:
            chips.append(f'<span class="status-chip status-rejected">✗ {status_counts["rejected"]} rejected</span>')
        if status_counts["revised"] > 0:
            chips.append(f'<span class="status-chip status-revised">✎ {status_counts["revised"]} revised</span>')
        status_chips_html = '<div class="status-chips">' + "".join(chips) + "</div>"

    # Build hunks HTML
    hunks_html = ""
    for hunk in hunks_list:
        hunk_html = _render_hunk_block_html(edit_id_escaped, hunk)
        if hunk_html:
            hunks_html += hunk_html

    # Summary text
    summary_html = ""
    if summary:
        summary_html = f'<p class="summary-text">{escape(summary)}</p>'

    # Apply button label
    apply_label = "Apply changes" if not has_pending else f"Resolve {status_counts['pending']} pending to apply"
    apply_disabled = 'disabled' if has_pending else ''
    apply_class = 'btn-disabled' if has_pending else ''

    return f"""
    <html>
    <head>
        <style>
            * {{ box-sizing: border-box; }}
            html, body {{
                margin: 0;
                padding: 0;
                min-height: 100vh;
                width: 100%;
            }}
            body {{
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'SF Pro', sans-serif;
                background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
                font-size: 16px;
                color: #e0e0e0;
                padding: 24px;
            }}
            .header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 8px;
            }}
            .header-icon {{
                font-size: 24px;
            }}
            h1 {{
                margin: 0;
                color: #ffffff;
                font-size: 24px;
                font-weight: 600;
            }}
            .subtitle {{
                color: #8b949e;
                font-size: 14px;
                margin-bottom: 16px;
            }}
            .summary-text {{
                color: #c9d1d9;
                font-size: 15px;
                margin-bottom: 16px;
                line-height: 1.5;
            }}
            .status-chips {{
                display: flex;
                flex-wrap: wrap;
                gap: 8px;
                margin-bottom: 20px;
            }}
            .status-chip {{
                padding: 4px 12px;
                border-radius: 16px;
                font-size: 13px;
                font-weight: 500;
                border: 1px solid;
            }}
            .status-pending {{
                background: transparent;
                border-color: #6e7681;
                color: #8b949e;
            }}
            .status-accepted {{
                background: rgba(63, 185, 80, 0.15);
                border-color: #3fb950;
                color: #3fb950;
            }}
            .status-rejected {{
                background: rgba(248, 81, 73, 0.15);
                border-color: #f85149;
                color: #f85149;
            }}
            .status-revised {{
                background: rgba(88, 166, 255, 0.15);
                border-color: #58a6ff;
                color: #58a6ff;
            }}
            .hunk-card {{
                margin-bottom: 16px;
                border-radius: 8px;
                border: 1px solid;
                overflow: hidden;
            }}
            .hunk-card.status-pending {{
                border-color: #6e7681;
                background: rgba(255, 255, 255, 0.03);
            }}
            .hunk-card.status-accepted {{
                border-color: #3fb950;
                background: #1c4428;
            }}
            .hunk-card.status-rejected {{
                border-color: #f85149;
                background: #5c1a1b;
            }}
            .hunk-card.status-revised {{
                border-color: #58a6ff;
                background: #2d3a4d;
            }}
            .hunk-header {{
                padding: 12px 16px;
                display: flex;
                align-items: center;
                gap: 12px;
                border-bottom: 1px solid rgba(255, 255, 255, 0.1);
            }}
            .hunk-status-badge {{
                padding: 2px 8px;
                border-radius: 4px;
                font-size: 12px;
                font-weight: 600;
                text-transform: capitalize;
            }}
            .hunk-status-badge.pending {{
                background: #6e7681;
                color: #ffffff;
            }}
            .hunk-status-badge.accepted {{
                background: #3fb950;
                color: #ffffff;
            }}
            .hunk-status-badge.rejected {{
                background: #f85149;
                color: #ffffff;
            }}
            .hunk-status-badge.revised {{
                background: #58a6ff;
                color: #ffffff;
            }}
            .hunk-info {{
                color: #8b949e;
                font-size: 14px;
            }}
            .hunk-content {{
                padding: 0;
            }}
            .diff-line {{
                padding: 4px 12px;
                font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Fira Mono', monospace;
                font-size: 13px;
                line-height: 1.5;
                white-space: pre-wrap;
                word-break: break-all;
            }}
            .diff-line.added {{
                background: {DIFF_ADDED_BG};
                color: {DIFF_ADDED_TEXT};
            }}
            .diff-line.removed {{
                background: {DIFF_REMOVED_BG};
                color: {DIFF_REMOVED_TEXT};
            }}
            .diff-line.context {{
                background: {DIFF_CONTEXT_BG};
                color: {DIFF_CONTEXT_TEXT};
            }}
            .hunk-actions {{
                padding: 12px 16px;
                display: flex;
                justify-content: flex-end;
                gap: 8px;
                border-top: 1px solid rgba(255, 255, 255, 0.1);
            }}
            .unchanged-block {{
                padding: 12px 16px;
                background: {DIFF_CONTEXT_BG};
                border-radius: 8px;
                margin-bottom: 16px;
            }}
            .unchanged-text {{
                color: {DIFF_CONTEXT_TEXT};
                font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Fira Mono', monospace;
                font-size: 13px;
                white-space: pre-wrap;
            }}
            .actions-row {{
                display: flex;
                justify-content: flex-end;
                gap: 12px;
                margin-top: 24px;
                padding-top: 16px;
                border-top: 1px solid rgba(255, 255, 255, 0.1);
            }}
            .btn {{
                padding: 10px 20px;
                border: none;
                border-radius: 8px;
                font-size: 14px;
                font-weight: 600;
                cursor: pointer;
                transition: all 0.2s ease;
                display: inline-flex;
                align-items: center;
                gap: 6px;
            }}
            .btn:hover:not(:disabled) {{
                transform: translateY(-1px);
                box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            }}
            .btn:active:not(:disabled) {{
                transform: translateY(0);
            }}
            .btn-primary {{
                background: #238636;
                color: white;
            }}
            .btn-primary:hover:not(:disabled) {{
                background: #2ea043;
            }}
            .btn-secondary {{
                background: #373e47;
                color: #c9d1d9;
            }}
            .btn-secondary:hover:not(:disabled) {{
                background: #444c56;
            }}
            .btn-danger {{
                background: #da3633;
                color: white;
            }}
            .btn-danger:hover:not(:disabled) {{
                background: #f85149;
            }}
            .btn-text {{
                background: transparent;
                color: #8b949e;
            }}
            .btn-text:hover:not(:disabled) {{
                background: rgba(255, 255, 255, 0.1);
                color: #c9d1d9;
            }}
            .btn-disabled {{
                opacity: 0.5;
                cursor: not-allowed;
            }}
            .btn-sm {{
                padding: 6px 12px;
                font-size: 12px;
            }}
        </style>
    </head>
    <body>
        <div class="header">
            <span class="header-icon">✏️</span>
            <h1>Proposed changes</h1>
        </div>
        <p class="subtitle">{title} (v{note.version})</p>
        {summary_html}
        {status_chips_html}

        <div class="hunks-container">
            {hunks_html}
        </div>

        <div class="actions-row">
            <button class="btn btn-text" onclick="discardEdit()">✗ Discard all</button>
            <button class="btn btn-primary {apply_class}" onclick="applyChanges()" {apply_disabled}>✓ {apply_label}</button>
        </div>

        <script>
            const EDIT_ID = "{edit_id_escaped}";

            // Host-adaptive action helper - works with both ChatGPT Apps and MCP-UI hosts
            // ChatGPT Apps: uses window.openai.callTool (Apps SDK)
            // MCP-UI hosts (ToolBridge, Nanobot, Goose): uses window.parent.postMessage
            function callTool(toolName, params) {{
                const finalParams = params || {{}};

                // ChatGPT Apps environment (Apps SDK)
                if (window.openai && typeof window.openai.callTool === 'function') {{
                    window.openai.callTool(toolName, finalParams);
                    return;
                }}

                // MCP-UI hosts (ToolBridge Flutter, Nanobot, Goose, etc.)
                window.parent.postMessage({{
                    type: 'tool',
                    payload: {{
                        toolName: toolName,
                        params: finalParams
                    }}
                }}, '*');
            }}

            function applyChanges() {{
                callTool('apply_note_edit', {{
                    edit_id: EDIT_ID,
                    ui_format: 'html'
                }});
            }}

            function discardEdit() {{
                callTool('discard_note_edit', {{
                    edit_id: EDIT_ID,
                    ui_format: 'html'
                }});
            }}

            function acceptHunk(hunkId) {{
                callTool('accept_note_edit_hunk', {{
                    edit_id: EDIT_ID,
                    hunk_id: hunkId,
                    ui_format: 'html'
                }});
            }}

            function rejectHunk(hunkId) {{
                callTool('reject_note_edit_hunk', {{
                    edit_id: EDIT_ID,
                    hunk_id: hunkId,
                    ui_format: 'html'
                }});
            }}

            function reviseHunk(hunkId) {{
                const revised = window.prompt('Enter replacement text for this change');
                if (revised === null) return;
                callTool('revise_note_edit_hunk', {{
                    edit_id: EDIT_ID,
                    hunk_id: hunkId,
                    revised_text: revised,
                    ui_format: 'html'
                }});
            }}
        </script>
    </body>
    </html>
    """


def _render_hunk_block_html(edit_id: str, hunk: "NoteEditHunkState") -> str:
    """Render a single hunk as an HTML block."""
    if hunk.kind == "unchanged":
        # For unchanged hunks, show abbreviated context
        if not hunk.original:
            return ""

        lines = hunk.original.split('\n')
        if len(lines) > 3:
            context_text = f"... ({len(lines)} unchanged lines) ..."
        else:
            context_text = escape(hunk.original)

        return f'<div class="unchanged-block"><span class="unchanged-text">{context_text}</span></div>'

    # Changed hunk - build card with header, diff, and actions
    hunk_id = escape(hunk.id)
    status = hunk.status

    # Kind + line range
    kind_labels = {
        "added": "Added",
        "removed": "Removed",
        "modified": "Modified",
    }
    kind_label = kind_labels.get(hunk.kind, hunk.kind.capitalize())

    line_info = ""
    if hunk.orig_start is not None and hunk.orig_end is not None:
        if hunk.orig_start == hunk.orig_end:
            line_info = f"line {hunk.orig_start}"
        else:
            line_info = f"lines {hunk.orig_start}-{hunk.orig_end}"
    elif hunk.new_start is not None and hunk.new_end is not None:
        if hunk.new_start == hunk.new_end:
            line_info = f"line {hunk.new_start}"
        else:
            line_info = f"lines {hunk.new_start}-{hunk.new_end}"

    header_text = kind_label
    if line_info:
        header_text += f" ({line_info})"

    # Build diff content HTML
    diff_html = _render_diff_content_html(hunk.kind, hunk.original, hunk.proposed, hunk.revised_text)

    # Actions row (only for pending hunks)
    actions_html = ""
    if status == "pending":
        actions_html = f"""
        <div class="hunk-actions">
            <button class="btn btn-sm btn-text" onclick="rejectHunk('{hunk_id}')">✗ Reject</button>
            <button class="btn btn-sm btn-secondary" onclick="reviseHunk('{hunk_id}')">✎ Revise...</button>
            <button class="btn btn-sm btn-primary" onclick="acceptHunk('{hunk_id}')">✓ Accept</button>
        </div>
        """

    return f"""
    <div class="hunk-card status-{status}" data-hunk-id="{hunk_id}">
        <div class="hunk-header">
            <span class="hunk-status-badge {status}">{status}</span>
            <span class="hunk-info">{header_text}</span>
        </div>
        <div class="hunk-content">
            {diff_html}
        </div>
        {actions_html}
    </div>
    """


def _render_diff_content_html(
    kind: str,
    original: str,
    proposed: str,
    revised_text: str | None = None,
) -> str:
    """Render the diff content for a hunk."""
    lines_html = ""

    # Use revised_text if available
    display_proposed = revised_text if revised_text is not None else proposed

    if kind == "removed":
        # Show removed lines
        for line in original.split('\n'):
            lines_html += f'<div class="diff-line removed">- {escape(line)}</div>'
        # If revised, also show the replacement text
        if revised_text:
            for line in revised_text.split('\n'):
                lines_html += f'<div class="diff-line added">+ {escape(line)}</div>'

    elif kind == "added":
        # Only show added lines
        for line in display_proposed.split('\n'):
            lines_html += f'<div class="diff-line added">+ {escape(line)}</div>'

    elif kind == "modified":
        # Show removed then added
        if original:
            for line in original.split('\n'):
                lines_html += f'<div class="diff-line removed">- {escape(line)}</div>'
        if display_proposed:
            for line in display_proposed.split('\n'):
                lines_html += f'<div class="diff-line added">+ {escape(line)}</div>'

    return lines_html


def render_note_edit_success_html(note: "Note") -> str:
    """
    Render HTML for successful note edit confirmation.

    Args:
        note: The updated note after applying changes

    Returns:
        HTML string with success confirmation
    """
    title = escape((note.payload.get("title") or "Untitled note").strip())
    content = escape((note.payload.get("content") or "").strip())
    tags = note.payload.get("tags") or []

    tags_html = ""
    if tags:
        tags_html = '<div class="tags">' + "".join(
            f'<span class="tag">{escape(str(tag))}</span>' for tag in tags[:5]
        ) + "</div>"

    return f"""
    <html>
    <head>
        <style>
            * {{ box-sizing: border-box; }}
            html, body {{
                margin: 0;
                padding: 0;
                min-height: 100vh;
                width: 100%;
            }}
            body {{
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'SF Pro', sans-serif;
                background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
                font-size: 16px;
                color: #e0e0e0;
                padding: 24px;
            }}
            .header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 8px;
            }}
            .header-icon {{
                font-size: 24px;
                color: #3fb950;
            }}
            h1 {{
                margin: 0;
                color: #ffffff;
                font-size: 24px;
                font-weight: 600;
            }}
            .subtitle {{
                color: #8b949e;
                font-size: 14px;
                margin-bottom: 20px;
            }}
            .note-card {{
                background: rgba(255, 255, 255, 0.05);
                border: 1px solid rgba(255, 255, 255, 0.1);
                border-radius: 12px;
                padding: 20px;
            }}
            .note-title {{
                font-size: 20px;
                font-weight: 600;
                color: #ffffff;
                margin-bottom: 12px;
            }}
            .tags {{
                margin-bottom: 16px;
                display: flex;
                flex-wrap: wrap;
                gap: 8px;
            }}
            .tag {{
                background: rgba(88, 166, 255, 0.15);
                color: #58a6ff;
                padding: 4px 10px;
                border-radius: 12px;
                font-size: 12px;
            }}
            .divider {{
                height: 1px;
                background: rgba(255, 255, 255, 0.1);
                margin: 16px 0;
            }}
            .note-content {{
                color: #c9d1d9;
                line-height: 1.6;
                white-space: pre-wrap;
            }}
        </style>
    </head>
    <body>
        <div class="header">
            <span class="header-icon">✓</span>
            <h1>Changes applied</h1>
        </div>
        <p class="subtitle">Updated to v{note.version}</p>

        <div class="note-card">
            <div class="note-title">{title}</div>
            {tags_html}
            <div class="divider"></div>
            <div class="note-content">{content}</div>
        </div>
    </body>
    </html>
    """


def render_note_edit_discarded_html(title: str) -> str:
    """
    Render HTML for discarded note edit confirmation.

    Args:
        title: The note title

    Returns:
        HTML string with discard confirmation
    """
    return f"""
    <html>
    <head>
        <style>
            * {{ box-sizing: border-box; }}
            html, body {{
                margin: 0;
                padding: 0;
                min-height: 100vh;
                width: 100%;
            }}
            body {{
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'SF Pro', sans-serif;
                background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
                font-size: 16px;
                color: #e0e0e0;
                padding: 24px;
            }}
            .header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 12px;
            }}
            .header-icon {{
                font-size: 24px;
                color: #8b949e;
            }}
            h1 {{
                margin: 0;
                color: #ffffff;
                font-size: 24px;
                font-weight: 600;
            }}
            .message {{
                color: #8b949e;
                font-size: 15px;
                line-height: 1.5;
            }}
        </style>
    </head>
    <body>
        <div class="header">
            <span class="header-icon">✗</span>
            <h1>Changes discarded</h1>
        </div>
        <p class="message">Pending edits for '{escape(title)}' have been discarded.</p>
    </body>
    </html>
    """


def render_note_edit_error_html(
    error_message: str,
    note_uid: str | None = None,
) -> str:
    """
    Render HTML for note edit error.

    Args:
        error_message: The error message to display
        note_uid: Optional note UID for retry suggestion

    Returns:
        HTML string with error display
    """
    retry_hint = ""
    if note_uid:
        retry_hint = """
        <p class="retry-hint">The note may have been modified. Please re-run edit_note_ui to create a fresh diff.</p>
        """

    return f"""
    <html>
    <head>
        <style>
            * {{ box-sizing: border-box; }}
            html, body {{
                margin: 0;
                padding: 0;
                min-height: 100vh;
                width: 100%;
            }}
            body {{
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'SF Pro', sans-serif;
                background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
                font-size: 16px;
                color: #e0e0e0;
                padding: 24px;
            }}
            .error-container {{
                background: rgba(248, 81, 73, 0.1);
                border: 1px solid #f85149;
                border-radius: 12px;
                padding: 20px;
            }}
            .header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 12px;
            }}
            .header-icon {{
                font-size: 24px;
                color: #f85149;
            }}
            h1 {{
                margin: 0;
                color: #ffffff;
                font-size: 20px;
                font-weight: 600;
            }}
            .error-message {{
                color: #ffa198;
                font-size: 15px;
                line-height: 1.5;
                margin: 0;
            }}
            .retry-hint {{
                color: #8b949e;
                font-size: 13px;
                margin-top: 12px;
                margin-bottom: 0;
            }}
        </style>
    </head>
    <body>
        <div class="error-container">
            <div class="header">
                <span class="header-icon">⚠</span>
                <h1>Failed to apply changes</h1>
            </div>
            <p class="error-message">{escape(error_message)}</p>
            {retry_hint}
        </div>
    </body>
    </html>
    """
