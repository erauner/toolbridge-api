"""
ChatGPT Apps SDK widget templates.

These HTML templates are used as output templates for ChatGPT Apps SDK integration.
They render data from window.openai.toolOutput and invoke tools via window.openai.callTool.

Key differences from standard MCP-UI templates:
- Data comes from window.openai.toolOutput (structured content from tool result)
- Tool calls use window.openai.callTool(toolName, params)
- Templates are served as resources with MIME type text/html+skybridge
"""

from html import escape


def _apps_base_styles() -> str:
    """Common CSS styles for Apps SDK widgets."""
    return """
        * { box-sizing: border-box; }
        html, body {
            margin: 0;
            padding: 0;
            min-height: 100vh;
            width: 100%;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            font-size: 16px;
            color: #1a1a1a;
            padding: 16px;
            background: #ffffff;
        }
        h2 {
            margin: 0 0 16px 0;
            color: #111827;
            font-size: 24px;
            font-weight: 600;
        }
        .loading {
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 200px;
            color: #6b7280;
            font-style: italic;
        }
        .error {
            background: #fef2f2;
            border: 1px solid #fecaca;
            border-radius: 8px;
            padding: 16px;
            color: #dc2626;
        }
        .empty {
            color: #6b7280;
            font-style: italic;
            text-align: center;
            padding: 32px;
        }
        .btn {
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
        }
        .btn:hover {
            transform: translateY(-1px);
            box-shadow: 0 2px 8px rgba(0,0,0,0.15);
        }
        .btn:active {
            transform: translateY(0);
        }
        .btn-primary {
            background: #3b82f6;
            color: white;
        }
        .btn-primary:hover {
            background: #2563eb;
        }
        .btn-danger {
            background: #ef4444;
            color: white;
        }
        .btn-danger:hover {
            background: #dc2626;
        }
        .btn-success {
            background: #22c55e;
            color: white;
        }
        .btn-success:hover {
            background: #16a34a;
        }
        .btn-secondary {
            background: #6b7280;
            color: white;
        }
        .btn-secondary:hover {
            background: #4b5563;
        }
    """


def _apps_base_script() -> str:
    """Base JavaScript for Apps SDK widgets."""
    return """
        // ChatGPT Apps SDK widget initialization
        (function() {
            // Store data globally for render functions
            window.widgetData = null;

            // Listen for tool output from Apps SDK
            window.addEventListener('message', function(event) {
                // Apps SDK sends structured content via postMessage
                if (event.data && event.data.type === 'toolOutput') {
                    window.widgetData = event.data.payload;
                    render();
                }
            });

            // Signal ready to Apps SDK
            if (window.openai && window.openai.ready) {
                window.openai.ready();
            }

            // Check if toolOutput is already available (sync case)
            if (window.openai && window.openai.toolOutput) {
                window.widgetData = window.openai.toolOutput;
                render();
            }
        })();

        // Tool call helper - uses Apps SDK callTool
        function callTool(toolName, params) {
            if (window.openai && typeof window.openai.callTool === 'function') {
                window.openai.callTool(toolName, params || {});
            } else {
                console.warn('Apps SDK callTool not available');
            }
        }
    """


