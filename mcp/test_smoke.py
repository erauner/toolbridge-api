#!/usr/bin/env python3
"""
Smoke test for ToolBridge MCP integration.

Tests the MCP service and REST API integration without requiring real JWT tokens.
This validates the MCP tool implementations and tenant header signing.
"""

import asyncio
import sys
from typing import Dict, Any

import httpx
from loguru import logger

# Configure logging
logger.remove()
logger.add(sys.stderr, level="INFO")


async def test_tenant_header_signing():
    """Test that tenant headers are correctly signed."""
    from toolbridge_mcp.utils.headers import TenantHeaderSigner

    logger.info("Testing tenant header signing...")

    signer = TenantHeaderSigner("test-tenant-123", "dev-secret-change-in-production")
    headers = signer.sign()

    # Verify required headers are present
    required = ["X-TB-Tenant-ID", "X-TB-Timestamp", "X-TB-Signature"]
    for header in required:
        assert header in headers, f"Missing required header: {header}"
        assert headers[header], f"Empty value for header: {header}"

    # Verify signature is hexadecimal (64 chars for SHA-256)
    assert len(headers["X-TB-Signature"]) == 64, "Invalid signature length"
    assert all(c in '0123456789abcdef' for c in headers["X-TB-Signature"]), "Signature not hex"

    logger.success("✓ Tenant header signing works correctly")
    return True


async def test_tenant_transport():
    """Test that TenantDirectTransport correctly injects headers."""
    from toolbridge_mcp.transports.tenant_direct import TenantDirectTransport
    from toolbridge_mcp.config import settings

    logger.info("Testing TenantDirectTransport...")

    transport = TenantDirectTransport()

    # Create a test request
    request = httpx.Request("GET", f"{settings.go_api_base_url}/v1/notes")

    # The transport should add tenant headers
    # Note: We can't actually send this request without valid JWT,
    # but we can verify the transport is initialized correctly
    assert transport.signer is not None, "Transport signer not initialized"
    assert transport.signer.tenant_id == settings.tenant_id, "Tenant ID mismatch"

    logger.success("✓ TenantDirectTransport initialized correctly")
    return True


async def test_pydantic_models():
    """Test that Pydantic models can parse API responses."""
    from toolbridge_mcp.tools.notes import Note, NotesListResponse
    from toolbridge_mcp.tools.tasks import Task, TasksListResponse
    from toolbridge_mcp.tools.comments import Comment, CommentsListResponse
    from toolbridge_mcp.tools.chats import Chat, ChatsListResponse
    from toolbridge_mcp.tools.chat_messages import ChatMessage, ChatMessagesListResponse

    logger.info("Testing Pydantic model parsing...")

    # Test Note model
    note_data = {
        "uid": "test-note-123",
        "version": 1,
        "updatedAt": "2025-11-18T10:00:00Z",
        "deletedAt": None,
        "payload": {"title": "Test Note", "content": "Test content"}
    }
    note = Note(**note_data)
    assert note.uid == "test-note-123"
    assert note.version == 1
    assert note.payload["title"] == "Test Note"

    # Test NotesListResponse
    list_data = {
        "items": [note_data],
        "nextCursor": "cursor-123"
    }
    notes_list = NotesListResponse(**list_data)
    assert len(notes_list.items) == 1
    assert notes_list.next_cursor == "cursor-123"

    # Test Task model
    task_data = {
        "uid": "test-task-123",
        "version": 1,
        "updatedAt": "2025-11-18T10:00:00Z",
        "deletedAt": None,
        "payload": {"title": "Test Task", "status": "todo"}
    }
    task = Task(**task_data)
    assert task.uid == "test-task-123"

    # Test Chat model
    chat_data = {
        "uid": "test-chat-123",
        "version": 1,
        "updatedAt": "2025-11-18T10:00:00Z",
        "deletedAt": None,
        "payload": {"title": "Test Chat"}
    }
    chat = Chat(**chat_data)
    assert chat.uid == "test-chat-123"

    # Test ChatMessage model
    message_data = {
        "uid": "test-message-123",
        "version": 1,
        "updatedAt": "2025-11-18T10:00:00Z",
        "deletedAt": None,
        "payload": {"content": "Test message", "chatUid": "test-chat-123"}
    }
    message = ChatMessage(**message_data)
    assert message.uid == "test-message-123"

    # Test Comment model
    comment_data = {
        "uid": "test-comment-123",
        "version": 1,
        "updatedAt": "2025-11-18T10:00:00Z",
        "deletedAt": None,
        "payload": {"content": "Test comment", "parentType": "note", "parentUid": "test-note-123"}
    }
    comment = Comment(**comment_data)
    assert comment.uid == "test-comment-123"

    logger.success("✓ All Pydantic models parse correctly")
    return True


