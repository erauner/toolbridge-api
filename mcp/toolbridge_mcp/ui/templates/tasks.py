"""
HTML templates for Task UI resources.

Converts Task models into HTML for MCP-UI rendering.
NOTE: These are minimal stub templates. A future ticket will add:
- Proper CSS styling
- Interactive elements (complete, edit, delete buttons)
- MCP-UI action handlers (postMessage events)
"""

from typing import Iterable, TYPE_CHECKING
from html import escape

if TYPE_CHECKING:
    from toolbridge_mcp.tools.tasks import Task


def _get_status_icon(status: str) -> str:
    """Get an emoji icon for task status."""
    icons = {
        "todo": "⬜",
        "in_progress": "🔄",
        "done": "✅",
        "archived": "📦",
    }
    return icons.get(status, "⬜")


def _get_priority_class(priority: str) -> str:
    """Get CSS class for priority styling."""
    return f"priority-{priority}" if priority in ("low", "medium", "high") else ""


def render_tasks_list_html(
    tasks: Iterable["Task"],
    limit: int = 20,
    include_deleted: bool = False,
) -> str:
    """
    Render an HTML list of tasks.

    Args:
        tasks: Iterable of Task objects to display
        limit: Current list limit (passed to action tools to preserve context)
        include_deleted: Current include_deleted setting (passed to action tools)

    Returns:
        HTML string with a styled list of tasks
    """
    tasks_list = list(tasks)

    if not tasks_list:
        return """
        <html>
        <head>
            <style>
                body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; }
                .empty { color: #666; font-style: italic; }
            </style>
        </head>
        <body>
            <h2>✅ Tasks</h2>
            <p class="empty">No tasks found.</p>
        </body>
        </html>
        """

    items_html = ""
    for task in tasks_list:
        title = escape(task.payload.get("title") or "Untitled")
        desc_raw = task.payload.get("description") or ""
        description = escape(desc_raw[:80])
        if len(desc_raw) > 80:
            description += "..."
        uid = escape(task.uid)
        status = task.payload.get("status") or "todo"
        priority = task.payload.get("priority") or ""
        due_date = task.payload.get("dueDate") or ""

        status_icon = _get_status_icon(status)
        priority_class = _get_priority_class(priority)

        due_html = ""
        if due_date:
            due_html = f'<span class="due-date">📅 {escape(due_date[:10])}</span>'

        priority_html = ""
        if priority:
            priority_html = f'<span class="priority {priority_class}">{escape(priority)}</span>'

        # Show different action buttons based on status
        if status == "done":
            action_buttons = f'''
                <button class="btn btn-archive" onclick="archiveTask('{uid}')">📦 Archive</button>
            '''
        else:
            action_buttons = f'''
                <button class="btn btn-complete" onclick="completeTask('{uid}')">✅ Complete</button>
            '''

        items_html += f"""
        <li class="task-item {priority_class}" data-uid="{uid}" data-status="{escape(status)}">
            <div class="task-header">
                <span class="status-icon">{status_icon}</span>
                <span class="task-title">{title}</span>
                {priority_html}
            </div>
            <div class="task-description">{description}</div>
            <div class="task-meta">
                {due_html}
                <span class="uid">UID: {uid[:8]}...</span>
            </div>
            <div class="task-actions">
                <button class="btn btn-view" onclick="viewTask('{uid}')">👁 View</button>
                {action_buttons}
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
                background: #166534;
                font-size: 18px;
                color: #ffffff;
                padding: 16px 24px;
            }}
            h2 {{
                margin-top: 0;
                color: #fde047;
                font-size: 28px;
                margin-bottom: 8px;
                text-shadow: 2px 2px 4px rgba(0,0,0,0.4);
            }}
            .tasks-list {{ list-style: none; padding: 0; margin: 0; }}
            .task-item {{
                padding: 16px 20px;
                margin-bottom: 12px;
                background: #15803d;
                border-radius: 12px;
                box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            }}
            .task-item.priority-high {{
                background: linear-gradient(135deg, #dc2626 0%, #b91c1c 100%);
            }}
            .task-item.priority-medium {{
                background: linear-gradient(135deg, #ca8a04 0%, #a16207 100%);
            }}
            .task-item.priority-low {{
                background: #4b5563;
            }}
            .task-header {{ display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }}
            .status-icon {{ font-size: 24px; }}
            .task-title {{ font-weight: 700; color: #ffffff; flex: 1; font-size: 20px; }}
            .priority {{
                font-size: 12px;
                padding: 4px 12px;
                border-radius: 6px;
                text-transform: uppercase;
                font-weight: 800;
                letter-spacing: 0.5px;
                background: rgba(0,0,0,0.3);
                color: #ffffff;
            }}
            .task-description {{ color: rgba(255,255,255,0.85); font-size: 16px; margin-bottom: 8px; line-height: 1.4; }}
            .task-meta {{ color: rgba(255,255,255,0.7); font-size: 13px; display: flex; gap: 16px; font-weight: 500; }}
            .due-date {{ color: #67e8f9; font-weight: 600; }}
            .count {{ color: #86efac; font-size: 16px; margin-bottom: 16px; }}

            /* Action buttons */
            .task-actions {{
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
            .btn-complete {{
                background: #22c55e;
                color: white;
            }}
            .btn-complete:hover {{
                background: #16a34a;
            }}
            .btn-archive {{
                background: #6b7280;
                color: white;
            }}
            .btn-archive:hover {{
                background: #4b5563;
            }}
        </style>
    </head>
    <body>
        <h2>✅ Tasks</h2>
        <p class="count">Showing {len(tasks_list)} task(s)</p>
        <ul class="tasks-list">
            {items_html}
        </ul>

        <script>
            // List context for preserving state across action tool calls
            const LIST_CONTEXT = {{
                limit: {limit},
                include_deleted: {'true' if include_deleted else 'false'}
            }};

            // MCP-UI action helper - sends tool calls to the host
            function callTool(toolName, params) {{
                window.parent.postMessage({{
                    type: 'tool',
                    payload: {{
                        toolName: toolName,
                        params: params
                    }}
                }}, '*');
            }}

            // View task details
            function viewTask(taskUid) {{
                callTool('show_task_ui', {{
                    uid: taskUid,
                    include_deleted: LIST_CONTEXT.include_deleted
                }});
            }}

            // Complete a task (mark as done) - uses UI tool for interactive response
            function completeTask(taskUid) {{
                callTool('process_task_ui', {{
                    uid: taskUid,
                    action: 'complete',
                    limit: LIST_CONTEXT.limit,
                    include_deleted: LIST_CONTEXT.include_deleted
                }});
            }}

            // Archive a completed task - uses UI tool for interactive response
            function archiveTask(taskUid) {{
                callTool('archive_task_ui', {{
                    uid: taskUid,
                    limit: LIST_CONTEXT.limit,
                    include_deleted: LIST_CONTEXT.include_deleted
                }});
            }}
        </script>
    </body>
    </html>
    """


