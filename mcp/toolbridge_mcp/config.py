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

    # Authentication
    # Shared JWT token for backend API authentication
    # The MCP server uses this token when calling the Go API on behalf of users
    # TODO: Add OAuth/PKCE support for per-user authentication in future PR
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


# Global settings instance
settings = Settings()
