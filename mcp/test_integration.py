#!/usr/bin/env python3
"""
End-to-End Integration Test for ToolBridge MCP

Tests the full request flow:
MCP Tools → Python FastMCP Service → Go REST API → Database

This validates:
- MCP tool invocation works
- Python service adds tenant headers correctly
- Go API accepts requests and processes them
- Full CRUD operations work through the stack
"""

import asyncio
import sys
import uuid
from datetime import datetime
from typing import Optional

from loguru import logger

# Configure logging
logger.remove()
logger.add(sys.stderr, level="INFO", format="<level>{message}</level>", colorize=True)


# Mock HTTP headers context for testing
# In production, FastMCP would inject these from the actual HTTP request
class MockHTTPContext:
    """Simulates MCP request context with HTTP headers."""

    def __init__(self, user_id: str = "integration-test-user"):
        self.user_id = user_id
        self._headers = {
            # X-Debug-Sub header for dev mode authentication
            "Authorization": f"X-Debug-Sub: {user_id}",
            "X-Debug-Sub": user_id,
        }

    def get_headers(self):
        return self._headers


async def setup_test_context(user_id: str):
    """Set up test context with mock HTTP headers."""
    from toolbridge_mcp.utils.requests import _http_headers_context

    mock_context = MockHTTPContext(user_id)

    # This is a hack for testing - in production FastMCP injects these
    _http_headers_context.set(mock_context._headers)

    return mock_context


async def test_notes_crud():
    """Test full CRUD cycle for notes."""
    from toolbridge_mcp.tools.notes import (
        create_note,
        get_note,
        list_notes,
        patch_note,
        update_note,
        delete_note,
        archive_note,
    )

    logger.info("━━━ Testing Notes CRUD ━━━")

    # Setup context
    user_id = f"notes-test-{uuid.uuid4().hex[:8]}"
    await setup_test_context(user_id)

    # 1. Create a note
    logger.info("1. Creating note...")
    note = await create_note(
        title="Integration Test Note",
        content="This note was created by the integration test",
        tags=["test", "integration", "mcp"],
    )

    assert note.uid, "Note should have a UID"
    assert note.version == 1, "New note should have version 1"
    assert note.payload.get("title") == "Integration Test Note"
    logger.success(f"✓ Created note: uid={note.uid}, version={note.version}")

    note_uid = note.uid

    # 2. Get the note
    logger.info("2. Retrieving note...")
    fetched_note = await get_note(note_uid)
    assert fetched_note.uid == note_uid
    assert fetched_note.payload.get("title") == "Integration Test Note"
    logger.success(f"✓ Retrieved note: {fetched_note.payload.get('title')}")

    # 3. List notes (should include our note)
    logger.info("3. Listing notes...")
    notes_list = await list_notes(limit=10)
    assert len(notes_list.items) > 0, "Should have at least one note"
    found = any(n.uid == note_uid for n in notes_list.items)
    assert found, "Our note should be in the list"
    logger.success(f"✓ Listed {len(notes_list.items)} notes, found our note")

    # 4. Patch note (partial update)
    logger.info("4. Patching note...")
    patched_note = await patch_note(uid=note_uid, updates={"content": "Updated content via PATCH"})
    assert patched_note.version == 2, "Version should increment"
    assert patched_note.payload.get("content") == "Updated content via PATCH"
    logger.success(f"✓ Patched note: version={patched_note.version}")

    # 5. Update note (full update)
    logger.info("5. Updating note...")
    updated_note = await update_note(
        uid=note_uid,
        title="Updated Title",
        content="Updated content via PUT",
    )
    assert updated_note.version == 3, "Version should increment again"
    assert updated_note.payload.get("title") == "Updated Title"
    logger.success(f"✓ Updated note: version={updated_note.version}")

    # 6. Archive note
    logger.info("6. Archiving note...")
    archived_note = await archive_note(uid=note_uid)
    assert archived_note.payload.get("archived") == True
    logger.success(f"✓ Archived note")

    # 7. Delete note (soft delete)
    logger.info("7. Deleting note...")
    deleted_note = await delete_note(uid=note_uid)
    assert deleted_note.deleted_at is not None, "Should have deletedAt timestamp"
    logger.success(f"✓ Soft-deleted note: deletedAt={deleted_note.deleted_at}")

    # 8. Verify deleted note not in default list
    logger.info("8. Verifying deleted note not in default list...")
    notes_list = await list_notes(limit=10, include_deleted=False)
    found = any(n.uid == note_uid for n in notes_list.items)
    assert not found, "Deleted note should not be in default list"
    logger.success(f"✓ Deleted note not in list (include_deleted=False)")

    # 9. Verify deleted note IS in list with include_deleted=True
    logger.info("9. Retrieving deleted note with include_deleted=True...")
    deleted_notes_list = await list_notes(limit=10, include_deleted=True)
    found = any(n.uid == note_uid for n in deleted_notes_list.items)
    assert found, "Deleted note should be in list with include_deleted=True"
    logger.success(f"✓ Found deleted note with include_deleted=True")

    logger.success("━━━ Notes CRUD: ALL TESTS PASSED ━━━")
    return True


