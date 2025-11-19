"""
Async HTTP client factory for making requests to the Go API.

Provides a context manager pattern for creating httpx clients with the
TenantDirectTransport, which automatically adds tenant headers to requests.
"""

from contextlib import asynccontextmanager
from typing import AsyncGenerator, Callable, Optional, AsyncContextManager

import httpx
from loguru import logger

# Global client factory (can be overridden for testing)
_client_factory: Optional[Callable[[], AsyncContextManager[httpx.AsyncClient]]] = None


def set_client_factory(factory: Callable[[], AsyncContextManager[httpx.AsyncClient]]) -> None:
    """
    Override the default client factory.
    
    This is primarily used for testing to inject mock clients.
    
    Args:
        factory: Async context manager function that yields an httpx.AsyncClient
    """
    global _client_factory
    _client_factory = factory
    logger.debug("Custom client factory set")


@asynccontextmanager
async def get_client() -> AsyncGenerator[httpx.AsyncClient, None]:
    """
    Get an AsyncClient as a context manager.
    
    If a custom factory has been set via set_client_factory(), uses that.
    Otherwise, creates a client with TenantDirectTransport.
    
    Usage:
        async with get_client() as client:
            response = await client.get("/v1/notes")
    
    Yields:
        httpx.AsyncClient configured with tenant transport
    """
    if _client_factory:
        # Use custom factory (e.g., for testing)
        async with _client_factory() as client:
            yield client
    else:
        # Use default TenantDirectTransport
        from toolbridge_mcp.transports.tenant_direct import TenantDirectTransport
        from toolbridge_mcp.config import settings
        
        transport = TenantDirectTransport()
        async with httpx.AsyncClient(
            transport=transport,
            base_url=settings.go_api_base_url,
            timeout=httpx.Timeout(30.0),
        ) as client:
            yield client
