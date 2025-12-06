"""
Unit tests for MCP-UI HTML templates.

Tests the template rendering functions to ensure they produce valid HTML
and correctly escape user input.
"""

import pytest
from unittest.mock import MagicMock
from html import escape


class TestNotesTemplates:
    """Test suite for notes HTML templates."""

    def _create_mock_note(
        self,
        uid: str = "test-uid-123",
        version: int = 1,
        title: str = "Test Note",
        content: str = "Test content",
        tags: list = None,
        status: str = None,
        updated_at: str = "2025-01-01T00:00:00Z",
        deleted_at: str = None,
    ):
        """Create a mock Note object for testing."""
        note = MagicMock()
        note.uid = uid
        note.version = version
        note.updated_at = updated_at
        note.deleted_at = deleted_at
        note.payload = {
            "title": title,
            "content": content,
        }
        if tags:
            note.payload["tags"] = tags
        if status:
            note.payload["status"] = status
        return note

    def test_render_notes_list_html_empty(self):
        """Test rendering empty notes list."""
        from toolbridge_mcp.ui.templates.notes import render_notes_list_html

        html = render_notes_list_html([])

        assert "<html>" in html
        assert "No notes found" in html
        assert "📝 Notes" in html

    def test_render_notes_list_html_single_note(self):
        """Test rendering a single note in list."""
        from toolbridge_mcp.ui.templates.notes import render_notes_list_html

        note = self._create_mock_note(
            uid="abc123",
            title="My Note",
            content="Some content here",
        )

        html = render_notes_list_html([note])

        assert "<html>" in html
        assert "My Note" in html
        assert "Some content here" in html
        assert "abc123" in html
        assert "Showing 1 note(s)" in html

    def test_render_notes_list_html_multiple_notes(self):
        """Test rendering multiple notes in list."""
        from toolbridge_mcp.ui.templates.notes import render_notes_list_html

        notes = [
            self._create_mock_note(uid="note1", title="First Note"),
            self._create_mock_note(uid="note2", title="Second Note"),
            self._create_mock_note(uid="note3", title="Third Note"),
        ]

        html = render_notes_list_html(notes)

        assert "First Note" in html
        assert "Second Note" in html
        assert "Third Note" in html
        assert "Showing 3 note(s)" in html

    def test_render_notes_list_html_truncates_long_content(self):
        """Test that long content is truncated with ellipsis."""
        from toolbridge_mcp.ui.templates.notes import render_notes_list_html

        long_content = "A" * 150  # More than 100 chars
        note = self._create_mock_note(content=long_content)

        html = render_notes_list_html([note])

        # Should truncate and add ellipsis
        assert "..." in html
        # Should not contain full content
        assert long_content not in html

    def test_render_notes_list_html_escapes_html(self):
        """Test that HTML in content is properly escaped."""
        from toolbridge_mcp.ui.templates.notes import render_notes_list_html

        xss_title = "<script>alert('xss')</script>"
        note = self._create_mock_note(title=xss_title)

        html = render_notes_list_html([note])

        # Raw script tag should NOT be in output
        assert "<script>" not in html
        # Escaped version should be present
        assert escape(xss_title) in html

    def test_render_note_detail_html(self):
        """Test rendering a single note detail view."""
        from toolbridge_mcp.ui.templates.notes import render_note_detail_html

        note = self._create_mock_note(
            uid="detail-uid-456",
            version=3,
            title="Detailed Note",
            content="Full content goes here",
            tags=["important", "work"],
            status="pinned",
        )

        html = render_note_detail_html(note)

        assert "<html>" in html
        assert "Detailed Note" in html
        assert "Full content goes here" in html
        assert "important" in html
        assert "work" in html
        assert "pinned" in html
        assert "Version: 3" in html
        assert "detail-uid-456" in html

    def test_render_note_detail_html_escapes_html(self):
        """Test that HTML in note detail is properly escaped."""
        from toolbridge_mcp.ui.templates.notes import render_note_detail_html

        xss_content = "<img src=x onerror=alert('xss')>"
        note = self._create_mock_note(content=xss_content)

        html = render_note_detail_html(note)

        assert "<img src=x" not in html
        assert escape(xss_content) in html


