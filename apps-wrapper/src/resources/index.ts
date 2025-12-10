/**
 * Resource definitions for HTML widgets.
 *
 * These resources are served with text/html+skybridge MIME type (via adapter)
 * and are referenced by tools via openai/outputTemplate.
 *
 * Pattern (from mcpui.dev):
 * - Static templates (adapters.appsSdk.enabled) → ChatGPT
 * - Embedded resources (no adapter) → MCP-UI hosts
 */

export interface ResourceDefinition {
  uri: string;
  name: string;
  description: string;
  widgetDescription: string;
}

export const RESOURCE_DEFINITIONS: ResourceDefinition[] = [
  // ════════════════════════════════════════════════════════════════
  // NOTES WIDGETS
  // ════════════════════════════════════════════════════════════════
  {
    uri: "ui://toolbridge/notes/list",
    name: "Notes List Widget",
    description: "Interactive list of notes with View and Delete buttons",
    widgetDescription:
      "A styled card list showing note titles, content previews, and action buttons. Users can view details or delete notes directly from the widget.",
  },
  {
    uri: "ui://toolbridge/notes/detail",
    name: "Note Detail Widget",
    description: "Detailed view of a single note with full content",
    widgetDescription:
      "A detailed card showing the full note content, tags, metadata (UID, version, timestamps), and action buttons for editing or deleting.",
  },
  {
    uri: "ui://toolbridge/notes/edit",
    name: "Note Edit Diff Widget",
    description: "Side-by-side diff preview for note edits",
    widgetDescription:
      "A diff viewer showing proposed changes to a note. Each change (hunk) has Accept/Reject buttons. Users can review and selectively apply changes before committing.",
  },

  // ════════════════════════════════════════════════════════════════
  // TASKS WIDGETS
  // ════════════════════════════════════════════════════════════════
  {
    uri: "ui://toolbridge/tasks/list",
    name: "Tasks List Widget",
    description: "Interactive list of tasks with status indicators",
    widgetDescription:
      "A styled list of tasks showing titles, status badges, and action buttons. Users can mark tasks complete or delete them from the widget.",
  },
];

/**
 * Creates HTML content for a widget based on URI and data.
 *
 * This generates self-contained HTML that:
 * - Reads data from window.__MCP_DATA__ (injected by ChatGPT bridge)
 * - Falls back to embedded data for MCP-UI hosts
 * - Uses ChatGPT bridge for tool calls when available
 */
