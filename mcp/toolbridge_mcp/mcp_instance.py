"""
MCP server instance with OAuth 2.1 authentication.

This module creates the MCP server instance configured with Auth0Provider
for per-user authentication via browser-based OAuth 2.1 + PKCE flow.
"""

from fastmcp import FastMCP
from fastmcp.server.auth.providers.auth0 import Auth0Provider
from loguru import logger

from toolbridge_mcp.config import settings

# Validate OAuth configuration at module load
settings.validate_oauth_config()

# Create OAuth provider for per-user authentication
# Users authenticate via claude.ai web UI → browser → Auth0 login
auth_provider = Auth0Provider(
    config_url=f"https://{settings.oauth_domain}/.well-known/openid-configuration",  # Auth0 discovery URL
    client_id=settings.oauth_client_id,
    client_secret=settings.oauth_client_secret or "",  # Empty string for public clients
    base_url=settings.oauth_base_url,
    # Scopes that users will consent to during OAuth flow
    required_scopes=[
        "openid",
        "profile",
        "email",
        "sync:read",
        "sync:write",
    ],
    # Request access to backend API (not MCP server's own audience)
    audience=settings.backend_api_audience,
    # Allow Claude's callback URLs for Dynamic Client Registration
    allowed_client_redirect_uris=[
        "https://claude.ai/api/mcp/auth_callback",
        "https://claude.com/api/mcp/auth_callback",
    ],
    # Skip FastMCP's consent page to avoid CSP issues with claude.ai
    # Auth0 will handle consent instead
    require_authorization_consent=False,
)

logger.info(f"✓ Auth0Provider configured: domain={settings.oauth_domain}, audience={settings.backend_api_audience}")

# Create MCP server instance with OAuth authentication
mcp = FastMCP(
    name="ToolBridge",
    auth=auth_provider,
)
