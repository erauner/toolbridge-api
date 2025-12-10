"""
Pytest configuration and fixtures for toolbridge_mcp tests.

Sets up required environment variables before any imports.
"""

import os

# Set required environment variables BEFORE any toolbridge_mcp imports
# These are only needed to satisfy pydantic-settings validation
# Config uses TOOLBRIDGE_ prefix (see config.py model_config)
os.environ.setdefault("TOOLBRIDGE_AUTHKIT_DOMAIN", "https://test.authkit.dev")
os.environ.setdefault("TOOLBRIDGE_PUBLIC_BASE_URL", "http://localhost:8080")
os.environ.setdefault("TOOLBRIDGE_GO_API_BASE_URL", "http://localhost:8080")