def notes_list_template_html() -> str:
    """
    Apps SDK template for notes list.

    Expected structuredContent:
    {
        "notes": [{"uid": "...", "version": 1, "payload": {"title": "...", "content": "..."}}],
        "limit": 20,
        "include_deleted": false,
        "next_cursor": null
    }
    """
    return f"""
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <style>
            {_apps_base_styles()}

            .notes-list {{
                list-style: none;
                padding: 0;
                margin: 0;
            }}
            .note-item {{
                padding: 16px;
                margin-bottom: 12px;
                background: #f9fafb;
                border-radius: 12px;
                border-left: 4px solid #3b82f6;
            }}
            .note-item:hover {{
                background: #f3f4f6;
            }}
            .note-title {{
                font-weight: 600;
                color: #111827;
                margin-bottom: 8px;
                font-size: 18px;
            }}
            .note-preview {{
                color: #4b5563;
                font-size: 14px;
                margin-bottom: 8px;
                line-height: 1.5;
            }}
            .note-meta {{
                color: #9ca3af;
                font-size: 12px;
            }}
            .note-actions {{
                margin-top: 12px;
                display: flex;
                gap: 8px;
                flex-wrap: wrap;
            }}
            .count {{
                color: #6b7280;
                font-size: 14px;
                margin-bottom: 16px;
            }}
        </style>
    </head>
    <body>
        <div id="content">
            <div class="loading">Loading notes...</div>
        </div>

        <script>
            {_apps_base_script()}

            function escapeHtml(text) {{
                const div = document.createElement('div');
                div.textContent = text || '';
                return div.innerHTML;
            }}

            function render() {{
                const container = document.getElementById('content');
                const data = window.widgetData;

                if (!data || !data.notes) {{
                    container.innerHTML = '<div class="error">No data available</div>';
                    return;
                }}

                const notes = data.notes;
                const limit = data.limit || 20;
                const includeDeleted = data.include_deleted || false;

                if (notes.length === 0) {{
                    container.innerHTML = `
                        <h2>Notes</h2>
                        <p class="empty">No notes found.</p>
                    `;
                    return;
                }}

                let html = `
                    <h2>Notes</h2>
                    <p class="count">Showing ${{notes.length}} note(s)</p>
                    <ul class="notes-list">
                `;

                notes.forEach(function(note) {{
                    const title = escapeHtml(note.payload?.title || 'Untitled');
                    const content = note.payload?.content || '';
                    const preview = escapeHtml(content.substring(0, 100)) + (content.length > 100 ? '...' : '');
                    const uid = note.uid;
                    const version = note.version || 1;

                    html += `
                        <li class="note-item" data-uid="${{uid}}">
                            <div class="note-title">${{title}}</div>
                            <div class="note-preview">${{preview}}</div>
                            <div class="note-meta">UID: ${{uid.substring(0, 8)}}... | v${{version}}</div>
                            <div class="note-actions">
                                <button class="btn btn-primary" onclick="viewNote('${{uid}}', ${{includeDeleted}})">View</button>
                                <button class="btn btn-danger" onclick="deleteNote('${{uid}}', ${{limit}}, ${{includeDeleted}})">Delete</button>
                            </div>
                        </li>
                    `;
                }});

                html += '</ul>';
                container.innerHTML = html;
            }}

            function viewNote(uid, includeDeleted) {{
                callTool('show_note_ui', {{
                    uid: uid,
                    include_deleted: includeDeleted
                }});
            }}

            function deleteNote(uid, limit, includeDeleted) {{
                callTool('delete_note_ui', {{
                    uid: uid,
                    limit: limit,
                    include_deleted: includeDeleted
                }});
            }}
        </script>
    </body>
    </html>
    """


