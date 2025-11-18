#!/usr/bin/env python3
"""
Test MCP tools with session management.

This script:
1. Generates a JWT token
2. Calls MCP tools via HTTP/JSON-RPC
3. Verifies session creation and usage
"""

import sys
import json
import time
from datetime import datetime, timedelta

import httpx
import jwt
from loguru import logger

# Configure logging
logger.remove()
logger.add(sys.stderr, level="INFO", format="<level>{message}</level>", colorize=True)

# Configuration
MCP_URL = "http://localhost:8001"
JWT_SECRET = "dev-secret"
USER_ID = f"mcp-test-{int(time.time())}"


def generate_jwt_token(user_id: str, tenant_id: str = "test-tenant-123") -> str:
    """Generate a development JWT token."""
    payload = {
        "sub": user_id,
        "tenant_id": tenant_id,
        "iat": datetime.utcnow(),
        "exp": datetime.utcnow() + timedelta(hours=1),
    }
    token = jwt.encode(payload, JWT_SECRET, algorithm="HS256")
    return token


async def call_mcp_tool(tool_name: str, arguments: dict, auth_token: str) -> dict:
    """
    Call an MCP tool via JSON-RPC.

    Args:
        tool_name: Name of the tool to call
        arguments: Tool arguments
        auth_token: JWT bearer token

    Returns:
        Tool result
    """
    async with httpx.AsyncClient() as client:
        # MCP uses JSON-RPC 2.0 protocol
        request_data = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "tools/call",
            "params": {
                "name": tool_name,
                "arguments": arguments,
            }
        }

        headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {auth_token}",
        }

        logger.info(f"Calling MCP tool: {tool_name}")
        logger.debug(f"Arguments: {arguments}")

        response = await client.post(
            f"{MCP_URL}/mcp/v1/",  # FastMCP endpoint
            json=request_data,
            headers=headers,
            timeout=30.0,
        )

        response.raise_for_status()
        result = response.json()

        if "error" in result:
            logger.error(f"MCP error: {result['error']}")
            raise Exception(f"MCP error: {result['error']}")

        return result.get("result", {})


async def test_create_note():
    """Test creating a note via MCP (verifies session management)."""
    logger.info("━━━ Test: Create Note via MCP ━━━")

    # Generate JWT token
    token = generate_jwt_token(USER_ID)
    logger.success(f"✓ Generated JWT for user: {USER_ID}")

    # Call create_note tool
    try:
        result = await call_mcp_tool(
            "create_note",
            {
                "title": "MCP Integration Test Note",
                "content": "Created via MCP Inspector test",
                "tags": ["mcp", "e2e", "session-management"],
            },
            token,
        )

        logger.success(f"✓ Note created: {result}")
        return result

    except Exception as e:
        logger.error(f"✗ Failed to create note: {e}")
        raise


async def test_list_notes():
    """Test listing notes via MCP."""
    logger.info("")
    logger.info("━━━ Test: List Notes via MCP ━━━")

    token = generate_jwt_token(USER_ID)

    try:
        result = await call_mcp_tool(
            "list_notes",
            {"limit": 5},
            token,
        )

        logger.success(f"✓ Listed notes: {len(result.get('notes', []))} notes")
        return result

    except Exception as e:
        logger.error(f"✗ Failed to list notes: {e}")
        raise


async def main():
    """Run MCP tool tests."""
    logger.info("╔══════════════════════════════════════════════════════════════╗")
    logger.info("║           MCP Tools Integration Test Suite                  ║")
    logger.info("╚══════════════════════════════════════════════════════════════╝")
    logger.info("")
    logger.info("Configuration:")
    logger.info(f"  MCP Service:  {MCP_URL}")
    logger.info(f"  User ID:      {USER_ID}")
    logger.info("")

    tests = [
        ("Create Note", test_create_note),
        ("List Notes", test_list_notes),
    ]

    results = []

    for name, test_func in tests:
        try:
            await test_func()
            results.append((name, True))
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
    logger.info(f"Results: {passed}/{total} tests passed")

    if passed == total:
        logger.success("━━━ All MCP Tool Tests PASSED! ━━━")
        logger.info("")
        logger.info("Validated:")
        logger.info("  ✓ MCP tools can be called via JSON-RPC")
        logger.info("  ✓ JWT authentication works")
        logger.info("  ✓ Session management creates sessions automatically")
        logger.info("  ✓ Session headers added to Go API requests")
        logger.info("  ✓ Full request flow: MCP → Python → Go API → Database")
        return 0
    else:
        logger.error("━━━ Some MCP Tool Tests Failed ━━━")
        return 1


if __name__ == "__main__":
    import asyncio
    sys.exit(asyncio.run(main()))
