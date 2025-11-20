"""
DEPRECATED: This test file is no longer relevant for Path B OAuth 2.1.

Path B uses FastMCP's Auth0Provider for per-user authentication,
eliminating the need for M2M token manager.

Original purpose: Test M2M Auth0 client credentials token fetch.
Path B replacement: FastMCP handles OAuth token validation automatically.

To test Path B OAuth:
1. Deploy MCP server with OAuth configuration:
   - Set TOOLBRIDGE_OAUTH_CLIENT_ID, TOOLBRIDGE_OAUTH_CLIENT_SECRET, etc.
   - Deploy to Fly.io: fly deploy --config fly.staging.toml
   
2. Verify OAuth metadata endpoint:
   curl https://toolbridge-mcp-staging.fly.dev/.well-known/oauth-protected-resource
   
3. Add connector in claude.ai:
   - Settings → Connectors → Add Connector
   - URL: https://toolbridge-mcp-staging.fly.dev
   - Authenticate via browser (Auth0 OAuth flow)
   
4. Test tool invocations in Claude Desktop:
   - Ask Claude to list notes, create a note, etc.
   - Check Fly.io logs: fly logs -a toolbridge-mcp-staging --tail
   
5. Test token exchange (optional):
   # Get your OAuth token from claude.ai network inspector
   curl -X POST https://toolbridgeapi.erauner.dev/auth/token-exchange \\
     -H "Authorization: Bearer YOUR_MCP_OAUTH_TOKEN" \\
     -H "Content-Type: application/json" \\
     -d '{"grant_type":"urn:ietf:params:oauth:grant-type:token-exchange","audience":"https://toolbridgeapi.erauner.dev"}'

This file is kept for reference but should not be executed.
"""
