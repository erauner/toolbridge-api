"""
DEPRECATED: This test file is no longer relevant for Path B OAuth 2.1.

Path B uses FastMCP's Auth0Provider for per-user authentication,
eliminating the need for M2M token manager and fallback modes.

Original purpose: Test M2M auth fallback to static/passthrough modes.
Path B replacement: Users authenticate via browser OAuth flow through claude.ai.

To test Path B OAuth:
1. Deploy MCP server with OAuth configuration
2. Add connector in claude.ai → Settings → Connectors
3. Authenticate via browser
4. Test tool invocations in Claude Desktop

This file is kept for reference but should not be executed.
"""
