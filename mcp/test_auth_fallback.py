"""
Test Auth0 fallback behavior.

This script verifies that when Auth0 initialization fails, the server
properly falls back to static/passthrough mode and requests succeed.
"""

import asyncio
import os
import sys


async def test_auth_fallback():
    """Test that Auth0 init failure triggers proper fallback."""
    print("\n🧪 Testing Auth0 fallback behavior...")
    print("=" * 60)

    # Capture original environment state for cleanup
    original_env = {}
    test_env_keys = [
        "TOOLBRIDGE_AUTH0_CLIENT_ID",
        "TOOLBRIDGE_AUTH0_CLIENT_SECRET",
        "TOOLBRIDGE_AUTH0_DOMAIN",
        "TOOLBRIDGE_AUTH0_AUDIENCE",
        "TOOLBRIDGE_JWT_TOKEN",
        "TOOLBRIDGE_TENANT_ID",
        "TOOLBRIDGE_TENANT_HEADER_SECRET",
    ]
    for key in test_env_keys:
        original_env[key] = os.environ.get(key)

    try:
        # Test Case 1: Auth0 init fails with static token available
        print("\n📋 Test 1: Auth0 fails, static token available")
        print("-" * 60)

        # Set environment for Auth0 mode but cause init failure by monkeypatching
        os.environ["TOOLBRIDGE_AUTH0_CLIENT_ID"] = "test_client_id"
        os.environ["TOOLBRIDGE_AUTH0_CLIENT_SECRET"] = "test_secret"
        os.environ["TOOLBRIDGE_AUTH0_DOMAIN"] = "test.auth0.com"
        os.environ["TOOLBRIDGE_AUTH0_AUDIENCE"] = "https://test.example.com"
        os.environ["TOOLBRIDGE_JWT_TOKEN"] = "test_static_token_123"
        os.environ["TOOLBRIDGE_TENANT_ID"] = "test-tenant"
        os.environ["TOOLBRIDGE_TENANT_HEADER_SECRET"] = "test-secret"

        # Monkeypatch init_token_manager to raise an exception
        import toolbridge_mcp.auth as auth_module

        original_init = auth_module.init_token_manager

        def failing_init(*args, **kwargs):
            raise Exception("Simulated Auth0 init failure (network error)")

        auth_module.init_token_manager = failing_init

        # Import server module (this triggers Auth0 init)
        # We need to reload it fresh for each test
        if "toolbridge_mcp.server" in sys.modules:
            del sys.modules["toolbridge_mcp.server"]
        if "toolbridge_mcp.config" in sys.modules:
            del sys.modules["toolbridge_mcp.config"]

        # This import should trigger Auth0 init failure and fallback to static
        from toolbridge_mcp.server import get_runtime_auth_mode, auth0_init_failed
        from toolbridge_mcp.config import settings

        configured_mode = settings.auth_mode()
        runtime_mode = get_runtime_auth_mode()
        init_failed = auth0_init_failed()

        print(f"Configured auth mode: {configured_mode}")
        print(f"Runtime auth mode: {runtime_mode}")
        print(f"Auth0 init failed: {init_failed}")

        # Verify fallback worked
        assert configured_mode == "auth0", "Should be configured for auth0"
        assert runtime_mode == "static", "Should fall back to static mode"
        assert init_failed is True, "Should report Auth0 init failure"

        # Test that get_auth_header works with fallback
        from toolbridge_mcp.utils.requests import get_auth_header

        try:
            auth_header = await get_auth_header()
            assert auth_header == "Bearer test_static_token_123", "Should use static token"
            print(f"✓ Auth header: {auth_header}")
            print("✓ Test 1 PASSED: Fallback to static token works")
        except Exception as e:
            print(f"✗ Test 1 FAILED: {e}")
            raise

        # Restore original init for cleanup
        auth_module.init_token_manager = original_init

        # Test Case 2: Auth0 init fails with no static token (passthrough)
        print("\n📋 Test 2: Auth0 fails, no static token (passthrough)")
        print("-" * 60)

        # Clear static token
        del os.environ["TOOLBRIDGE_JWT_TOKEN"]

        # Reload modules
        if "toolbridge_mcp.server" in sys.modules:
            del sys.modules["toolbridge_mcp.server"]
        if "toolbridge_mcp.config" in sys.modules:
            del sys.modules["toolbridge_mcp.config"]
        if "toolbridge_mcp.auth" in sys.modules:
            del sys.modules["toolbridge_mcp.auth"]

        # Monkeypatch again for test 2
        import toolbridge_mcp.auth as auth_module2

        auth_module2.init_token_manager = failing_init

        from toolbridge_mcp.server import get_runtime_auth_mode, auth0_init_failed
        from toolbridge_mcp.config import settings

        # Restore again
        auth_module2.init_token_manager = original_init

        configured_mode = settings.auth_mode()
        runtime_mode = get_runtime_auth_mode()
        init_failed = auth0_init_failed()

        print(f"Configured auth mode: {configured_mode}")
        print(f"Runtime auth mode: {runtime_mode}")
        print(f"Auth0 init failed: {init_failed}")

        assert configured_mode == "auth0", "Should be configured for auth0"
        assert runtime_mode == "passthrough", "Should fall back to passthrough mode"
        assert init_failed is True, "Should report Auth0 init failure"

        print("✓ Test 2 PASSED: Fallback to passthrough works")

        print("\n" + "=" * 60)
        print("✅ All Auth0 fallback tests passed!")
        print("=" * 60)

    finally:
        # Restore original environment state
        print("\n🧹 Cleaning up environment variables...")
        for key, original_value in original_env.items():
            if original_value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = original_value
        print("✓ Environment restored")


if __name__ == "__main__":
    asyncio.run(test_auth_fallback())
