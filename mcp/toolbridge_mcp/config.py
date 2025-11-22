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
    go_api_base_url: str = "http://localhost:8080"

    # WorkOS AuthKit Configuration
    # These configure FastMCP's AuthKitProvider for per-user authentication
    # Users authenticate via browser through WorkOS AuthKit OAuth 2.1 + PKCE flow
    authkit_domain: str  # WorkOS AuthKit domain (e.g., "toolbridge.authkit.app")

    # Public MCP URL (used in OAuth metadata and resource identification)
    public_base_url: str  # e.g., "https://toolbridge-mcp-staging.fly.dev"

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
    # Reserved for future timestamp validation - currently unused
    max_timestamp_skew_seconds: int = 300

    # Uvicorn / HTTP server behavior
    # shutdown_timeout_seconds controls how long uvicorn waits for in-flight requests
    # before force-closing during graceful shutdown (SIGTERM/SIGINT).
    # IMPORTANT: Must be less than Fly.io kill_timeout (currently 10s) to avoid SIGKILL
    shutdown_timeout_seconds: int = 7

    # Turn off uvicorn access logs to reduce noise in Fly.io logs (MCP already logs requests)
    uvicorn_access_log: bool = False

    model_config = SettingsConfigDict(
        env_prefix="TOOLBRIDGE_",
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )

    def validate_authkit_config(self) -> None:
        """Validate WorkOS AuthKit provider configuration at startup."""
        if not self.authkit_domain:
            raise ValueError(
                "TOOLBRIDGE_AUTHKIT_DOMAIN is required for WorkOS AuthKit authentication. "
                "Configure your WorkOS AuthKit domain and set this environment variable."
            )
        if not self.public_base_url:
            raise ValueError(
                "TOOLBRIDGE_PUBLIC_BASE_URL is required for OAuth metadata. "
                "Set this to the public URL of the MCP server."
            )


# Global settings instance
settings = Settings()