class TestTasksTemplates:
    """Test suite for tasks HTML templates."""

    def _create_mock_task(
        self,
        uid: str = "task-uid-123",
        version: int = 1,
        title: str = "Test Task",
        description: str = "Test description",
        status: str = "todo",
        priority: str = None,
        due_date: str = None,
        tags: list = None,
        updated_at: str = "2025-01-01T00:00:00Z",
        deleted_at: str = None,
    ):
        """Create a mock Task object for testing."""
        task = MagicMock()
        task.uid = uid
        task.version = version
        task.updated_at = updated_at
        task.deleted_at = deleted_at
        task.payload = {
            "title": title,
            "description": description,
            "status": status,
        }
        if priority:
            task.payload["priority"] = priority
        if due_date:
            task.payload["dueDate"] = due_date
        if tags:
            task.payload["tags"] = tags
        return task

    def test_render_tasks_list_html_empty(self):
        """Test rendering empty tasks list."""
        from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

        html = render_tasks_list_html([])

        assert "<html>" in html
        assert "No tasks found" in html
        assert "✅ Tasks" in html

    def test_render_tasks_list_html_single_task(self):
        """Test rendering a single task in list."""
        from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

        task = self._create_mock_task(
            uid="task123",
            title="My Task",
            status="in_progress",
            priority="high",
        )

        html = render_tasks_list_html([task])

        assert "<html>" in html
        assert "My Task" in html
        assert "Showing 1 task(s)" in html
        assert "🔄" in html  # in_progress icon
        assert "high" in html

    def test_render_tasks_list_html_status_icons(self):
        """Test that correct status icons are shown."""
        from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

        tasks = [
            self._create_mock_task(uid="t1", title="Todo", status="todo"),
            self._create_mock_task(uid="t2", title="In Progress", status="in_progress"),
            self._create_mock_task(uid="t3", title="Done", status="done"),
        ]

        html = render_tasks_list_html(tasks)

        assert "⬜" in html  # todo
        assert "🔄" in html  # in_progress
        assert "✅" in html  # done

    def test_render_tasks_list_html_priority_styling(self):
        """Test that priority classes are applied."""
        from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

        task = self._create_mock_task(priority="high")

        html = render_tasks_list_html([task])

        assert "priority-high" in html

    def test_render_tasks_list_html_with_due_date(self):
        """Test rendering task with due date."""
        from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

        task = self._create_mock_task(due_date="2025-12-31T23:59:59Z")

        html = render_tasks_list_html([task])

        assert "📅" in html
        assert "2025-12-31" in html

    def test_render_tasks_list_html_escapes_html(self):
        """Test that HTML in content is properly escaped."""
        from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

        xss_title = "<script>alert('xss')</script>"
        task = self._create_mock_task(title=xss_title)

        html = render_tasks_list_html([task])

        assert "<script>" not in html
        assert escape(xss_title) in html

    def test_render_task_detail_html(self):
        """Test rendering a single task detail view."""
        from toolbridge_mcp.ui.templates.tasks import render_task_detail_html

        task = self._create_mock_task(
            uid="detail-task-789",
            version=5,
            title="Detailed Task",
            description="Full task description here",
            status="in_progress",
            priority="medium",
            due_date="2025-06-15T10:00:00Z",
            tags=["sprint-1", "backend"],
        )

        html = render_task_detail_html(task)

        assert "<html>" in html
        assert "Detailed Task" in html
        assert "Full task description here" in html
        assert "in_progress" in html
        assert "medium" in html
        assert "sprint-1" in html
        assert "backend" in html
        assert "Version: 5" in html
        assert "2025-06-15" in html

    def test_render_task_detail_html_escapes_html(self):
        """Test that HTML in task detail is properly escaped."""
        from toolbridge_mcp.ui.templates.tasks import render_task_detail_html

        xss_desc = "onclick=\"alert('xss')\""
        task = self._create_mock_task(description=xss_desc)

        html = render_task_detail_html(task)

        assert "onclick=" not in html or escape(xss_desc) in html
