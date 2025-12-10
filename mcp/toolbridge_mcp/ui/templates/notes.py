"""
HTML templates for Note UI resources.

Converts Note models into HTML for MCP-UI rendering.
NOTE: These are minimal stub templates. A future ticket will add:
- Proper CSS styling
- Interactive elements (edit, delete buttons)
- MCP-UI action handlers (postMessage events)
"""

from typing import Iterable, TYPE_CHECKING
from html import escape

if TYPE_CHECKING:
    from toolbridge_mcp.tools.notes import Note


def render_notes_list_html(
    notes: Iterable["Note"],
    limit: int = 20,
    include_deleted: bool = False,
) -> str:
    """
    Render an HTML list of notes.

    Args:
        notes: Iterable of Note objects to display
        limit: Current list limit (passed to action tools to preserve context)
        include_deleted: Current include_deleted setting (passed to action tools)

    Returns:
        HTML string with a styled list of notes
    """
    notes_list = list(notes)

    if not notes_list:
        return """
        <html>
        <head>
            <style>
                body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; }
                .empty { color: #666; font-style: italic; }
            </style>
        </head>
        <body>
            <h2>📝 Notes</h2>
            <p class="empty">No notes found.</p>
        </body>
        </html>
        """

    items_html = ""
    for note in notes_list:
        title = escape(note.payload.get("title") or "Untitled")
        content_raw = note.payload.get("content") or ""
        content_preview = escape(content_raw[:100])
        if len(content_raw) > 100:
            content_preview += "..."
        uid = escape(note.uid)

        items_html += f"""
        <li class="note-item" data-uid="{uid}">
            <div class="note-title">{title}</div>
            <div class="note-preview">{content_preview}</div>
            <div class="note-meta">UID: {uid[:8]}... | v{note.version}</div>
            <div class="note-actions">
                <button class="btn btn-view" onclick="viewNote('{uid}')">👁 View</button>
                <button class="btn btn-delete" onclick="deleteNote('{uid}')">🗑️ Delete</button>
            </div>
        </li>
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
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                background: linear-gradient(135deg, #1565c0 0%, #0d47a1 50%, #1a237e 100%);
                font-size: 18px;
                color: #e0e0e0;
                padding: 24px;
            }}
            h2 {{
                margin-top: 0;
                color: #ffeb3b;
                font-size: 32px;
                margin-bottom: 8px;
                text-shadow: 2px 2px 4px rgba(0,0,0,0.5);
            }}
            .notes-list {{ list-style: none; padding: 0; margin: 0; }}
            .note-item {{
                padding: 20px 24px;
                margin-bottom: 16px;
                background: rgba(255,255,255,0.15);
                border-radius: 16px;
                border-left: 8px solid #4fc3f7;
                backdrop-filter: blur(10px);
                box-shadow: 0 4px 20px rgba(0,0,0,0.3);
            }}
            .note-item:hover {{
                background: rgba(255,255,255,0.2);
                transform: translateX(4px);
                transition: all 0.2s ease;
            }}
            .note-title {{
                font-weight: 700;
                color: #ffffff;
                margin-bottom: 10px;
                font-size: 22px;
                text-shadow: 1px 1px 2px rgba(0,0,0,0.3);
            }}
            .note-preview {{
                color: #e3f2fd;
                font-size: 17px;
                margin-bottom: 10px;
                line-height: 1.5;
            }}
            .note-meta {{
                color: #81d4fa;
                font-size: 14px;
                font-weight: 500;
            }}
            .count {{
                color: #b3e5fc;
                font-size: 18px;
                margin-bottom: 20px;
            }}

            /* Action buttons */
            .note-actions {{
                margin-top: 12px;
                display: flex;
                gap: 8px;
                flex-wrap: wrap;
            }}
            .btn {{
                padding: 8px 16px;
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
            .btn:hover {{
                transform: translateY(-2px);
                box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            }}
            .btn:active {{
                transform: translateY(0);
            }}
            .btn-view {{
                background: #3b82f6;
                color: white;
            }}
            .btn-view:hover {{
                background: #2563eb;
            }}
            .btn-delete {{
                background: #ef4444;
                color: white;
            }}
            .btn-delete:hover {{
                background: #dc2626;
            }}
        </style>
    </head>
    <body>
        <h2>📝 Notes</h2>
        <p class="count">Showing {len(notes_list)} note(s)</p>
        <ul class="notes-list">
            {items_html}
        </ul>

        <script>
            // List context for preserving state across action tool calls
            const LIST_CONTEXT = {{
                limit: {limit},
                include_deleted: {'true' if include_deleted else 'false'}
            }};

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

            // View note details
            function viewNote(noteUid) {{
                callTool('show_note_ui', {{
                    uid: noteUid,
                    include_deleted: LIST_CONTEXT.include_deleted
                }});
            }}

            // Delete a note - uses UI tool for interactive response
            function deleteNote(noteUid) {{
                callTool('delete_note_ui', {{
                    uid: noteUid,
                    limit: LIST_CONTEXT.limit,
                    include_deleted: LIST_CONTEXT.include_deleted
                }});
            }}
        </script>
    </body>
    </html>
    """


def render_note_detail_html(note: "Note") -> str:
    """
    Render HTML for a single note detail view.

    Args:
        note: Note object to display

    Returns:
        HTML string with the full note content
    """
    title = escape(note.payload.get("title") or "Untitled")
    content = escape(note.payload.get("content") or "No content")
    uid = escape(note.uid)
    tags = note.payload.get("tags") or []

    tags_html = ""
    if tags:
        tags_html = '<div class="tags">' + "".join(
            f'<span class="tag">{escape(str(tag))}</span>' for tag in tags
        ) + "</div>"

    status = note.payload.get("status") or ""
    status_badge = ""
    if status:
        status_badge = f'<span class="status-badge status-{escape(status)}">{escape(status)}</span>'

    return f"""
    <html>
    <head>
        <style>
            body {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; margin: 0; }}
            .note-header {{ display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }}
            h1 {{ margin: 0; color: #333; font-size: 24px; }}
            .content {{
                background: #f8f9fa;
                padding: 16px;
                border-radius: 8px;
                white-space: pre-wrap;
                line-height: 1.6;
            }}
            .meta {{ color: #666; font-size: 12px; margin-top: 16px; }}
            .tags {{ margin-top: 12px; }}
            .tag {{
                display: inline-block;
                background: #e9ecef;
                padding: 4px 8px;
                border-radius: 4px;
                font-size: 12px;
                margin-right: 4px;
            }}
            .status-badge {{
                display: inline-block;
                padding: 4px 8px;
                border-radius: 4px;
                font-size: 12px;
                font-weight: 500;
            }}
            .status-archived {{ background: #ffc107; color: #000; }}
            .status-pinned {{ background: #28a745; color: #fff; }}
        </style>
    </head>
    <body>
        <div class="note-header">
            <h1>📝 {title}</h1>
            {status_badge}
        </div>
        {tags_html}
        <div class="content">{content}</div>
        <div class="meta">
            UID: {uid} | Version: {note.version} | Updated: {escape(note.updated_at)}
        </div>
    </body>
    </html>
    """