export function createWidgetHtml(uri: string, data: unknown): string {
  const dataJson = JSON.stringify(data);

  // Base styles shared by all widgets
  const baseStyles = `
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: transparent;
      color: #1a1a1a;
      line-height: 1.5;
      padding: 16px;
    }
    .card {
      background: white;
      border-radius: 12px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.08);
      padding: 16px;
      margin-bottom: 12px;
    }
    .title { font-weight: 600; font-size: 16px; margin-bottom: 8px; }
    .preview { color: #666; font-size: 14px; }
    .meta { color: #888; font-size: 12px; margin-top: 8px; }
    .actions { display: flex; gap: 8px; margin-top: 12px; }
    .btn {
      padding: 8px 16px;
      border-radius: 8px;
      border: none;
      cursor: pointer;
      font-size: 14px;
      font-weight: 500;
      transition: all 0.2s;
    }
    .btn-primary {
      background: #0066cc;
      color: white;
    }
    .btn-primary:hover { background: #0052a3; }
    .btn-danger {
      background: #dc3545;
      color: white;
    }
    .btn-danger:hover { background: #c82333; }
    .btn-secondary {
      background: #f0f0f0;
      color: #333;
    }
    .btn-secondary:hover { background: #e0e0e0; }
    .empty { text-align: center; color: #666; padding: 32px; }
    .badge {
      display: inline-block;
      padding: 2px 8px;
      border-radius: 12px;
      font-size: 12px;
      font-weight: 500;
    }
    .badge-success { background: #d4edda; color: #155724; }
    .badge-warning { background: #fff3cd; color: #856404; }
    .badge-info { background: #d1ecf1; color: #0c5460; }
  `;

  // Bridge script for ChatGPT integration
  const bridgeScript = `
    // Data can come from:
    // 1. Embedded in template (for embedded resources with data)
    // 2. Fetched via bridge (for static templates in ChatGPT)
    let data = ${dataJson};

    // Tool call function - uses ChatGPT bridge when available
    async function callTool(name, args) {
      if (window.__MCP_BRIDGE__?.callTool) {
        return window.__MCP_BRIDGE__.callTool(name, args);
      }
      // Fallback: post message to parent (for MCP-UI hosts)
      window.parent?.postMessage({
        type: 'mcp:tool_call',
        tool: name,
        arguments: args
      }, '*');
    }

    // For static templates, fetch data via bridge on load
    async function fetchDataIfNeeded() {
      // Only fetch if we don't have data and bridge is available
      const hasData = data && (data.items?.length > 0 || data.notes?.length > 0 || data.note || data.uid);
      if (!hasData && window.__MCP_BRIDGE__?.callTool) {
        console.log('[Widget] No embedded data, fetching via bridge...');
        try {
          const result = await window.__MCP_BRIDGE__.callTool('list_notes', { limit: 20 });
          if (result?.structuredContent) {
            data = result.structuredContent;
          } else if (result?.content?.[0]?.text) {
            data = JSON.parse(result.content[0].text);
          }
          console.log('[Widget] Fetched data:', Object.keys(data || {}));
          if (typeof render === 'function') render();
        } catch (e) {
          console.error('[Widget] Failed to fetch data:', e);
        }
      }
    }
  `;

  // Widget-specific content based on URI
  switch (uri) {
    case "ui://toolbridge/notes/list":
      return createNotesListWidget(baseStyles, bridgeScript, dataJson);
    case "ui://toolbridge/notes/detail":
      return createNoteDetailWidget(baseStyles, bridgeScript, dataJson);
    case "ui://toolbridge/notes/edit":
      return createNoteEditWidget(baseStyles, bridgeScript, dataJson);
    case "ui://toolbridge/tasks/list":
      return createTasksListWidget(baseStyles, bridgeScript, dataJson);
    default:
      return createFallbackWidget(baseStyles, uri, dataJson);
  }
}