def render_task_detail_html(task: "Task") -> str:
    """
    Render HTML for a single task detail view.

    Args:
        task: Task object to display

    Returns:
        HTML string with the full task content
    """
    title = escape(task.payload.get("title") or "Untitled")
    description = escape(task.payload.get("description") or "No description")
    uid = escape(task.uid)
    status = task.payload.get("status") or "todo"
    priority = task.payload.get("priority") or ""
    due_date = task.payload.get("dueDate") or ""
    tags = task.payload.get("tags") or []

    status_icon = _get_status_icon(status)
    priority_class = _get_priority_class(priority)

    tags_html = ""
    if tags:
        tags_html = '<div class="tags">' + "".join(
            f'<span class="tag">{escape(str(tag))}</span>' for tag in tags
        ) + "</div>"

    due_html = ""
    if due_date:
        due_html = f'<div class="due-date">📅 Due: {escape(due_date)}</div>'

    priority_html = ""
    if priority:
        priority_html = f'<span class="priority {priority_class}">{escape(priority)}</span>'

    return f"""
    <html>
    <head>
        <style>
            body {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; margin: 0; }}
            .task-header {{ display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }}
            h1 {{ margin: 0; color: #333; font-size: 24px; }}
            .status-icon {{ font-size: 28px; }}
            .description {{
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
            .priority {{
                font-size: 12px;
                padding: 4px 8px;
                border-radius: 4px;
                text-transform: uppercase;
                font-weight: 500;
            }}
            .priority-high {{ background: #f8d7da; color: #721c24; }}
            .priority-medium {{ background: #fff3cd; color: #856404; }}
            .priority-low {{ background: #e2e3e5; color: #383d41; }}
            .due-date {{ color: #007bff; margin-top: 12px; font-weight: 500; }}
            .status {{ margin-top: 8px; color: #666; }}
        </style>
    </head>
    <body>
        <div class="task-header">
            <span class="status-icon">{status_icon}</span>
            <h1>{title}</h1>
            {priority_html}
        </div>
        <div class="status">Status: {escape(status)}</div>
        {due_html}
        {tags_html}
        <h3>Description</h3>
        <div class="description">{description}</div>
        <div class="meta">
            UID: {uid} | Version: {task.version} | Updated: {escape(task.updated_at)}
        </div>
    </body>
    </html>
    """
