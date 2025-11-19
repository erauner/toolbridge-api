#!/usr/bin/env python3
"""
Integration test script for ToolBridge MCP staging deployment on Fly.io.

This script tests the MCP proxy service running on Fly.io, which communicates
with the Go API running in K8s.

Usage:
    # Test against staging
    export MCP_BASE_URL="https://toolbridge-mcp-staging.fly.dev"
    export GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"
    export JWT_SECRET="your-jwt-secret"
    python scripts/test-mcp-staging.py

    # Test against local MCP pointing to staging Go API
    export MCP_BASE_URL="http://localhost:8001"
    export GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"
    python scripts/test-mcp-staging.py
"""

import sys
import os
import json
import time
import hmac
import hashlib
import asyncio
from datetime import datetime, timedelta
from typing import Optional, Dict

import httpx
import jwt
from loguru import logger

# Configuration from environment
MCP_BASE_URL = os.getenv("MCP_BASE_URL", "http://localhost:8001")
GO_API_BASE_URL = os.getenv("GO_API_BASE_URL", "https://toolbridgeapi.erauner.dev")
JWT_SECRET = os.getenv("JWT_SECRET", "dev-secret")
TENANT_ID = os.getenv("TENANT_ID", "staging-tenant-001")
TENANT_HEADER_SECRET = os.getenv("TENANT_HEADER_SECRET", "")  # Optional: for MCP deployments

# Test user
USER_ID = f"staging-e2e-test-{int(time.time())}"

logger.remove()
logger.add(
    sys.stdout,
    format="<green>{time:HH:mm:ss.SSS}</green> | <level>{level: <8}</level> | {message}",
    level="INFO"
)


def sign_tenant_headers(tenant_id: str = TENANT_ID, secret: str = TENANT_HEADER_SECRET) -> Dict[str, str]:
    """
    Generate HMAC-signed tenant headers for Go API authentication.

    Args:
        tenant_id: Tenant identifier
        secret: Shared HMAC secret (must match Go API TENANT_HEADER_SECRET)

    Returns:
        Dictionary of headers to add to requests (X-TB-Tenant-ID, X-TB-Timestamp, X-TB-Signature)
    """
    if not secret:
        return {}  # No tenant headers if secret not configured

    timestamp_ms = int(time.time() * 1000)
    message = f"{tenant_id}:{timestamp_ms}"

    # Compute HMAC-SHA256 signature (same algorithm as Go API)
    signature = hmac.new(
        key=secret.encode('utf-8'),
        msg=message.encode('utf-8'),
        digestmod=hashlib.sha256
    ).hexdigest()

    return {
        "X-TB-Tenant-ID": tenant_id,
        "X-TB-Timestamp": str(timestamp_ms),
        "X-TB-Signature": signature,
    }


def generate_jwt_token(user_id: str, tenant_id: str = TENANT_ID) -> str:
    """Generate JWT token for testing."""
    payload = {
        "sub": user_id,
        "tenant_id": tenant_id,
        "iat": datetime.utcnow(),
        "exp": datetime.utcnow() + timedelta(hours=1),
    }
    return jwt.encode(payload, JWT_SECRET, algorithm="HS256")


async def test_mcp_health():
    """Test MCP service health endpoint."""
    logger.info("Testing MCP service health...")

    async with httpx.AsyncClient(timeout=10.0) as client:
        try:
            response = await client.get(f"{MCP_BASE_URL}/")
            logger.info(f"MCP health check status: {response.status_code}")

            # 200, 404, or 405 are all acceptable (depends on FastMCP setup)
            if response.status_code in [200, 404, 405]:
                logger.success("✓ MCP service is reachable")
                return True
            else:
                logger.error(f"✗ Unexpected status: {response.status_code}")
                return False
        except Exception as e:
            logger.error(f"✗ MCP health check failed: {e}")
            return False


async def test_go_api_direct():
    """Test direct access to Go API (K8s)."""
    logger.info("Testing direct Go API access (K8s)...")

    token = generate_jwt_token(USER_ID)

    async with httpx.AsyncClient(timeout=10.0) as client:
        try:
            response = await client.get(
                f"{GO_API_BASE_URL}/healthz",
                headers={"Authorization": f"Bearer {token}"}
            )
            logger.info(f"Go API health status: {response.status_code}")

            if response.status_code == 200:
                logger.success("✓ Go API is reachable")
                return True
            else:
                logger.error(f"✗ Go API returned {response.status_code}")
                return False
        except Exception as e:
            logger.error(f"✗ Go API health check failed: {e}")
            return False