def note_detail_template_html() -> str:
    """
    Apps SDK template for note detail view.

    Expected structuredContent:
    {
        "note": {"uid": "...", "version": 1, "payload": {"title": "...", "content": "...", "tags": []}}
    }
    """
    return f"""
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <style>
            {_apps_base_styles()}

            .note-header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 16px;
            }}
            h1 {{
                margin: 0;
                color: #111827;
                font-size: 24px;
                flex: 1;
            }}
            .content {{
                background: #f9fafb;
                padding: 16px;
                border-radius: 8px;
                white-space: pre-wrap;
                line-height: 1.6;
                font-size: 15px;
            }}
            .meta {{
                color: #6b7280;
                font-size: 12px;
                margin-top: 16px;
            }}
            .tags {{
                margin-top: 12px;
                display: flex;
                gap: 8px;
                flex-wrap: wrap;
            }}
            .tag {{
                display: inline-block;
                background: #e5e7eb;
                padding: 4px 10px;
                border-radius: 16px;
                font-size: 12px;
                color: #374151;
            }}
            .status-badge {{
                display: inline-block;
                padding: 4px 10px;
                border-radius: 16px;
                font-size: 12px;
                font-weight: 500;
            }}
            .status-archived {{
                background: #fef3c7;
                color: #92400e;
            }}
            .status-pinned {{
                background: #d1fae5;
                color: #065f46;
            }}
            .actions {{
                margin-top: 20px;
                display: flex;
                gap: 8px;
            }}
        </style>
    </head>
    <body>
        <div id="content">
            <div class="loading">Loading note...</div>
        </div>

        <script>
            {_apps_base_script()}

            function escapeHtml(text) {{
                const div = document.createElement('div');
                div.textContent = text || '';
                return div.innerHTML;
            }}

            function render() {{
                const container = document.getElementById('content');
                const data = window.widgetData;

                if (!data || !data.note) {{
                    container.innerHTML = '<div class="error">Note not found</div>';
                    return;
                }}

                const note = data.note;
                const title = escapeHtml(note.payload?.title || 'Untitled');
                const content = escapeHtml(note.payload?.content || 'No content');
                const uid = note.uid;
                const version = note.version || 1;
                const updatedAt = note.updated_at || '';
                const tags = note.payload?.tags || [];
                const status = note.payload?.status || '';

                let tagsHtml = '';
                if (tags.length > 0) {{
                    tagsHtml = '<div class="tags">' + tags.map(t => `<span class="tag">${{escapeHtml(String(t))}}</span>`).join('') + '</div>';
                }}

                let statusHtml = '';
                if (status) {{
                    statusHtml = `<span class="status-badge status-${{escapeHtml(status)}}">${{escapeHtml(status)}}</span>`;
                }}

                container.innerHTML = `
                    <div class="note-header">
                        <h1>${{title}}</h1>
                        ${{statusHtml}}
                    </div>
                    ${{tagsHtml}}
                    <div class="content">${{content}}</div>
                    <div class="meta">
                        UID: ${{uid}} | Version: ${{version}} | Updated: ${{escapeHtml(updatedAt)}}
                    </div>
                    <div class="actions">
                        <button class="btn btn-secondary" onclick="backToList()">Back to List</button>
                        <button class="btn btn-danger" onclick="deleteNote('${{uid}}')">Delete</button>
                    </div>
                `;
            }}

            function backToList() {{
                callTool('list_notes_ui', {{}});
            }}

            function deleteNote(uid) {{
                callTool('delete_note_ui', {{ uid: uid }});
            }}
        </script>
    </body>
    </html>
    """


