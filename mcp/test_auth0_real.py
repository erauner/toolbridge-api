#!/usr/bin/env python3
"""
Test Auth0 automatic token refresh with real credentials.
"""

import asyncio
from toolbridge_mcp.config import settings
from toolbridge_mcp.auth import get_token_manager, get_access_token
from toolbridge_mcp.utils.requests import call_get


async def test_auth0_token_fetch():
    """Test that Auth0 token manager fetches and caches tokens."""
    print("\n🧪 Testing Auth0 Token Manager with Real Credentials")
    print("=" * 70)

    # Check configuration
    print(f"\n📋 Configuration:")
    print(f"   Auth mode: {settings.auth_mode()}")
    print(f"   Auth0 domain: {settings.auth0_domain}")
    print(f"   Auth0 audience: {settings.auth0_audience}")
    print(f"   Go API: {settings.go_api_base_url}")

    # Get token manager instance
    token_manager = get_token_manager()
    if not token_manager:
        print("\n❌ TokenManager not initialized!")
        print("   Make sure Auth0 credentials are configured in .env")
        return False

    print(f"\n✓ TokenManager initialized")
    print(f"   Refresh buffer: {token_manager._refresh_buffer.total_seconds()}s")

    # Test 1: Fetch token
    print(f"\n📋 Test 1: Fetch Auth0 access token")
    print("-" * 70)

    try:
        token = await get_access_token()
        print(f"✓ Token fetched successfully")
        print(f"   Token prefix: {token[:50]}...")
        print(f"   Token length: {len(token)} characters")

        if token_manager.expires_at:
            print(f"   Expires at: {token_manager.expires_at.isoformat()}Z")
            print(f"   Last refresh: {token_manager.last_refresh_at.isoformat()}Z")

    except Exception as e:
        print(f"❌ Token fetch failed: {e}")
        return False

    # Test 2: Cached token (should be instant)
    print(f"\n📋 Test 2: Verify token caching")
    print("-" * 70)

    try:
        token2 = await get_access_token()
        if token == token2:
            print(f"✓ Token cached correctly (same token returned)")
        else:
            print(f"⚠ Warning: Different token returned (unexpected)")
    except Exception as e:
        print(f"❌ Cached token fetch failed: {e}")
        return False

    # Test 3: Test Go API integration
    print(f"\n📋 Test 3: Test Go API integration")
    print("-" * 70)

    try:
        # Make a direct API call to test authentication
        import httpx
        from toolbridge_mcp.utils.requests import get_auth_header

        print("   Testing Auth0 token with Go API...")
        auth_header = await get_auth_header()

        async with httpx.AsyncClient(timeout=10.0) as client:
            # Try to create a sync session (tests authentication)
            response = await client.post(
                f"{settings.go_api_base_url}/v1/sync/sessions",
                headers={
                    "Authorization": auth_header,
                    "X-ToolBridge-Tenant-ID": settings.tenant_id,
                    "X-ToolBridge-Tenant-Header-Secret": settings.tenant_header_secret,
                    "Content-Type": "application/json",
                },
            )
            response.raise_for_status()

            session_data = response.json()
            print(f"   ✓ API authenticated successfully with Auth0 token")
            print(f"   ✓ Response status: {response.status_code}")
            print(f"   ✓ Session ID: {session_data.get('id', 'N/A')}")
            print(f"   ✓ Session epoch: {session_data.get('epoch', 'N/A')}")

    except httpx.HTTPStatusError as e:
        print(f"   ❌ HTTP error: {e.response.status_code}")
        print(f"   Response: {e.response.text}")
        return False
    except Exception as e:
        print(f"   ❌ Go API test failed: {e}")
        import traceback
        traceback.print_exc()
        return False

    print("\n" + "=" * 70)
    print("✅ All Auth0 token tests passed!")
    print("=" * 70)

    # Print token manager stats
    print(f"\n📊 Token Manager Stats:")
    print(f"   Last refresh success: {token_manager.last_refresh_success}")
    print(f"   Failure count: {token_manager.failure_count}")
    print(f"   Expires at: {token_manager.expires_at}")

    return True


if __name__ == "__main__":
    # Import server to trigger automatic TokenManager initialization
    import toolbridge_mcp.server  # noqa: F401

    success = asyncio.run(test_auth0_token_fetch())
    exit(0 if success else 1)