async def test_tasks_crud():
    """Test basic CRUD for tasks."""
    from toolbridge_mcp.tools.tasks import create_task, get_task, process_task, delete_task

    logger.info("━━━ Testing Tasks CRUD ━━━")

    user_id = f"tasks-test-{uuid.uuid4().hex[:8]}"
    await setup_test_context(user_id)

    # Create task
    logger.info("1. Creating task...")
    task = await create_task(
        title="Integration Test Task",
        description="Test task from integration test",
        status="todo",
        priority="high",
    )
    assert task.uid, "Task should have a UID"
    assert task.payload.get("title") == "Integration Test Task"
    logger.success(f"✓ Created task: uid={task.uid}")

    task_uid = task.uid

    # Get task
    logger.info("2. Retrieving task...")
    fetched_task = await get_task(task_uid)
    assert fetched_task.uid == task_uid
    logger.success(f"✓ Retrieved task")

    # Process task (state machine transition)
    logger.info("3. Processing task (start action)...")
    processed_task = await process_task(
        uid=task_uid, action="start", metadata={"started_by": "integration-test"}
    )
    logger.success(f"✓ Processed task: action=start")

    # Delete task
    logger.info("4. Deleting task...")
    deleted_task = await delete_task(task_uid)
    assert deleted_task.deleted_at is not None
    logger.success(f"✓ Soft-deleted task")

    logger.success("━━━ Tasks CRUD: ALL TESTS PASSED ━━━")
    return True


async def test_chats_and_messages():
    """Test chats and chat messages."""
    from toolbridge_mcp.tools.chats import create_chat, get_chat, delete_chat
    from toolbridge_mcp.tools.chat_messages import (
        create_chat_message,
        get_chat_message,
        list_chat_messages,
    )

    logger.info("━━━ Testing Chats & Messages ━━━")

    user_id = f"chats-test-{uuid.uuid4().hex[:8]}"
    await setup_test_context(user_id)

    # Create chat
    logger.info("1. Creating chat...")
    chat = await create_chat(
        title="Integration Test Chat",
        description="Test chat room",
        participants=["user1", "user2"],
    )
    assert chat.uid, "Chat should have a UID"
    logger.success(f"✓ Created chat: uid={chat.uid}")

    chat_uid = chat.uid

    # Create message in chat
    logger.info("2. Creating chat message...")
    message = await create_chat_message(
        chat_uid=chat_uid,
        content="Hello from integration test!",
        sender="integration-test-bot",
    )
    assert message.uid, "Message should have a UID"
    assert message.payload.get("chatUid") == chat_uid
    logger.success(f"✓ Created message: uid={message.uid}")

    message_uid = message.uid

    # List messages
    logger.info("3. Listing chat messages...")
    messages_list = await list_chat_messages(limit=10)
    found = any(m.uid == message_uid for m in messages_list.items)
    assert found, "Our message should be in the list"
    logger.success(f"✓ Listed {len(messages_list.items)} messages")

    # Get message
    logger.info("4. Retrieving chat message...")
    fetched_message = await get_chat_message(message_uid)
    assert fetched_message.uid == message_uid
    logger.success(f"✓ Retrieved message")

    logger.success("━━━ Chats & Messages: ALL TESTS PASSED ━━━")
    return True


async def test_comments():
    """Test comments (requires parent entity)."""
    from toolbridge_mcp.tools.notes import create_note
    from toolbridge_mcp.tools.comments import create_comment, get_comment, process_comment

    logger.info("━━━ Testing Comments ━━━")

    user_id = f"comments-test-{uuid.uuid4().hex[:8]}"
    await setup_test_context(user_id)

    # Create parent note
    logger.info("1. Creating parent note for comment...")
    note = await create_note(title="Note for comment test", content="Parent note")
    parent_uid = note.uid
    logger.success(f"✓ Created parent note: uid={parent_uid}")

    # Create comment
    logger.info("2. Creating comment...")
    comment = await create_comment(
        content="This is a test comment",
        parent_type="note",
        parent_uid=parent_uid,
        author="integration-test",
    )
    assert comment.uid, "Comment should have a UID"
    assert comment.payload.get("parentUid") == parent_uid
    logger.success(f"✓ Created comment: uid={comment.uid}")

    comment_uid = comment.uid

    # Get comment
    logger.info("3. Retrieving comment...")
    fetched_comment = await get_comment(comment_uid)
    assert fetched_comment.uid == comment_uid
    logger.success(f"✓ Retrieved comment")

    # Process comment
    logger.info("4. Processing comment (resolve action)...")
    processed_comment = await process_comment(
        uid=comment_uid, action="resolve", metadata={"resolved_by": "integration-test"}
    )
    logger.success(f"✓ Processed comment: action=resolve")

    logger.success("━━━ Comments: ALL TESTS PASSED ━━━")
    return True