def note_edit_template_html() -> str:
    """
    Apps SDK template for note edit diff view.

    Expected structuredContent:
    {
        "edit_id": "...",
        "note_uid": "...",
        "title": "...",
        "summary": "...",
        "hunks": [
            {"id": "h1", "kind": "changed", "status": "pending", "original": "...", "proposed": "..."}
        ],
        "counts": {"accepted": 0, "rejected": 0, "revised": 0, "pending": 3}
    }
    """
    return f"""
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <style>
            {_apps_base_styles()}

            .edit-header {{
                margin-bottom: 16px;
            }}
            .summary {{
                color: #4b5563;
                font-size: 14px;
                margin-bottom: 12px;
                padding: 12px;
                background: #f0f9ff;
                border-radius: 8px;
                border-left: 4px solid #3b82f6;
            }}
            .counts {{
                display: flex;
                gap: 16px;
                margin-bottom: 16px;
                font-size: 14px;
            }}
            .count-item {{
                padding: 4px 12px;
                border-radius: 16px;
                font-weight: 500;
            }}
            .count-accepted {{ background: #d1fae5; color: #065f46; }}
            .count-rejected {{ background: #fee2e2; color: #991b1b; }}
            .count-revised {{ background: #dbeafe; color: #1e40af; }}
            .count-pending {{ background: #f3f4f6; color: #374151; }}

            .hunks-list {{
                margin-bottom: 20px;
            }}
            .hunk {{
                margin-bottom: 16px;
                border: 1px solid #e5e7eb;
                border-radius: 8px;
                overflow: hidden;
            }}
            .hunk-header {{
                padding: 8px 12px;
                background: #f9fafb;
                border-bottom: 1px solid #e5e7eb;
                display: flex;
                justify-content: space-between;
                align-items: center;
                font-size: 13px;
            }}
            .hunk-status {{
                padding: 2px 8px;
                border-radius: 12px;
                font-size: 11px;
                font-weight: 600;
            }}
            .status-pending {{ background: #fef3c7; color: #92400e; }}
            .status-accepted {{ background: #d1fae5; color: #065f46; }}
            .status-rejected {{ background: #fee2e2; color: #991b1b; }}
            .status-revised {{ background: #dbeafe; color: #1e40af; }}

            .hunk-content {{
                padding: 12px;
                font-family: 'SF Mono', Monaco, 'Courier New', monospace;
                font-size: 13px;
                line-height: 1.5;
            }}
            .diff-line {{
                padding: 2px 8px;
                margin: 0;
                white-space: pre-wrap;
                word-break: break-all;
            }}
            .diff-added {{
                background: #d1fae5;
                color: #065f46;
            }}
            .diff-removed {{
                background: #fee2e2;
                color: #991b1b;
                text-decoration: line-through;
            }}
            .diff-context {{
                background: #f9fafb;
                color: #6b7280;
            }}

            .hunk-actions {{
                padding: 8px 12px;
                background: #f9fafb;
                border-top: 1px solid #e5e7eb;
                display: flex;
                gap: 8px;
            }}
            .hunk-actions .btn {{
                font-size: 12px;
                padding: 6px 12px;
            }}

            .main-actions {{
                display: flex;
                gap: 12px;
                padding-top: 16px;
                border-top: 1px solid #e5e7eb;
            }}
        </style>
    </head>
    <body>
        <div id="content">
            <div class="loading">Loading diff...</div>
        </div>

        <script>
            {_apps_base_script()}

            function escapeHtml(text) {{
                const div = document.createElement('div');
                div.textContent = text || '';
                return div.innerHTML;
            }}

            function render() {{
                const container = document.getElementById('content');
                const data = window.widgetData;

                if (!data || !data.edit_id) {{
                    container.innerHTML = '<div class="error">Edit session not found</div>';
                    return;
                }}

                const editId = data.edit_id;
                const title = escapeHtml(data.title || 'Note Edit');
                const summary = data.summary ? escapeHtml(data.summary) : '';
                const hunks = data.hunks || [];
                const counts = data.counts || {{}};

                let summaryHtml = summary ? `<div class="summary">${{summary}}</div>` : '';

                let countsHtml = `
                    <div class="counts">
                        <span class="count-item count-accepted">Accepted: ${{counts.accepted || 0}}</span>
                        <span class="count-item count-rejected">Rejected: ${{counts.rejected || 0}}</span>
                        <span class="count-item count-revised">Revised: ${{counts.revised || 0}}</span>
                        <span class="count-item count-pending">Pending: ${{counts.pending || 0}}</span>
                    </div>
                `;

                let hunksHtml = '<div class="hunks-list">';
                hunks.forEach(function(hunk, index) {{
                    const hunkId = hunk.id || 'h' + index;
                    const kind = hunk.kind || 'unchanged';
                    const status = hunk.status || 'pending';
                    const original = hunk.original || '';
                    const proposed = hunk.proposed || '';

                    if (kind === 'unchanged') {{
                        // Skip unchanged hunks or show collapsed
                        return;
                    }}

                    let diffHtml = '';
                    if (kind === 'removed') {{
                        original.split('\\n').forEach(line => {{
                            diffHtml += `<div class="diff-line diff-removed">- ${{escapeHtml(line)}}</div>`;
                        }});
                    }} else if (kind === 'added') {{
                        proposed.split('\\n').forEach(line => {{
                            diffHtml += `<div class="diff-line diff-added">+ ${{escapeHtml(line)}}</div>`;
                        }});
                    }} else {{
                        // Changed - show both
                        original.split('\\n').forEach(line => {{
                            diffHtml += `<div class="diff-line diff-removed">- ${{escapeHtml(line)}}</div>`;
                        }});
                        proposed.split('\\n').forEach(line => {{
                            diffHtml += `<div class="diff-line diff-added">+ ${{escapeHtml(line)}}</div>`;
                        }});
                    }}

                    hunksHtml += `
                        <div class="hunk" data-hunk-id="${{hunkId}}">
                            <div class="hunk-header">
                                <span>Change #${{index + 1}} (${{kind}})</span>
                                <span class="hunk-status status-${{status}}">${{status.toUpperCase()}}</span>
                            </div>
                            <div class="hunk-content">${{diffHtml}}</div>
                            <div class="hunk-actions">
                                <button class="btn btn-success" onclick="acceptHunk('${{editId}}', '${{hunkId}}')" ${{status !== 'pending' ? 'disabled' : ''}}>Accept</button>
                                <button class="btn btn-danger" onclick="rejectHunk('${{editId}}', '${{hunkId}}')" ${{status !== 'pending' ? 'disabled' : ''}}>Reject</button>
                            </div>
                        </div>
                    `;
                }});
                hunksHtml += '</div>';

                const canApply = (counts.pending || 0) === 0;

                container.innerHTML = `
                    <div class="edit-header">
                        <h2>Edit: ${{title}}</h2>
                        ${{summaryHtml}}
                        ${{countsHtml}}
                    </div>
                    ${{hunksHtml}}
                    <div class="main-actions">
                        <button class="btn btn-success" onclick="applyEdit('${{editId}}')" ${{!canApply ? 'disabled' : ''}}>Apply All Changes</button>
                        <button class="btn btn-secondary" onclick="discardEdit('${{editId}}')">Discard</button>
                    </div>
                `;
            }}

            function acceptHunk(editId, hunkId) {{
                callTool('accept_note_edit_hunk', {{ edit_id: editId, hunk_id: hunkId }});
            }}

            function rejectHunk(editId, hunkId) {{
                callTool('reject_note_edit_hunk', {{ edit_id: editId, hunk_id: hunkId }});
            }}

            function applyEdit(editId) {{
                callTool('apply_note_edit', {{ edit_id: editId }});
            }}

            function discardEdit(editId) {{
                callTool('discard_note_edit', {{ edit_id: editId }});
            }}
        </script>
    </body>
    </html>
    """


