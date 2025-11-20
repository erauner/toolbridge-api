"""
Auth0 token management for ToolBridge MCP server.

This module provides automatic Auth0 token acquisition and refresh using
the OAuth2 client credentials flow. Tokens are cached in-memory and
automatically refreshed before expiry.
"""

from toolbridge_mcp.auth.token_manager import (
    TokenManager,
    TokenError,
    init_token_manager,
    get_token_manager,
    get_access_token,
    shutdown_token_manager,
)

__all__ = [
    "TokenManager",
    "TokenError",
    "init_token_manager",
    "get_token_manager",
    "get_access_token",
    "shutdown_token_manager",
]
