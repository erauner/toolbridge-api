"""
Auth0 token manager with automatic refresh.

Handles OAuth2 client credentials flow with in-memory token caching,
automatic refresh before expiry, and resilient failure handling.
"""

import asyncio
from datetime import datetime, timedelta
from typing import Optional

import httpx
from loguru import logger
from pydantic import BaseModel


class TokenError(Exception):
    """Raised when token acquisition fails after retries."""

    pass


class TokenResponse(BaseModel):
    """Auth0 token response."""

    access_token: str
    expires_in: int
    token_type: str
    scope: Optional[str] = None


class TokenManager:
    """
    Manages Auth0 access tokens with automatic refresh.

    Fetches tokens using OAuth2 client credentials flow and caches
    them in-memory until they're close to expiration. Implements
    retry logic with exponential backoff for resilience.

    Thread-safe via asyncio.Lock to prevent concurrent refresh attempts.
    """

    def __init__(
        self,
        *,
        client_id: str,
        client_secret: str,
        domain: str,
        audience: str,
        refresh_buffer: timedelta = timedelta(minutes=5),
        timeout: float = 10.0,
    ):
        """
        Initialize token manager.

        Args:
            client_id: Auth0 client ID (M2M application)
            client_secret: Auth0 client secret
            domain: Auth0 domain (e.g., dev-xxx.us.auth0.com)
            audience: Auth0 API audience/identifier
            refresh_buffer: Time before expiry to refresh (default 5 minutes)
            timeout: HTTP request timeout in seconds
        """
        self.client_id = client_id
        self.client_secret = client_secret
        self.domain = domain
        self.audience = audience
        self._refresh_buffer = refresh_buffer

        # Token cache
        self._token: Optional[str] = None
        self._expires_at: Optional[datetime] = None
        self._last_refresh_at: Optional[datetime] = None

        # Failure tracking for observability
        self._failure_count: int = 0
        self._last_refresh_success: bool = True

        # Concurrency control
        self._lock = asyncio.Lock()

        # HTTP client for token requests
        self._client = httpx.AsyncClient(
            base_url=f"https://{domain}",
            timeout=timeout,
        )

        logger.debug(
            f"TokenManager initialized: domain={domain}, "
            f"audience={audience}, buffer={refresh_buffer.total_seconds()}s"
        )

    async def get_token(self) -> str:
        """
        Get valid access token, refreshing if needed.

        Uses double-checked locking to prevent thundering herd:
        - First check without lock (fast path for valid cached token)
        - Acquire lock and check again before refreshing

        Returns:
            Valid Auth0 access token

        Raises:
            TokenError: If token acquisition fails after retries
        """
        # Fast path: return cached token if still valid
        if self._is_token_valid():
            logger.debug("Using cached Auth0 token")
            return self._token

        # Slow path: acquire lock and refresh
        async with self._lock:
            # Double-check after acquiring lock (another coroutine may have refreshed)
            if self._is_token_valid():
                logger.debug("Token refreshed by concurrent request")
                return self._token

            # Refresh token
            logger.info("Fetching new Auth0 access token")
            await self._refresh_token()
            return self._token

    def _is_token_valid(self) -> bool:
        """
        Check if cached token is still valid.

        Returns:
            True if token exists and hasn't expired (accounting for buffer)
        """
        if not self._token or not self._expires_at:
            return False

        now = datetime.utcnow()
        # Token is valid if current time + buffer < expiry time
        return now < (self._expires_at - self._refresh_buffer)

    async def _refresh_token(self) -> None:
        """
        Fetch new access token from Auth0 with retry logic.

        Implements capped exponential backoff:
        - Attempt 1: immediate
        - Attempt 2: wait 0.5s
        - Attempt 3: wait 1.0s
        - Attempt 4: wait 2.0s (final attempt)

        Raises:
            TokenError: If all retry attempts fail
        """
        token_url = "/oauth/token"
        payload = {
            "client_id": self.client_id,
            "client_secret": self.client_secret,
            "audience": self.audience,
            "grant_type": "client_credentials",
        }

        max_attempts = 4
        base_delay = 0.5  # seconds

        for attempt in range(1, max_attempts + 1):
            try:
                logger.debug(f"Auth0 token request attempt {attempt}/{max_attempts}")

                response = await self._client.post(
                    token_url,
                    json=payload,
                    headers={"content-type": "application/json"},
                )
                response.raise_for_status()

                token_data = TokenResponse(**response.json())

                # Update cache
                self._token = token_data.access_token
                self._expires_at = datetime.utcnow() + timedelta(seconds=token_data.expires_in)
                self._last_refresh_at = datetime.utcnow()
                self._last_refresh_success = True
                self._failure_count = 0  # Reset on success

                logger.info(
                    f"Auth0 token refreshed successfully "
                    f"(expires in {token_data.expires_in}s, at {self._expires_at.isoformat()}Z)"
                )

                return  # Success

            except httpx.HTTPStatusError as e:
                self._failure_count += 1
                logger.error(
                    f"Auth0 token request failed (attempt {attempt}/{max_attempts}): "
                    f"HTTP {e.response.status_code} - {e.response.text}"
                )

                # Don't retry on 4xx errors (likely bad credentials)
                if 400 <= e.response.status_code < 500:
                    self._last_refresh_success = False
                    raise TokenError(
                        f"Auth0 authentication failed: {e.response.status_code} {e.response.text}"
                    ) from e

            except Exception as e:
                self._failure_count += 1
                logger.error(f"Auth0 token request failed (attempt {attempt}/{max_attempts}): {e}")

            # Exponential backoff before retry (except on last attempt)
            if attempt < max_attempts:
                delay = base_delay * (2 ** (attempt - 1))
                logger.debug(f"Retrying in {delay}s...")
                await asyncio.sleep(delay)

        # All attempts failed
        self._last_refresh_success = False
        raise TokenError(
            f"Failed to fetch Auth0 token after {max_attempts} attempts. "
            f"Check TOOLBRIDGE_AUTH0_CLIENT_ID/SECRET and Auth0 availability."
        )

    @property
    def expires_at(self) -> Optional[datetime]:
        """Get token expiry time (for observability)."""
        return self._expires_at

    @property
    def last_refresh_at(self) -> Optional[datetime]:
        """Get last successful refresh time (for observability)."""
        return self._last_refresh_at

    @property
    def last_refresh_success(self) -> bool:
        """Get status of last refresh attempt (for health checks)."""
        return self._last_refresh_success

    @property
    def failure_count(self) -> int:
        """Get count of consecutive failures (for observability)."""
        return self._failure_count

    async def close(self) -> None:
        """Close HTTP client and cleanup resources."""
        await self._client.aclose()
        logger.debug("TokenManager closed")


