"""
MCP server instance with OAuth 2.1 authentication.

This module creates the MCP server instance configured with AuthKitProvider
for per-user authentication via browser-based OAuth 2.1 + PKCE flow.
"""

from fastmcp import FastMCP
from fastmcp.server.auth.providers.workos import AuthKitProvider
from loguru import logger

from toolbridge_mcp.config import settings

# Validate WorkOS AuthKit configuration at module load
settings.validate_authkit_config()

# Create WorkOS AuthKit provider for per-user authentication
# Users authenticate via claude.ai web UI → browser → WorkOS AuthKit login
# The MCP server acts as a protected resource that validates WorkOS tokens
auth_provider = AuthKitProvider(
    authkit_domain=settings.authkit_domain,
    # MCP's public URL (used in OAuth metadata)
    base_url=settings.public_base_url,
)

logger.info(
    f"✓ AuthKitProvider configured: domain={settings.authkit_domain}, "
    f"backend_audience={settings.backend_api_audience}"
)

# Create MCP server instance with OAuth authentication
mcp = FastMCP(
    name="ToolBridge",
    auth=auth_provider,
)
