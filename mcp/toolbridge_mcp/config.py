"""
Configuration management for ToolBridge MCP service.

Loads settings from environment variables with TOOLBRIDGE_ prefix.
"""

from typing import Literal

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Application settings loaded from environment variables."""

    # Tenant configuration
    tenant_id: str
    tenant_header_secret: str

    # Go API connection
    go_api_base_url: str = "http://localhost:8081"

    # Auth0 configuration (automatic token refresh mode)
    # When all four Auth0 settings are provided, the MCP server will automatically
    # fetch and refresh tokens using OAuth2 client credentials flow.
    auth0_client_id: str | None = None
    auth0_client_secret: str | None = None
    auth0_domain: str = "dev-zysv6k3xo7pkwmcb.us.auth0.com"
    auth0_audience: str = "https://toolbridgeapi.erauner.dev"

    # Token refresh configuration
    # How many seconds before token expiry to trigger refresh (default 5 minutes)
    token_refresh_buffer_seconds: int = 300
    # HTTP timeout for Auth0 requests in seconds
    auth0_timeout_seconds: float = 10.0

    # Authentication (deprecated - use Auth0 client credentials instead)
    # Shared JWT token for backend API authentication
    # DEPRECATED: Use TOOLBRIDGE_AUTH0_CLIENT_ID/SECRET for automatic refresh
    jwt_token: str | None = None

    # Logging
    log_level: str = "INFO"

    # Server configuration
    host: str = "0.0.0.0"
    port: int = 8001

    # Security
    # Timestamp validation window in seconds (default 5 minutes)
    max_timestamp_skew_seconds: int = 300

    model_config = SettingsConfigDict(
        env_prefix="TOOLBRIDGE_",
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )

    def auth_mode(self) -> Literal["auth0", "static", "passthrough"]:
        """
        Determine current authentication mode.

        Returns:
            - "auth0": Automatic token refresh using client credentials
            - "static": Static JWT token (deprecated)
            - "passthrough": Per-user tokens from MCP request headers
        """
        # Check if all required Auth0 credentials are present
        if (
            self.auth0_client_id
            and self.auth0_client_secret
            and self.auth0_domain
            and self.auth0_audience
        ):
            return "auth0"

        # Fall back to static token if configured
        if self.jwt_token:
            return "static"

        # Default to passthrough mode (per-user authentication)
        return "passthrough"


# Global settings instance
settings = Settings()