function createNotesListWidget(baseStyles: string, bridgeScript: string, dataJson: string): string {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    ${baseStyles}
    .loading { text-align: center; padding: 32px; color: #666; }
    .spinner { display: inline-block; width: 24px; height: 24px; border: 3px solid #f3f3f3; border-top: 3px solid #0066cc; border-radius: 50%; animation: spin 1s linear infinite; }
    @keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }
  </style>
</head>
<body>
  <div id="root"></div>
  <script>
    ${bridgeScript}

    let isLoading = false;

    function render() {
      const root = document.getElementById('root');

      // Show loading state
      if (isLoading) {
        root.innerHTML = '<div class="loading"><div class="spinner"></div><div style="margin-top:12px">Loading notes...</div></div>';
        return;
      }

      // Handle both { notes: [...] } and { items: [...] } formats
      const notes = data.notes || data.items || [];

      if (notes.length === 0) {
        root.innerHTML = '<div class="empty">No notes found</div>';
        return;
      }

      root.innerHTML = notes.map(note => {
        // Handle both flat structure and payload structure
        const title = note.title || note.payload?.title || 'Untitled';
        const content = note.content || note.payload?.content || '';
        const tags = note.tags || note.payload?.tags || [];
        const uid = note.uid;
        return \`
        <div class="card">
          <div class="title">\${escapeHtml(title)}</div>
          <div class="preview">\${escapeHtml(truncate(content, 100))}</div>
          <div class="meta">
            \${tags.length ? tags.map(t => \`<span class="badge badge-info">\${escapeHtml(t)}</span>\`).join(' ') : ''}
          </div>
          <div class="actions">
            <button class="btn btn-primary" onclick="viewNote('\${uid}')">View</button>
            <button class="btn btn-danger" onclick="deleteNote('\${uid}')">Delete</button>
          </div>
        </div>
      \`;
      }).join('');
    }

    function escapeHtml(str) {
      const div = document.createElement('div');
      div.textContent = str;
      return div.innerHTML;
    }

    function truncate(str, len) {
      return str.length > len ? str.slice(0, len) + '...' : str;
    }

    function viewNote(uid) {
      callTool('show_note_ui', { uid });
    }

    function deleteNote(uid) {
      if (confirm('Delete this note?')) {
        callTool('delete_note_ui', { uid });
      }
    }

    // For static templates in ChatGPT, fetch data via bridge
    async function fetchDataIfNeeded() {
      const hasData = data && (data.items?.length > 0 || data.notes?.length > 0);
      if (!hasData && window.__MCP_BRIDGE__?.callTool) {
        console.log('[NotesListWidget] No embedded data, fetching via bridge...');
        isLoading = true;
        render();
        try {
          const result = await window.__MCP_BRIDGE__.callTool('list_notes', { limit: 20 });
          console.log('[NotesListWidget] Bridge result:', result);
          if (result?.structuredContent) {
            data = result.structuredContent;
          } else if (result?.content?.[0]?.text) {
            data = JSON.parse(result.content[0].text);
          }
          console.log('[NotesListWidget] Fetched data keys:', Object.keys(data || {}));
        } catch (e) {
          console.error('[NotesListWidget] Failed to fetch data:', e);
        }
        isLoading = false;
      }
      render();
    }

    // Initialize: fetch data if needed, then render
    fetchDataIfNeeded();
  </script>
</body>
</html>`;
}

function createNoteDetailWidget(baseStyles: string, bridgeScript: string, dataJson: string): string {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    ${baseStyles}
    .content { white-space: pre-wrap; font-size: 14px; }
    .meta-row { display: flex; justify-content: space-between; }
  </style>
</head>
<body>
  <div id="root"></div>
  <script>
    ${bridgeScript}

    function render() {
      const root = document.getElementById('root');
      const note = data.note || data;
      // Handle both flat structure and payload structure
      const title = note.title || note.payload?.title || 'Untitled';
      const content = note.content || note.payload?.content || '';
      const tags = note.tags || note.payload?.tags || [];
      const uid = note.uid || 'N/A';
      const version = note.version || 1;

      root.innerHTML = \`
        <div class="card">
          <div class="title">\${escapeHtml(title)}</div>
          <div class="content">\${escapeHtml(content)}</div>
          <div class="meta">
            \${tags.length ? tags.map(t => \`<span class="badge badge-info">\${escapeHtml(t)}</span>\`).join(' ') : ''}
          </div>
          <div class="meta" style="margin-top: 12px">
            <div class="meta-row">
              <span>UID: \${uid}</span>
              <span>Version: \${version}</span>
            </div>
          </div>
          <div class="actions">
            <button class="btn btn-primary" onclick="editNote()">Edit</button>
            <button class="btn btn-danger" onclick="deleteNote()">Delete</button>
            <button class="btn btn-secondary" onclick="backToList()">Back to List</button>
          </div>
        </div>
      \`;
    }

    function escapeHtml(str) {
      const div = document.createElement('div');
      div.textContent = str;
      return div.innerHTML;
    }

    function editNote() {
      const note = data.note || data;
      callTool('edit_note_ui', { uid: note.uid, instructions: 'Edit this note' });
    }

    function deleteNote() {
      const note = data.note || data;
      if (confirm('Delete this note?')) {
        callTool('delete_note_ui', { uid: note.uid });
      }
    }

    function backToList() {
      callTool('list_notes_ui', {});
    }

    render();
  </script>
</body>
</html>`;
}

function createNoteEditWidget(baseStyles: string, bridgeScript: string, dataJson: string): string {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    ${baseStyles}
    .diff-container { font-family: monospace; font-size: 13px; }
    .hunk { margin-bottom: 16px; border: 1px solid #ddd; border-radius: 8px; overflow: hidden; }
    .hunk-header { background: #f6f8fa; padding: 8px 12px; border-bottom: 1px solid #ddd; }
    .hunk-content { padding: 0; }
    .line { padding: 2px 12px; white-space: pre-wrap; }
    .line-removed { background: #ffeef0; color: #cb2431; }
    .line-added { background: #e6ffed; color: #22863a; }
    .line-context { background: white; color: #24292e; }
    .hunk-actions { padding: 8px 12px; background: #f6f8fa; border-top: 1px solid #ddd; }
  </style>
</head>
<body>
  <div id="root"></div>
  <script>
    ${bridgeScript}

    let acceptedHunks = new Set();
    let rejectedHunks = new Set();

    function render() {
      const root = document.getElementById('root');
      const hunks = data.hunks || [];
      const noteUid = data.uid;
      const editId = data.edit_id;

      if (hunks.length === 0) {
        root.innerHTML = '<div class="empty">No changes to review</div>';
        return;
      }

      root.innerHTML = \`
        <div class="card">
          <div class="title">Review Changes</div>
          <div class="diff-container">
            \${hunks.map((hunk, i) => \`
              <div class="hunk" id="hunk-\${i}">
                <div class="hunk-header">Change \${i + 1} of \${hunks.length}</div>
                <div class="hunk-content">
                  \${hunk.lines?.map(line => \`
                    <div class="line \${getLineClass(line)}">\${escapeHtml(line)}</div>
                  \`).join('') || ''}
                </div>
                <div class="hunk-actions">
                  <button class="btn btn-primary" onclick="acceptHunk(\${i})" \${acceptedHunks.has(i) ? 'disabled' : ''}>
                    \${acceptedHunks.has(i) ? 'Accepted' : 'Accept'}
                  </button>
                  <button class="btn btn-danger" onclick="rejectHunk(\${i})" \${rejectedHunks.has(i) ? 'disabled' : ''}>
                    \${rejectedHunks.has(i) ? 'Rejected' : 'Reject'}
                  </button>
                </div>
              </div>
            \`).join('')}
          </div>
          <div class="actions" style="margin-top: 16px">
            <button class="btn btn-primary" onclick="applyChanges()">Apply Selected Changes</button>
            <button class="btn btn-secondary" onclick="cancelEdit()">Cancel</button>
          </div>
        </div>
      \`;
    }

    function getLineClass(line) {
      if (line.startsWith('-')) return 'line-removed';
      if (line.startsWith('+')) return 'line-added';
      return 'line-context';
    }

    function escapeHtml(str) {
      const div = document.createElement('div');
      div.textContent = str;
      return div.innerHTML;
    }

    function acceptHunk(index) {
      acceptedHunks.add(index);
      rejectedHunks.delete(index);
      render();
    }

    function rejectHunk(index) {
      rejectedHunks.add(index);
      acceptedHunks.delete(index);
      render();
    }

    function applyChanges() {
      const indices = Array.from(acceptedHunks);
      callTool('apply_note_edit', {
        edit_id: data.edit_id,
        accepted_indices: indices
      });
    }

    function cancelEdit() {
      callTool('list_notes_ui', {});
    }

    render();
  </script>
</body>
</html>`;
}

function createTasksListWidget(baseStyles: string, bridgeScript: string, dataJson: string): string {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>${baseStyles}</style>
</head>
<body>
  <div id="root"></div>
  <script>
    ${bridgeScript}

    function render() {
      const root = document.getElementById('root');
      const tasks = data.tasks || [];

      if (tasks.length === 0) {
        root.innerHTML = '<div class="empty">No tasks found</div>';
        return;
      }

      root.innerHTML = tasks.map(task => \`
        <div class="card">
          <div class="title">
            \${escapeHtml(task.title || 'Untitled')}
            <span class="badge \${getStatusBadge(task.status)}">\${task.status || 'pending'}</span>
          </div>
          <div class="preview">\${escapeHtml(task.description || '')}</div>
          <div class="actions">
            <button class="btn btn-primary" onclick="toggleTask('\${task.uid}', '\${task.status}')">
              \${task.status === 'completed' ? 'Mark Incomplete' : 'Mark Complete'}
            </button>
            <button class="btn btn-danger" onclick="deleteTask('\${task.uid}')">Delete</button>
          </div>
        </div>
      \`).join('');
    }

    function escapeHtml(str) {
      const div = document.createElement('div');
      div.textContent = str;
      return div.innerHTML;
    }

    function getStatusBadge(status) {
      switch (status) {
        case 'completed': return 'badge-success';
        case 'in_progress': return 'badge-warning';
        default: return 'badge-info';
      }
    }

    function toggleTask(uid, currentStatus) {
      const newStatus = currentStatus === 'completed' ? 'pending' : 'completed';
      callTool('update_task', { uid, status: newStatus });
    }

    function deleteTask(uid) {
      if (confirm('Delete this task?')) {
        callTool('delete_task', { uid });
      }
    }

    render();
  </script>
</body>
</html>`;
}

function createFallbackWidget(baseStyles: string, uri: string, dataJson: string): string {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>${baseStyles}</style>
</head>
<body>
  <div class="card">
    <div class="title">Widget: ${uri}</div>
    <div class="preview">Data: <pre>${dataJson}</pre></div>
  </div>
</body>
</html>`;
}
