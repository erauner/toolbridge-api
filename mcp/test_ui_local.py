#!/usr/bin/env python3
"""
Local test for MCP-UI functionality.

Tests the UI templates and resource builder WITHOUT requiring:
- Go API backend
- OAuth authentication
- Network calls

This verifies:
1. HTML templates render correctly
2. UIResource structure is correct
3. XSS protection works
4. Text fallback is present
"""

import sys
from unittest.mock import MagicMock
from loguru import logger

# Configure logging
logger.remove()
logger.add(sys.stderr, level="INFO", format="<level>{message}</level>", colorize=True)


def create_mock_note(
    uid: str = "test-note-uid",
    version: int = 1,
    title: str = "Test Note",
    content: str = "Test content",
    tags: list = None,
    status: str = None,
):
    """Create a mock Note object."""
    note = MagicMock()
    note.uid = uid
    note.version = version
    note.updated_at = "2025-01-01T00:00:00Z"
    note.deleted_at = None
    note.payload = {"title": title, "content": content}
    if tags:
        note.payload["tags"] = tags
    if status:
        note.payload["status"] = status
    return note


def create_mock_task(
    uid: str = "test-task-uid",
    version: int = 1,
    title: str = "Test Task",
    description: str = "Test description",
    status: str = "todo",
    priority: str = None,
    due_date: str = None,
):
    """Create a mock Task object."""
    task = MagicMock()
    task.uid = uid
    task.version = version
    task.updated_at = "2025-01-01T00:00:00Z"
    task.deleted_at = None
    task.payload = {"title": title, "description": description, "status": status}
    if priority:
        task.payload["priority"] = priority
    if due_date:
        task.payload["dueDate"] = due_date
    return task


def test_notes_templates():
    """Test notes HTML template rendering."""
    from toolbridge_mcp.ui.templates.notes import render_notes_list_html, render_note_detail_html

    logger.info("━━━ Testing Notes Templates ━━━")

    # 1. Test empty list
    logger.info("1. Testing empty notes list...")
    html = render_notes_list_html([])
    assert "<html>" in html
    assert "No notes found" in html
    logger.success("✓ Empty list renders correctly")

    # 2. Test list with notes
    logger.info("2. Testing notes list with items...")
    notes = [
        create_mock_note(uid="note-1", title="First Note", content="Content 1"),
        create_mock_note(uid="note-2", title="Second Note", content="Content 2"),
    ]
    html = render_notes_list_html(notes)
    assert "First Note" in html
    assert "Second Note" in html
    assert "Showing 2 note(s)" in html
    logger.success("✓ Notes list renders correctly")

    # 3. Test XSS protection
    logger.info("3. Testing XSS protection...")
    xss_note = create_mock_note(title="<script>alert('xss')</script>")
    html = render_notes_list_html([xss_note])
    assert "<script>" not in html
    assert "&lt;script&gt;" in html
    logger.success("✓ XSS is properly escaped")

    # 4. Test detail view
    logger.info("4. Testing note detail view...")
    note = create_mock_note(
        uid="detail-note",
        title="Detailed Note",
        content="Full content here",
        tags=["tag1", "tag2"],
        status="pinned",
    )
    html = render_note_detail_html(note)
    assert "Detailed Note" in html
    assert "Full content here" in html
    assert "tag1" in html
    assert "pinned" in html
    logger.success("✓ Note detail renders correctly")

    logger.success("━━━ Notes Templates: ALL PASSED ━━━")
    return True


def test_tasks_templates():
    """Test tasks HTML template rendering."""
    from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html, render_task_detail_html

    logger.info("━━━ Testing Tasks Templates ━━━")

    # 1. Test empty list
    logger.info("1. Testing empty tasks list...")
    html = render_tasks_list_html([])
    assert "<html>" in html
    assert "No tasks found" in html
    logger.success("✓ Empty list renders correctly")

    # 2. Test list with tasks
    logger.info("2. Testing tasks list with items...")
    tasks = [
        create_mock_task(uid="task-1", title="Todo Task", status="todo"),
        create_mock_task(uid="task-2", title="In Progress", status="in_progress"),
        create_mock_task(uid="task-3", title="Done Task", status="done", priority="high"),
    ]
    html = render_tasks_list_html(tasks)
    assert "Todo Task" in html
    assert "In Progress" in html
    assert "Done Task" in html
    assert "⬜" in html  # todo icon
    assert "🔄" in html  # in_progress icon
    assert "✅" in html  # done icon
    assert "priority-high" in html
    logger.success("✓ Tasks list renders correctly with status icons")

    # 3. Test task with due date
    logger.info("3. Testing task with due date...")
    task = create_mock_task(due_date="2025-12-31T23:59:59Z")
    html = render_tasks_list_html([task])
    assert "📅" in html
    assert "2025-12-31" in html
    logger.success("✓ Due date renders correctly")

    # 4. Test detail view
    logger.info("4. Testing task detail view...")
    task = create_mock_task(
        uid="detail-task",
        title="Detailed Task",
        description="Full description",
        status="in_progress",
        priority="medium",
        due_date="2025-06-15T10:00:00Z",
    )
    html = render_task_detail_html(task)
    assert "Detailed Task" in html
    assert "Full description" in html
    assert "in_progress" in html
    assert "medium" in html
    logger.success("✓ Task detail renders correctly")

    logger.success("━━━ Tasks Templates: ALL PASSED ━━━")
    return True