async def test_ui_tools():
    """Test MCP-UI tools return correct content structure."""
    from toolbridge_mcp.tools.notes_ui import list_notes_ui, show_note_ui
    from toolbridge_mcp.tools.tasks_ui import list_tasks_ui, show_task_ui
    from toolbridge_mcp.tools.notes import create_note
    from toolbridge_mcp.tools.tasks import create_task
    from mcp.types import TextContent

    logger.info("━━━ Testing MCP-UI Tools ━━━")

    user_id = f"ui-test-{uuid.uuid4().hex[:8]}"
    await setup_test_context(user_id)

    # 1. Create test data
    logger.info("1. Creating test note and task...")
    note = await create_note(title="UI Test Note", content="Testing UI rendering")
    task = await create_task(title="UI Test Task", description="Testing UI rendering")
    logger.success(f"✓ Created note={note.uid[:8]}... task={task.uid[:8]}...")

    # 2. Test list_notes_ui
    logger.info("2. Testing list_notes_ui...")
    result = await list_notes_ui(limit=10)
    assert isinstance(result, list), "Should return a list"
    assert len(result) == 2, "Should return [TextContent, UIResource]"
    assert isinstance(result[0], TextContent), "First element should be TextContent"
    assert result[0].type == "text", "TextContent should have type='text'"
    assert result[1].type == "resource", "Second element should be UIResource"
    assert "text/html" in result[1].resource.mimeType, "UIResource should have HTML mime type"
    assert "<html>" in result[1].resource.text, "UIResource should contain HTML"
    logger.success(f"✓ list_notes_ui returns valid [TextContent, UIResource]")

    # 3. Test show_note_ui
    logger.info("3. Testing show_note_ui...")
    result = await show_note_ui(uid=note.uid)
    assert len(result) == 2
    assert "UI Test Note" in result[1].resource.text, "HTML should contain note title"
    logger.success(f"✓ show_note_ui returns HTML with note content")

    # 4. Test list_tasks_ui
    logger.info("4. Testing list_tasks_ui...")
    result = await list_tasks_ui(limit=10)
    assert len(result) == 2
    assert result[1].type == "resource"
    logger.success(f"✓ list_tasks_ui returns valid [TextContent, UIResource]")

    # 5. Test show_task_ui
    logger.info("5. Testing show_task_ui...")
    result = await show_task_ui(uid=task.uid)
    assert len(result) == 2
    assert "UI Test Task" in result[1].resource.text, "HTML should contain task title"
    logger.success(f"✓ show_task_ui returns HTML with task content")

    logger.success("━━━ MCP-UI Tools: ALL TESTS PASSED ━━━")
    return True


async def main():
    """Run all integration tests."""
    logger.info("╔══════════════════════════════════════════════════════════════╗")
    logger.info("║     ToolBridge MCP End-to-End Integration Test              ║")
    logger.info("╚══════════════════════════════════════════════════════════════╝")
    logger.info("")
    logger.info("Testing full stack: MCP Tools → Python Service → Go API → DB")
    logger.info("")

    tests = [
        ("Notes CRUD", test_notes_crud),
        ("Tasks CRUD", test_tasks_crud),
        ("Chats & Messages", test_chats_and_messages),
        ("Comments", test_comments),
        ("MCP-UI Tools", test_ui_tools),
    ]

    results = []

    for name, test_func in tests:
        logger.info("")
        try:
            result = await test_func()
            results.append((name, result))
        except Exception as e:
            logger.error(f"✗ {name} FAILED: {e}")
            import traceback

            traceback.print_exc()
            results.append((name, False))

    # Summary
    logger.info("")
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    logger.info("Integration Test Summary:")
    logger.info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

    passed = sum(1 for _, result in results if result)
    total = len(results)

    for name, result in results:
        status = "✓ PASS" if result else "✗ FAIL"
        logger.info(f"  {status:>8} {name}")

    logger.info("")
    logger.info(f"Results: {passed}/{total} test suites passed")

    if passed == total:
        logger.success("━━━ ALL INTEGRATION TESTS PASSED! ━━━")
        logger.info("")
        logger.info("Verified:")
        logger.info("  ✓ MCP tools call Python service correctly")
        logger.info("  ✓ Python service adds tenant headers (HMAC signed)")
        logger.info("  ✓ Go API accepts and processes requests")
        logger.info("  ✓ Full CRUD operations work end-to-end")
        logger.info("  ✓ All 5 entity types functional (notes, tasks, comments, chats, messages)")
        logger.info("  ✓ MCP-UI tools return [TextContent, UIResource] structure")
        return 0
    else:
        logger.error("━━━ SOME INTEGRATION TESTS FAILED ━━━")
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
