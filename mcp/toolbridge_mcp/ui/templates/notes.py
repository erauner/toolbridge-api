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


def render_notes_list_html(notes: Iterable["Note"]) -> str:
    """
    Render an HTML list of notes.

    Args:
        notes: Iterable of Note objects to display

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
        title = escape(note.payload.get("title", "Untitled"))
        content_preview = escape(note.payload.get("content", "")[:100])
        if len(note.payload.get("content", "")) > 100:
            content_preview += "..."
        uid = escape(note.uid)

        items_html += f"""
        <li class="note-item" data-uid="{uid}">
            <div class="note-title">{title}</div>
            <div class="note-preview">{content_preview}</div>
            <div class="note-meta">UID: {uid[:8]}... | v{note.version}</div>
        </li>
        """

    return f"""
    <html>
    <head>
        <style>
            body {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; margin: 0; }}
            h2 {{ margin-top: 0; color: #333; }}
            .notes-list {{ list-style: none; padding: 0; margin: 0; }}
            .note-item {{
                padding: 12px;
                margin-bottom: 8px;
                background: #f8f9fa;
                border-radius: 8px;
                border-left: 4px solid #007bff;
            }}
            .note-title {{ font-weight: 600; color: #333; margin-bottom: 4px; }}
            .note-preview {{ color: #666; font-size: 14px; margin-bottom: 4px; }}
            .note-meta {{ color: #999; font-size: 12px; }}
            .count {{ color: #666; font-size: 14px; margin-bottom: 12px; }}
        </style>
    </head>
    <body>
        <h2>📝 Notes</h2>
        <p class="count">Showing {len(notes_list)} note(s)</p>
        <ul class="notes-list">
            {items_html}
        </ul>
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
    title = escape(note.payload.get("title", "Untitled"))
    content = escape(note.payload.get("content", "No content"))
    uid = escape(note.uid)
    tags = note.payload.get("tags", [])

    tags_html = ""
    if tags:
        tags_html = '<div class="tags">' + "".join(
            f'<span class="tag">{escape(str(tag))}</span>' for tag in tags
        ) + "</div>"

    status = note.payload.get("status", "")
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
