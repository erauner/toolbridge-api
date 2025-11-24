"""
Custom httpx transport that adds tenant header to requests.

The TenantDirectTransport wraps httpx.AsyncHTTPTransport and automatically
injects X-Tenant-ID header on all outbound requests to the Go API.

Supports two modes:
- Single-tenant: Uses configured TENANT_ID (smoke testing)
- Multi-tenant: Uses dynamically resolved tenant ID (primary mode)
"""

import httpx
from loguru import logger

from toolbridge_mcp.config import settings


class TenantDirectTransport(httpx.AsyncBaseTransport):
    """
    Transport that adds X-Tenant-ID header to all requests.

    This transport wraps the standard AsyncHTTPTransport and injects
    the X-Tenant-ID header before forwarding requests to the Go API.
    Tenant ID is either configured (single-tenant) or dynamically resolved
    (multi-tenant) via the requests module.
    """

    def __init__(self):
        """
        Initialize transport.

        The actual tenant_id is resolved at request time by the requests module.
        This allows us to support both single-tenant (configured) and multi-tenant
        (dynamic resolution) modes.
        """
        # Create underlying HTTP transport for actual network requests
        self._transport = httpx.AsyncHTTPTransport()

        mode = "single-tenant" if settings.tenant_id else "multi-tenant"
        logger.debug(
            f"TenantDirectTransport initialized: mode={mode}, "
            f"go_api={settings.go_api_base_url}"
        )

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        """
        Handle an HTTP request by adding X-Tenant-ID header and forwarding.

        Args:
            request: The HTTP request to process

        Returns:
            HTTP response from the Go API
        """
        # Import here to avoid circular dependency
        from toolbridge_mcp.utils.requests import get_cached_tenant_id

        # Get tenant_id (should already be resolved by ensure_tenant_resolved)
        tenant_id = get_cached_tenant_id()

        if tenant_id:
            request.headers["X-Tenant-ID"] = tenant_id
            logger.debug(f"{request.method} {request.url.path} [tenant_id={tenant_id}]")
        else:
            # This should not happen if ensure_tenant_resolved was called
            logger.warning(
                f"{request.method} {request.url.path} - No tenant_id available. "
                "This may indicate ensure_tenant_resolved() was not called."
            )

        # Forward request to Go API
        try:
            response = await self._transport.handle_async_request(request)

            logger.debug(
                f"{request.method} {request.url.path} -> {response.status_code} "
                f"[tenant_id={tenant_id or 'none'}]"
            )

            return response
        except Exception as e:
            logger.error(f"Request failed: {request.method} {request.url.path} - {e}")
            raise

    async def aclose(self):
        """Close the underlying transport."""
        await self._transport.aclose()
        logger.debug("TenantDirectTransport closed")
