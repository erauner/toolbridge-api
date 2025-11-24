"""
Authentication module for ToolBridge MCP.

OAuth 2.1 per-user authentication with token exchange for backend API.
"""

from toolbridge_mcp.auth.token_exchange import (
    TokenExchangeError,
    exchange_for_backend_jwt,
    issue_backend_jwt,
    extract_user_id_from_backend_jwt,
)
from toolbridge_mcp.auth.tenant_resolver import (
    TenantResolutionError,
    MultiOrganizationError,
    resolve_tenant,
)

__all__ = [
    "TokenExchangeError",
    "exchange_for_backend_jwt",
    "issue_backend_jwt",
    "extract_user_id_from_backend_jwt",
    "TenantResolutionError",
    "MultiOrganizationError",
    "resolve_tenant",
]
