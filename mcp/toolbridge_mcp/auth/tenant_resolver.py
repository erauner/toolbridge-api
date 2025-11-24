"""
Tenant resolution for multi-tenant MCP deployments.

Calls /v1/auth/tenant endpoint to resolve tenant ID from authenticated user.
"""

import httpx
from loguru import logger


class TenantResolutionError(Exception):
    """Raised when tenant resolution fails."""
    pass


class MultiOrganizationError(TenantResolutionError):
    """Raised when user belongs to multiple organizations and must select one."""

    def __init__(self, organizations: list[dict[str, str]]):
        self.organizations = organizations
        super().__init__(
            f"User belongs to {len(organizations)} organizations. "
            "Organization selection not yet implemented."
        )


async def resolve_tenant(id_token: str, api_base_url: str) -> str:
    """
    Resolve tenant ID for authenticated user.

    Calls the backend /v1/auth/tenant endpoint which validates the ID token
    and queries WorkOS API to determine which organization(s) the user belongs to.

    Args:
        id_token: ID token from OIDC authentication
        api_base_url: Base URL of the Go API (e.g., http://localhost:8080)

    Returns:
        Tenant ID (organization ID) for the user

    Raises:
        TenantResolutionError: If resolution fails
        MultiOrganizationError: If user belongs to multiple organizations
    """
    url = f"{api_base_url}/v1/auth/tenant"

    logger.debug(f"Resolving tenant via {url}")

    try:
        async with httpx.AsyncClient() as client:
            response = await client.get(
                url,
                headers={
                    "Authorization": f"Bearer {id_token}",
                    "Accept": "application/json",
                },
                timeout=10.0,
            )

            if response.status_code == 401:
                raise TenantResolutionError(
                    "Authentication failed. Token may be expired or invalid."
                )

            if response.status_code == 403:
                raise TenantResolutionError(
                    "User not authorized to access any organizations."
                )

            if response.status_code != 200:
                raise TenantResolutionError(
                    f"Tenant resolution failed with status {response.status_code}: "
                    f"{response.text}"
                )

            data = response.json()

            # Check if multi-organization response
            requires_selection = data.get("requires_selection", False)
            if requires_selection:
                organizations = data.get("organizations", [])
                raise MultiOrganizationError(organizations)

            # Single organization - extract tenant_id
            tenant_id = data.get("tenant_id")
            if not tenant_id:
                raise TenantResolutionError(
                    "Response missing tenant_id field"
                )

            org_name = data.get("organization_name", "Unknown")
            logger.info(
                f"Tenant resolved successfully: {tenant_id} ({org_name})"
            )

            return tenant_id

    except httpx.HTTPError as e:
        raise TenantResolutionError(
            f"Failed to connect to tenant resolution endpoint: {e}"
        ) from e