def test_ui_resources():
    """Test UI resource builder."""
    from toolbridge_mcp.ui.resources import build_ui_with_text
    from mcp.types import TextContent

    logger.info("━━━ Testing UI Resources ━━━")

    # 1. Test basic structure
    logger.info("1. Testing basic UIResource structure...")
    result = build_ui_with_text(
        uri="ui://toolbridge/test",
        html="<p>Test HTML</p>",
        text_summary="Test summary",
    )
    assert isinstance(result, list)
    assert len(result) == 2
    logger.success("✓ Returns list with 2 elements")

    # 2. Test TextContent
    logger.info("2. Testing TextContent (text fallback)...")
    assert isinstance(result[0], TextContent)
    assert result[0].type == "text"
    assert result[0].text == "Test summary"
    logger.success("✓ TextContent is first element with correct text")

    # 3. Test UIResource
    logger.info("3. Testing UIResource structure...")
    ui_resource = result[1]
    assert ui_resource.type == "resource"
    assert ui_resource.resource.mimeType == "text/html"
    assert "<p>Test HTML</p>" in ui_resource.resource.text
    logger.success("✓ UIResource has correct MIME type and HTML content")

    # 4. Test URI
    logger.info("4. Testing URI in UIResource...")
    assert "ui://toolbridge/test" in str(ui_resource.resource.uri)
    logger.success("✓ UIResource has correct URI")

    # 5. Test with full HTML document
    logger.info("5. Testing with full HTML document...")
    full_html = """
    <html>
    <head><title>Test</title></head>
    <body><h1>Hello World</h1></body>
    </html>
    """
    result = build_ui_with_text(
        uri="ui://toolbridge/full",
        html=full_html,
        text_summary="Full document",
    )
    assert "<h1>Hello World</h1>" in result[1].resource.text
    logger.success("✓ Full HTML document preserved")

    logger.success("━━━ UI Resources: ALL PASSED ━━━")
    return True


def test_end_to_end_rendering():
    """Test end-to-end: mock data → template → UIResource."""
    from toolbridge_mcp.ui.resources import build_ui_with_text
    from toolbridge_mcp.ui.templates.notes import render_notes_list_html
    from toolbridge_mcp.ui.templates.tasks import render_tasks_list_html

    logger.info("━━━ Testing End-to-End Rendering ━━━")

    # 1. Notes end-to-end
    logger.info("1. Testing notes end-to-end pipeline...")
    notes = [
        create_mock_note(uid="e2e-note-1", title="E2E Note 1"),
        create_mock_note(uid="e2e-note-2", title="E2E Note 2"),
    ]
    html = render_notes_list_html(notes)
    result = build_ui_with_text(
        uri="ui://toolbridge/notes/list",
        html=html,
        text_summary=f"Showing {len(notes)} notes",
    )

    # Verify the full pipeline
    assert result[0].text == "Showing 2 notes"
    assert "E2E Note 1" in result[1].resource.text
    assert "E2E Note 2" in result[1].resource.text
    assert result[1].resource.mimeType == "text/html"
    logger.success("✓ Notes pipeline works: mock data → HTML → UIResource")

    # 2. Tasks end-to-end
    logger.info("2. Testing tasks end-to-end pipeline...")
    tasks = [
        create_mock_task(uid="e2e-task-1", title="E2E Task 1", status="todo"),
        create_mock_task(uid="e2e-task-2", title="E2E Task 2", status="done", priority="high"),
    ]
    html = render_tasks_list_html(tasks)
    result = build_ui_with_text(
        uri="ui://toolbridge/tasks/list",
        html=html,
        text_summary=f"Showing {len(tasks)} tasks",
    )

    # Verify the full pipeline
    assert result[0].text == "Showing 2 tasks"
    assert "E2E Task 1" in result[1].resource.text
    assert "E2E Task 2" in result[1].resource.text
    assert "priority-high" in result[1].resource.text
    logger.success("✓ Tasks pipeline works: mock data → HTML → UIResource")

    logger.success("━━━ End-to-End Rendering: ALL PASSED ━━━")
    return True


def main():
    """Run all local UI tests."""
    logger.info("╔══════════════════════════════════════════════════════════════╗")
    logger.info("║       MCP-UI Local Test Suite (No Auth Required)            ║")
    logger.info("╚══════════════════════════════════════════════════════════════╝")
    logger.info("")

    tests = [
        ("Notes Templates", test_notes_templates),
        ("Tasks Templates", test_tasks_templates),
        ("UI Resources", test_ui_resources),
        ("End-to-End Rendering", test_end_to_end_rendering),
    ]

    results = []
    for name, test_func in tests:
        logger.info("")
        try:
            result = test_func()
            results.append((name, result))
        except Exception as e:
            logger.error(f"✗ {name} FAILED: {e}")
            import traceback
            traceback.print_exc()
            results.append((name, False))

    # Summary
    logger.info("")
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    logger.info("Test Summary:")
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

    passed = sum(1 for _, result in results if result)
    total = len(results)

    for name, result in results:
        status = "✓ PASS" if result else "✗ FAIL"
        logger.info(f"  {status:>8} {name}")

    logger.info("")
    logger.info(f"Results: {passed}/{total} test suites passed")

    if passed == total:
        logger.success("━━━ ALL LOCAL UI TESTS PASSED! ━━━")
        logger.info("")
        logger.info("Verified:")
        logger.info("  ✓ Notes templates render HTML correctly")
        logger.info("  ✓ Tasks templates render HTML correctly")
        logger.info("  ✓ XSS protection via HTML escaping")
        logger.info("  ✓ UIResource structure is correct")
        logger.info("  ✓ TextContent fallback is present")
        logger.info("  ✓ End-to-end pipeline: data → HTML → UIResource")
        return 0
    else:
        logger.error("━━━ SOME TESTS FAILED ━━━")
        return 1


if __name__ == "__main__":
    sys.exit(main())
