"""
Custom httpx transport that adds tenant headers to requests.

The TenantDirectTransport wraps httpx.AsyncHTTPTransport and automatically
injects signed tenant headers on all outbound requests to the Go API.
"""

import httpx
from loguru import logger

from toolbridge_mcp.config import settings
from toolbridge_mcp.utils.headers import TenantHeaderSigner


class TenantDirectTransport(httpx.AsyncBaseTransport):
    """
    Transport that adds HMAC-signed tenant headers to all requests.

    This transport wraps the standard AsyncHTTPTransport and injects
    tenant authentication headers before forwarding requests to the Go API.
    The headers include tenant ID, timestamp, and HMAC signature.
    """

    def __init__(self):
        """
        Initialize transport with tenant header signer.

        Reads tenant configuration from global settings.
        """
        self.signer = TenantHeaderSigner(
            secret=settings.tenant_header_secret,
            tenant_id=settings.tenant_id,
            skew_seconds=settings.max_timestamp_skew_seconds,
        )

        # Create underlying HTTP transport for actual network requests
        self._transport = httpx.AsyncHTTPTransport()

        logger.debug(
            f"TenantDirectTransport initialized: tenant_id={settings.tenant_id}, "
            f"go_api={settings.go_api_base_url}"
        )

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        """
        Handle an HTTP request by adding tenant headers and forwarding.

        Args:
            request: The HTTP request to process

        Returns:
            HTTP response from the Go API
        """
        # Generate and add signed tenant headers
        tenant_headers = self.signer.sign()
        for key, value in tenant_headers.items():
            request.headers[key] = value

        logger.debug(f"{request.method} {request.url.path} [tenant_id={settings.tenant_id}]")

        # Forward request to Go API
        try:
            response = await self._transport.handle_async_request(request)

            logger.debug(
                f"{request.method} {request.url.path} -> {response.status_code} "
                f"[tenant_id={settings.tenant_id}]"
            )

            return response
        except Exception as e:
            logger.error(f"Request failed: {request.method} {request.url.path} - {e}")
            raise

    async def aclose(self):
        """Close the underlying transport."""
        await self._transport.aclose()
        logger.debug("TenantDirectTransport closed")