def tasks_list_template_html() -> str:
    """
    Apps SDK template for tasks list.

    Expected structuredContent:
    {
        "tasks": [{"uid": "...", "version": 1, "payload": {"title": "...", "status": "...", "priority": "..."}}],
        "limit": 20,
        "include_deleted": false,
        "next_cursor": null
    }
    """
    return f"""
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <style>
            {_apps_base_styles()}

            .tasks-list {{
                list-style: none;
                padding: 0;
                margin: 0;
            }}
            .task-item {{
                padding: 16px;
                margin-bottom: 12px;
                background: #f9fafb;
                border-radius: 12px;
            }}
            .task-item.priority-high {{
                border-left: 4px solid #ef4444;
            }}
            .task-item.priority-medium {{
                border-left: 4px solid #f59e0b;
            }}
            .task-item.priority-low {{
                border-left: 4px solid #6b7280;
            }}
            .task-item:not([class*="priority-"]) {{
                border-left: 4px solid #3b82f6;
            }}
            .task-header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 8px;
            }}
            .status-icon {{
                font-size: 20px;
            }}
            .task-title {{
                font-weight: 600;
                color: #111827;
                flex: 1;
                font-size: 16px;
            }}
            .priority {{
                font-size: 11px;
                padding: 2px 8px;
                border-radius: 12px;
                text-transform: uppercase;
                font-weight: 600;
                letter-spacing: 0.5px;
            }}
            .priority-high {{ background: #fee2e2; color: #991b1b; }}
            .priority-medium {{ background: #fef3c7; color: #92400e; }}
            .priority-low {{ background: #f3f4f6; color: #374151; }}
            .task-description {{
                color: #4b5563;
                font-size: 14px;
                margin-bottom: 8px;
                line-height: 1.4;
            }}
            .task-meta {{
                color: #9ca3af;
                font-size: 12px;
                display: flex;
                gap: 16px;
            }}
            .due-date {{
                color: #3b82f6;
                font-weight: 500;
            }}
            .task-actions {{
                margin-top: 12px;
                display: flex;
                gap: 8px;
                flex-wrap: wrap;
            }}
            .count {{
                color: #6b7280;
                font-size: 14px;
                margin-bottom: 16px;
            }}
        </style>
    </head>
    <body>
        <div id="content">
            <div class="loading">Loading tasks...</div>
        </div>

        <script>
            {_apps_base_script()}

            const STATUS_ICONS = {{
                'todo': '\\u2B1C',
                'in_progress': '\\uD83D\\uDD04',
                'done': '\\u2705',
                'archived': '\\uD83D\\uDCE6'
            }};

            function escapeHtml(text) {{
                const div = document.createElement('div');
                div.textContent = text || '';
                return div.innerHTML;
            }}

            function render() {{
                const container = document.getElementById('content');
                const data = window.widgetData;

                if (!data || !data.tasks) {{
                    container.innerHTML = '<div class="error">No data available</div>';
                    return;
                }}

                const tasks = data.tasks;
                const limit = data.limit || 20;
                const includeDeleted = data.include_deleted || false;

                if (tasks.length === 0) {{
                    container.innerHTML = `
                        <h2>Tasks</h2>
                        <p class="empty">No tasks found.</p>
                    `;
                    return;
                }}

                let html = `
                    <h2>Tasks</h2>
                    <p class="count">Showing ${{tasks.length}} task(s)</p>
                    <ul class="tasks-list">
                `;

                tasks.forEach(function(task) {{
                    const title = escapeHtml(task.payload?.title || 'Untitled');
                    const description = task.payload?.description || '';
                    const descPreview = escapeHtml(description.substring(0, 80)) + (description.length > 80 ? '...' : '');
                    const uid = task.uid;
                    const status = task.payload?.status || 'todo';
                    const priority = task.payload?.priority || '';
                    const dueDate = task.payload?.dueDate || '';

                    const statusIcon = STATUS_ICONS[status] || STATUS_ICONS['todo'];
                    const priorityClass = priority ? 'priority-' + priority : '';

                    let dueDateHtml = dueDate ? `<span class="due-date">Due: ${{escapeHtml(dueDate.substring(0, 10))}}</span>` : '';
                    let priorityHtml = priority ? `<span class="priority ${{priorityClass}}">${{escapeHtml(priority)}}</span>` : '';

                    let actionButton = status === 'done'
                        ? `<button class="btn btn-secondary" onclick="archiveTask('${{uid}}', ${{limit}}, ${{includeDeleted}})">Archive</button>`
                        : `<button class="btn btn-success" onclick="completeTask('${{uid}}', ${{limit}}, ${{includeDeleted}})">Complete</button>`;

                    html += `
                        <li class="task-item ${{priorityClass}}" data-uid="${{uid}}" data-status="${{status}}">
                            <div class="task-header">
                                <span class="status-icon">${{statusIcon}}</span>
                                <span class="task-title">${{title}}</span>
                                ${{priorityHtml}}
                            </div>
                            <div class="task-description">${{descPreview}}</div>
                            <div class="task-meta">
                                ${{dueDateHtml}}
                                <span>UID: ${{uid.substring(0, 8)}}...</span>
                            </div>
                            <div class="task-actions">
                                <button class="btn btn-primary" onclick="viewTask('${{uid}}', ${{includeDeleted}})">View</button>
                                ${{actionButton}}
                            </div>
                        </li>
                    `;
                }});

                html += '</ul>';
                container.innerHTML = html;
            }}

            function viewTask(uid, includeDeleted) {{
                callTool('show_task_ui', {{
                    uid: uid,
                    include_deleted: includeDeleted
                }});
            }}

            function completeTask(uid, limit, includeDeleted) {{
                callTool('process_task_ui', {{
                    uid: uid,
                    action: 'complete',
                    limit: limit,
                    include_deleted: includeDeleted
                }});
            }}

            function archiveTask(uid, limit, includeDeleted) {{
                callTool('archive_task_ui', {{
                    uid: uid,
                    limit: limit,
                    include_deleted: includeDeleted
                }});
            }}
        </script>
    </body>
    </html>
    """


