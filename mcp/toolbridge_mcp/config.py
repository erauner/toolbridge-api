"""
Configuration management for ToolBridge MCP service.

Loads settings from environment variables with TOOLBRIDGE_ prefix.
"""

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Application settings loaded from environment variables."""

    # Tenant configuration
    tenant_id: str
    tenant_header_secret: str

    # Go API connection
    go_api_base_url: str = "http://localhost:8081"

    # OAuth Provider Configuration (Path B)
    # These configure FastMCP's Auth0Provider for per-user authentication
    # Users authenticate via browser through Auth0 OAuth 2.1 + PKCE flow
    oauth_client_id: str  # Auth0 SPA or Web App client ID
    oauth_client_secret: str | None = None  # Optional for public clients
    oauth_domain: str = "dev-zysv6k3xo7pkwmcb.us.auth0.com"
    oauth_audience: str = "https://toolbridge-mcp.fly.dev"  # THIS MCP server audience
    oauth_base_url: str = "https://toolbridge-mcp-staging.fly.dev"  # Public MCP URL

    # Backend API Configuration
    # The Go API that MCP server calls after token exchange
    backend_api_audience: str = "https://toolbridgeapi.erauner.dev"

    # JWT Signing (Optional - for token exchange Option 2)
    # Private key for signing backend JWTs if not using backend /token-exchange endpoint
    jwt_signing_key: str | None = None

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

    def validate_oauth_config(self) -> None:
        """Validate OAuth provider configuration at startup."""
        if not self.oauth_client_id:
            raise ValueError(
                "TOOLBRIDGE_OAUTH_CLIENT_ID is required for OAuth 2.1 authentication. "
                "Create an Auth0 application and set this environment variable."
            )


# Global settings instance
settings = Settings()