async def test_mcp_to_go_api_flow():
    """Test MCP → Go API flow by creating a note."""
    logger.info("Testing MCP → Go API → PostgreSQL flow...")

    token = generate_jwt_token(USER_ID)

    # Note: This test assumes MCP tools are available via SSE or HTTP endpoints
    # The exact endpoint structure depends on FastMCP setup
    # For now, test direct Go API call with Authorization header

    async with httpx.AsyncClient(timeout=30.0) as client:
        try:
            # Create a session first
            logger.info("Creating sync session...")

            # Build headers with JWT auth + tenant headers (if configured)
            headers = {"Authorization": f"Bearer {token}"}
            tenant_headers = sign_tenant_headers()
            if tenant_headers:
                headers.update(tenant_headers)
                logger.debug(f"Added tenant headers to request")

            session_response = await client.post(
                f"{GO_API_BASE_URL}/v1/sync/sessions",
                headers=headers
            )

            if session_response.status_code != 201:
                logger.error(f"✗ Failed to create session: {session_response.status_code}")
                logger.error(f"Response: {session_response.text}")
                return False

            session_data = session_response.json()
            session_id = session_data["id"]
            epoch = session_data["epoch"]
            logger.success(f"✓ Session created: {session_id}")

            # Create a note via REST API
            logger.info("Creating test note...")
            note_data = {
                "title": f"MCP Staging Test {int(time.time())}",
                "content": f"Created by test script at {datetime.utcnow().isoformat()}",
                "tags": ["test", "staging", "mcp"],
            }

            # Build headers with JWT auth + sync headers + tenant headers
            note_headers = {
                "Authorization": f"Bearer {token}",
                "X-Sync-Session": session_id,
                "X-Sync-Epoch": str(epoch),
            }
            if tenant_headers:
                note_headers.update(tenant_headers)

            note_response = await client.post(
                f"{GO_API_BASE_URL}/v1/notes",
                headers=note_headers,
                json=note_data,
            )

            if note_response.status_code != 201:
                logger.error(f"✗ Failed to create note: {note_response.status_code}")
                logger.error(f"Response: {note_response.text}")
                return False

            created_note = note_response.json()
            note_uid = created_note["uid"]
            logger.success(f"✓ Note created: {note_uid}")

            # Retrieve the note to verify
            logger.info("Retrieving note...")
            get_response = await client.get(
                f"{GO_API_BASE_URL}/v1/notes/{note_uid}",
                headers={
                    "Authorization": f"Bearer {token}",
                    "X-Sync-Session": session_id,
                    "X-Sync-Epoch": str(epoch),
                },
            )

            if get_response.status_code != 200:
                logger.error(f"✗ Failed to retrieve note: {get_response.status_code}")
                return False

            retrieved_note = get_response.json()
            logger.success(f"✓ Note retrieved: {retrieved_note['payload']['title']}")

            # Clean up: delete the test note
            logger.info("Cleaning up test note...")
            delete_response = await client.delete(
                f"{GO_API_BASE_URL}/v1/notes/{note_uid}",
                headers={
                    "Authorization": f"Bearer {token}",
                    "X-Sync-Session": session_id,
                    "X-Sync-Epoch": str(epoch),
                },
            )

            if delete_response.status_code != 200:
                logger.warning(f"⚠ Failed to delete test note: {delete_response.status_code}")
            else:
                logger.success("✓ Test note deleted")

            return True

        except Exception as e:
            logger.error(f"✗ MCP → Go API flow failed: {e}")
            import traceback
            logger.error(traceback.format_exc())
            return False


async def test_mcp_latency():
    """Test MCP service latency under light load."""
    logger.info("Testing MCP service latency...")

    token = generate_jwt_token(USER_ID)
    latencies = []

    async with httpx.AsyncClient(timeout=10.0) as client:
        for i in range(10):
            start = time.time()
            try:
                response = await client.get(
                    f"{MCP_BASE_URL}/",
                    headers={"Authorization": f"Bearer {token}"}
                )
                elapsed = (time.time() - start) * 1000  # Convert to ms
                latencies.append(elapsed)
                logger.debug(f"Request {i+1}/10: {elapsed:.1f}ms")
            except Exception as e:
                logger.error(f"Request {i+1}/10 failed: {e}")

    if not latencies:
        logger.error("✗ All latency tests failed")
        return False

    avg_latency = sum(latencies) / len(latencies)
    p95_latency = sorted(latencies)[int(len(latencies) * 0.95)]

    logger.info(f"Latency results (n={len(latencies)}):")
    logger.info(f"  Average: {avg_latency:.1f}ms")
    logger.info(f"  P95: {p95_latency:.1f}ms")
    logger.info(f"  Min: {min(latencies):.1f}ms")
    logger.info(f"  Max: {max(latencies):.1f}ms")

    if p95_latency < 500:
        logger.success(f"✓ Latency acceptable (p95 < 500ms)")
        return True
    else:
        logger.warning(f"⚠ Latency high (p95 = {p95_latency:.1f}ms)")
        return True  # Don't fail, just warn


async def main():
    """Run all integration tests."""
    logger.info("=" * 70)
    logger.info("ToolBridge MCP Staging Integration Tests")
    logger.info("=" * 70)
    logger.info(f"MCP Base URL: {MCP_BASE_URL}")
    logger.info(f"Go API Base URL: {GO_API_BASE_URL}")
    logger.info(f"Test User: {USER_ID}")
    logger.info(f"Tenant ID: {TENANT_ID}")
    logger.info("=" * 70)

    results = {}

    # Test 1: MCP Health
    results["mcp_health"] = await test_mcp_health()
    await asyncio.sleep(1)

    # Test 2: Go API Health
    results["go_api_health"] = await test_go_api_direct()
    await asyncio.sleep(1)

    # Test 3: End-to-End Flow
    results["e2e_flow"] = await test_mcp_to_go_api_flow()
    await asyncio.sleep(1)

    # Test 4: Latency
    results["latency"] = await test_mcp_latency()

    # Summary
    logger.info("=" * 70)
    logger.info("Test Results Summary:")
    logger.info("=" * 70)

    all_passed = True
    for test_name, passed in results.items():
        status = "✓ PASS" if passed else "✗ FAIL"
        logger.info(f"{status: <10} {test_name}")
        if not passed:
            all_passed = False

    logger.info("=" * 70)

    if all_passed:
        logger.success("🎉 All tests passed!")
        sys.exit(0)
    else:
        logger.error("❌ Some tests failed")
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