def task_detail_template_html() -> str:
    """
    Apps SDK template for task detail view.

    Expected structuredContent:
    {
        "task": {"uid": "...", "version": 1, "payload": {"title": "...", "status": "...", "priority": "...", "description": "..."}}
    }
    """
    return f"""
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <style>
            {_apps_base_styles()}

            .task-header {{
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 16px;
            }}
            .status-icon {{
                font-size: 28px;
            }}
            h1 {{
                margin: 0;
                color: #111827;
                font-size: 24px;
                flex: 1;
            }}
            .priority {{
                font-size: 12px;
                padding: 4px 12px;
                border-radius: 16px;
                text-transform: uppercase;
                font-weight: 600;
            }}
            .priority-high {{ background: #fee2e2; color: #991b1b; }}
            .priority-medium {{ background: #fef3c7; color: #92400e; }}
            .priority-low {{ background: #f3f4f6; color: #374151; }}
            .status {{
                margin-top: 8px;
                color: #6b7280;
                font-size: 14px;
            }}
            .due-date {{
                color: #3b82f6;
                margin-top: 12px;
                font-weight: 500;
            }}
            .tags {{
                margin-top: 12px;
                display: flex;
                gap: 8px;
                flex-wrap: wrap;
            }}
            .tag {{
                display: inline-block;
                background: #e5e7eb;
                padding: 4px 10px;
                border-radius: 16px;
                font-size: 12px;
                color: #374151;
            }}
            .description {{
                background: #f9fafb;
                padding: 16px;
                border-radius: 8px;
                white-space: pre-wrap;
                line-height: 1.6;
                font-size: 15px;
                margin-top: 16px;
            }}
            .meta {{
                color: #6b7280;
                font-size: 12px;
                margin-top: 16px;
            }}
            .actions {{
                margin-top: 20px;
                display: flex;
                gap: 8px;
            }}
        </style>
    </head>
    <body>
        <div id="content">
            <div class="loading">Loading task...</div>
        </div>

        <script>
            {_apps_base_script()}

            const STATUS_ICONS = {{
                'todo': '\\u2B1C',
                'in_progress': '\\uD83D\\uDD04',
                'done': '\\u2705',
                'archived': '\\uD83D\\uDCE6'
            }};

            function escapeHtml(text) {{
                const div = document.createElement('div');
                div.textContent = text || '';
                return div.innerHTML;
            }}

            function render() {{
                const container = document.getElementById('content');
                const data = window.widgetData;

                if (!data || !data.task) {{
                    container.innerHTML = '<div class="error">Task not found</div>';
                    return;
                }}

                const task = data.task;
                const title = escapeHtml(task.payload?.title || 'Untitled');
                const description = escapeHtml(task.payload?.description || 'No description');
                const uid = task.uid;
                const version = task.version || 1;
                const updatedAt = task.updated_at || '';
                const status = task.payload?.status || 'todo';
                const priority = task.payload?.priority || '';
                const dueDate = task.payload?.dueDate || '';
                const tags = task.payload?.tags || [];

                const statusIcon = STATUS_ICONS[status] || STATUS_ICONS['todo'];
                const priorityClass = priority ? 'priority-' + priority : '';

                let tagsHtml = '';
                if (tags.length > 0) {{
                    tagsHtml = '<div class="tags">' + tags.map(t => `<span class="tag">${{escapeHtml(String(t))}}</span>`).join('') + '</div>';
                }}

                let dueDateHtml = dueDate ? `<div class="due-date">Due: ${{escapeHtml(dueDate)}}</div>` : '';
                let priorityHtml = priority ? `<span class="priority ${{priorityClass}}">${{escapeHtml(priority)}}</span>` : '';

                container.innerHTML = `
                    <div class="task-header">
                        <span class="status-icon">${{statusIcon}}</span>
                        <h1>${{title}}</h1>
                        ${{priorityHtml}}
                    </div>
                    <div class="status">Status: ${{escapeHtml(status)}}</div>
                    ${{dueDateHtml}}
                    ${{tagsHtml}}
                    <h3>Description</h3>
                    <div class="description">${{description}}</div>
                    <div class="meta">
                        UID: ${{uid}} | Version: ${{version}} | Updated: ${{escapeHtml(updatedAt)}}
                    </div>
                    <div class="actions">
                        <button class="btn btn-secondary" onclick="backToList()">Back to List</button>
                    </div>
                `;
            }}

            function backToList() {{
                callTool('list_tasks_ui', {{}});
            }}
        </script>
    </body>
    </html>
    """