# Global singleton instance
_token_manager: Optional[TokenManager] = None


def init_token_manager(
    *,
    client_id: str,
    client_secret: str,
    domain: str,
    audience: str,
    refresh_buffer_seconds: int = 300,
    timeout: float = 10.0,
) -> None:
    """
    Initialize global token manager singleton.

    Args:
        client_id: Auth0 client ID
        client_secret: Auth0 client secret
        domain: Auth0 domain
        audience: Auth0 API audience
        refresh_buffer_seconds: Seconds before expiry to refresh (default 300)
        timeout: HTTP timeout in seconds

    Raises:
        RuntimeError: If token manager already initialized
    """
    global _token_manager

    if _token_manager is not None:
        raise RuntimeError("TokenManager already initialized")

    _token_manager = TokenManager(
        client_id=client_id,
        client_secret=client_secret,
        domain=domain,
        audience=audience,
        refresh_buffer=timedelta(seconds=refresh_buffer_seconds),
        timeout=timeout,
    )

    logger.info(
        f"Auth0 TokenManager initialized "
        f"(domain={domain}, audience={audience}, buffer={refresh_buffer_seconds}s)"
    )


def get_token_manager() -> Optional[TokenManager]:
    """
    Get global token manager instance.

    Returns:
        TokenManager instance if initialized, None otherwise
    """
    return _token_manager


async def get_access_token() -> str:
    """
    Get valid Auth0 access token from global manager.

    Returns:
        Valid access token

    Raises:
        RuntimeError: If token manager not initialized
        TokenError: If token acquisition fails
    """
    if _token_manager is None:
        raise RuntimeError(
            "TokenManager not initialized. "
            "Configure TOOLBRIDGE_AUTH0_CLIENT_ID and TOOLBRIDGE_AUTH0_CLIENT_SECRET."
        )

    return await _token_manager.get_token()


async def shutdown_token_manager() -> None:
    """
    Shutdown global token manager and cleanup resources.

    Safe to call even if not initialized.
    """
    global _token_manager

    if _token_manager is not None:
        await _token_manager.close()
        _token_manager = None
        logger.info("TokenManager shutdown complete")
