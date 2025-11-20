"""
Tenant header signing utilities.

Generates HMAC-SHA256 signed headers for authenticating requests to the Go API.
Headers include tenant ID, timestamp, and cryptographic signature to prevent
tampering and ensure tenant isolation.
"""

import hmac
import hashlib
import time
from typing import Dict

from loguru import logger


class TenantHeaderSigner:
    """
    Signs outbound requests with HMAC tenant headers.

    Uses HMAC-SHA256 to sign a message containing tenant ID and timestamp,
    ensuring that only services with the shared secret can create valid headers.
    """

    def __init__(self, secret: str, tenant_id: str, skew_seconds: int = 300):
        """
        Initialize the signer.

        Args:
            secret: Shared secret for HMAC signing (must match Go API)
            tenant_id: Tenant identifier for this service instance
            skew_seconds: Maximum acceptable timestamp skew (default 5 minutes)
        """
        self.secret = secret
        self.tenant_id = tenant_id
        self.skew_seconds = skew_seconds

    def sign(self) -> Dict[str, str]:
        """
        Generate signed tenant headers for the current timestamp.

        Returns:
            Dictionary of headers to add to HTTP requests:
            - X-TB-Tenant-ID: Tenant identifier
            - X-TB-Timestamp: Unix timestamp in milliseconds
            - X-TB-Signature: HMAC-SHA256 hex signature
        """
        timestamp_ms = int(time.time() * 1000)
        message = f"{self.tenant_id}:{timestamp_ms}"

        # Compute HMAC-SHA256 signature
        signature = hmac.new(
            key=self.secret.encode("utf-8"), msg=message.encode("utf-8"), digestmod=hashlib.sha256
        ).hexdigest()

        headers = {
            "X-TB-Tenant-ID": self.tenant_id,
            "X-TB-Timestamp": str(timestamp_ms),
            "X-TB-Signature": signature,
        }

        logger.debug(
            f"Signed tenant headers: tenant_id={self.tenant_id}, "
            f"timestamp_ms={timestamp_ms}, signature={signature[:16]}..."
        )

        return headers

    def verify(self, tenant_id: str, timestamp_ms: int, signature: str) -> bool:
        """
        Verify a signature (for testing purposes).

        Args:
            tenant_id: Tenant ID from headers
            timestamp_ms: Timestamp from headers
            signature: Signature to verify

        Returns:
            True if signature is valid, False otherwise
        """
        # Check timestamp within acceptable window
        current_time_ms = int(time.time() * 1000)
        skew_ms = abs(current_time_ms - timestamp_ms)
        if skew_ms > (self.skew_seconds * 1000):
            logger.warning(
                f"Timestamp outside acceptable window: skew={skew_ms}ms, "
                f"max={self.skew_seconds * 1000}ms"
            )
            return False

        # Recompute expected signature
        message = f"{tenant_id}:{timestamp_ms}"
        expected_sig = hmac.new(
            key=self.secret.encode("utf-8"), msg=message.encode("utf-8"), digestmod=hashlib.sha256
        ).hexdigest()

        # Constant-time comparison
        return hmac.compare_digest(expected_sig, signature)