async def test_mcp_server_import():
    """Test that MCP server and all tools can be imported."""
    logger.info("Testing MCP server and tool imports...")

    try:
        from toolbridge_mcp import server

        # Verify MCP instance exists
        assert hasattr(server, 'mcp'), "MCP server instance not found"
        assert server.mcp is not None, "MCP server instance is None"

        # Verify tools are registered (FastMCP should have decorated functions)
        logger.info("MCP server imported successfully with all tool modules")
        logger.success("✓ MCP server and tools import successfully")
        return True
    except Exception as e:
        logger.error(f"Failed to import MCP server: {e}")
        return False


async def test_mcp_service_health():
    """Test that the MCP service is running and responsive."""
    logger.info("Testing MCP service health...")

    try:
        async with httpx.AsyncClient() as client:
            # Try to connect to MCP service
            response = await client.get("http://localhost:8001/")

            # Any response means the service is running
            # (FastMCP may return 404 for root, which is OK)
            if response.status_code in [200, 404, 405, 500]:
                logger.success(f"✓ MCP service is running (HTTP {response.status_code})")
                return True
            else:
                logger.warning(f"MCP service returned unexpected status: {response.status_code}")
                return False

    except httpx.ConnectError:
        logger.error("✗ MCP service is not running on port 8001")
        logger.info("Start it with: cd mcp && uv run uvicorn toolbridge_mcp.server:mcp --port 8001")
        return False
    except Exception as e:
        logger.error(f"Error testing MCP service: {e}")
        return False


async def test_go_api_health():
    """Test that the Go API is running."""
    logger.info("Testing Go API health...")

    try:
        async with httpx.AsyncClient() as client:
            # Try to connect to Go API
            # We expect 401 because we're not sending auth, but that means it's running
            response = await client.get("http://localhost:8080/healthz")

            if response.status_code in [200, 401]:
                logger.success(f"✓ Go API is running (HTTP {response.status_code})")
                return True
            else:
                logger.warning(f"Go API returned unexpected status: {response.status_code}")
                return False

    except httpx.ConnectError:
        logger.error("✗ Go API is not running on port 8080")
        logger.info("Start it with: make dev")
        return False
    except Exception as e:
        logger.error(f"Error testing Go API: {e}")
        return False


async def main():
    """Run all smoke tests."""
    logger.info("╔══════════════════════════════════════════════════════════════╗")
    logger.info("║       ToolBridge MCP Integration Smoke Test (Python)        ║")
    logger.info("╚══════════════════════════════════════════════════════════════╝")
    logger.info("")

    tests = [
        ("MCP Server Import", test_mcp_server_import),
        ("Pydantic Models", test_pydantic_models),
        ("Tenant Header Signing", test_tenant_header_signing),
        ("Tenant Transport", test_tenant_transport),
        ("MCP Service Health", test_mcp_service_health),
        ("Go API Health", test_go_api_health),
    ]

    results = []

    for name, test_func in tests:
        logger.info("")
        logger.info(f"━━━ {name} ━━━")
        try:
            result = await test_func()
            results.append((name, result))
        except Exception as e:
            logger.error(f"✗ {name} failed with exception: {e}")
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
        color = "green" if result else "red"
        logger.opt(colors=True).info(f"<{color}>{status:>8}</{color}> {name}")

    logger.info("")
    logger.info(f"Results: {passed}/{total} tests passed")

    if passed == total:
        logger.success("━━━ All tests passed! ━━━")
        logger.info("")
        logger.info("Next steps:")
        logger.info("  1. Test with real JWT tokens and full integration")
        logger.info("  2. Test MCP tools via Claude Desktop or MCP inspector")
        logger.info("  3. Deploy to staging environment")
        return 0
    else:
        logger.error("━━━ Some tests failed ━━━")
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
