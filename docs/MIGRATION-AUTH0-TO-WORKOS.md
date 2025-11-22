# Auth0 to WorkOS AuthKit Migration Guide

**Branch**: `feat/migrate-auth0-to-workos-authkit`
**Status**: ✅ Complete (4 commits)
**Migration Date**: 2025-11-20

## Executive Summary

This migration replaces Auth0-specific authentication configuration with generic OIDC/JWT configuration, enabling support for **any OIDC provider** (WorkOS AuthKit, Auth0, Okta, Keycloak, etc.). The primary driver is adopting **WorkOS AuthKit** for improved OAuth 2.1 + PKCE authentication in the MCP (Model Context Protocol) server.

## Why Migrate?

1. **WorkOS AuthKit Advantages**:
   - OAuth 2.1 + PKCE support (better security for SPAs/mobile)
   - Simpler integration via FastMCP `AuthKitProvider`
   - Better developer experience for per-user authentication
   - Future-proof: WorkOS is designed for modern auth patterns

2. **Generic OIDC Support**:
   - No longer locked into Auth0-specific APIs
   - Easier to switch providers in the future
   - Standard OIDC patterns work with any provider

3. **Code Simplification**:
   - Python MCP: Replaced `Auth0Provider` with `AuthKitProvider` (fewer config params)
   - Go API: Generic `Issuer` + `JWKSURL` instead of hardcoded Auth0 domain logic

## What Changed

### Commit 1: Python MCP Core Migration (3ef39ef)

**Files**:
- `mcp/toolbridge_mcp/config.py`
- `mcp/toolbridge_mcp/mcp_instance.py`
- `mcp/toolbridge_mcp/server.py`
- `mcp/toolbridge_mcp/auth/token_exchange.py`
- `mcp/toolbridge_mcp/utils/requests.py`
- `mcp/.env.example`

**Changes**:
```python
# OLD (Auth0Provider)
from fastmcp.server.auth.providers.auth0 import Auth0Provider
auth_provider = Auth0Provider(
    config_url=f"https://{settings.oauth_domain}/.well-known/openid-configuration",
    client_id=settings.oauth_client_id,
    client_secret=settings.oauth_client_secret,
    audience=settings.oauth_audience,
    base_url=settings.oauth_base_url,
)

# NEW (AuthKitProvider)
from fastmcp.server.auth.providers.workos import AuthKitProvider
auth_provider = AuthKitProvider(
    authkit_domain=settings.authkit_domain,
    base_url=settings.public_base_url,
)
```

**Environment Variables**:
```bash
# OLD
TOOLBRIDGE_OAUTH_CLIENT_ID=xxx
TOOLBRIDGE_OAUTH_CLIENT_SECRET=xxx
TOOLBRIDGE_OAUTH_DOMAIN=dev-zysv6k3xo7pkwmcb.us.auth0.com
TOOLBRIDGE_OAUTH_AUDIENCE=https://toolbridge-mcp.fly.dev
TOOLBRIDGE_OAUTH_BASE_URL=https://toolbridge-mcp-staging.fly.dev

# NEW
TOOLBRIDGE_AUTHKIT_DOMAIN=svelte-monolith-27-staging.authkit.app
TOOLBRIDGE_PUBLIC_BASE_URL=https://toolbridge-mcp-staging.fly.dev
# TOOLBRIDGE_AUTHKIT_AUDIENCE=  # Optional
```

### Commit 2: Python Docs and Configs (8fb9403)

**Files**:
- `mcp/pyproject.toml`
- `Dockerfile.mcp-only`
- `fly.staging.toml`

**Changes**:
- Updated all Auth0 references in comments/docs to WorkOS AuthKit
- Updated Fly.io deployment examples with new env vars
- Updated Docker usage examples

### Commit 3: Go API Migration (c2515f7)

**Files**:
- `internal/auth/jwt.go`
- `internal/httpapi/token_exchange.go`

**Changes**:

**JWTCfg Struct**:
```go
// OLD (Auth0-specific)
type JWTCfg struct {
    HS256Secret       string
    DevMode           bool
    Auth0Domain       string   // ❌ Provider-specific
    Auth0Audience     string   // ❌ Provider-specific
    AcceptedAudiences []string
}

// NEW (Generic OIDC)
type JWTCfg struct {
    HS256Secret       string
    DevMode           bool
    Issuer            string   // ✅ Standard OIDC issuer
    JWKSURL           string   // ✅ Explicit JWKS endpoint
    Audience          string   // ✅ Optional primary audience
    AcceptedAudiences []string
}
```

**JWKS Cache**:
```go
// OLD: Derive JWKS URL from Auth0Domain
url := fmt.Sprintf("https://%s/.well-known/jwks.json", c.auth0Domain)

// NEW: Use explicit JWKSURL
resp, err := c.httpClient.Get(c.jwksURL)
```

**Token Validation**:
- Added `token_type` claim check to differentiate:
  - **Backend tokens** (`token_type="backend"`): Skip issuer/audience validation
  - **External tokens** (WorkOS, Auth0): Validate issuer and audience
- This enables token exchange flow where backend issues its own JWTs

### Commit 4: Infrastructure Config (f05270c)

**Files**:
- `cmd/server/main.go`
- `chart/values.yaml`
- `chart/templates/configmap.yaml`
- `chart/templates/deployment.yaml`
- `.env.example`
- `DEPLOYMENT.md`

**Go API Environment Variables**:
```bash
# OLD
AUTH0_DOMAIN=dev-zysv6k3xo7pkwmcb.us.auth0.com
AUTH0_AUDIENCE=https://toolbridgeapi.erauner.dev

# NEW
JWT_ISSUER=https://svelte-monolith-27-staging.authkit.app
JWT_JWKS_URL=https://svelte-monolith-27-staging.authkit.app/oauth2/jwks
JWT_AUDIENCE=https://toolbridgeapi.erauner.dev  # Optional
```

**Helm Values**:
```yaml
# OLD
api:
  auth0:
    domain: "dev-zysv6k3xo7pkwmcb.us.auth0.com"
    audience: "https://toolbridgeapi.erauner.dev"

# NEW
api:
  jwt:
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: ""  # Optional
```

## WorkOS AuthKit Configuration

### 1. MCP Server (Python)

**Environment Variables** (Fly.io secrets):
```bash
fly secrets set -a toolbridge-mcp-staging \
  TOOLBRIDGE_AUTHKIT_DOMAIN="svelte-monolith-27-staging.authkit.app" \
  TOOLBRIDGE_PUBLIC_BASE_URL="https://toolbridge-mcp-staging.fly.dev" \
  TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev" \
  TOOLBRIDGE_BACKEND_API_AUDIENCE="https://toolbridgeapi.erauner.dev" \
  TOOLBRIDGE_TENANT_ID="staging-tenant-001" \
  TOOLBRIDGE_TENANT_HEADER_SECRET="$(openssl rand -base64 32)"
```

**How It Works**:
1. User authenticates via claude.ai web UI (browser OAuth)
2. FastMCP `AuthKitProvider` validates user token against WorkOS
3. MCP server extracts user identity from token
4. Per-user API calls use token exchange to get backend JWT

### 2. Go API (Backend)

**Environment Variables** (Kubernetes secrets):
```bash
# Production Helm values (apps/toolbridge-api/helm-values-production.yaml)
api:
  jwt:
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: "https://toolbridgeapi.erauner.dev"  # Optional

  mcp:
    enabled: true
    # For WorkOS AuthKit with Dynamic Client Registration (DCR), leave empty:
    oauthAudience: ""  # Empty = skip audience validation (DCR mode)
    # For static client registration, set to MCP resource URL:
    # oauthAudience: "https://toolbridge-mcp-staging.fly.dev/mcp"
```

**How It Works**:
1. Validates WorkOS AuthKit RS256 tokens via JWKS
2. Accepts MCP OAuth tokens (via `MCP_OAUTH_AUDIENCE`)
3. Still supports HS256 backend tokens (from token exchange)
4. Backend tokens skip external validation (`token_type="backend"`)

## Backward Compatibility

### Still Using Auth0?

The **Go API** is backward compatible with Auth0 (or any OIDC provider) via generic JWT configuration:

**Go API (Auth0 Configuration)**:
```bash
JWT_ISSUER=https://dev-zysv6k3xo7pkwmcb.us.auth0.com
JWT_JWKS_URL=https://dev-zysv6k3xo7pkwmcb.us.auth0.com/.well-known/jwks.json
JWT_AUDIENCE=https://toolbridgeapi.erauner.dev
```

**Helm Values (Auth0 Configuration)**:
```yaml
api:
  jwt:
    issuer: "https://dev-zysv6k3xo7pkwmcb.us.auth0.com"
    jwksUrl: "https://dev-zysv6k3xo7pkwmcb.us.auth0.com/.well-known/jwks.json"
    audience: "https://toolbridgeapi.erauner.dev"
```

**⚠️ Important: Python MCP is WorkOS AuthKit Only**

The Python MCP server in this branch uses `AuthKitProvider`, which is **WorkOS-specific**. To use Auth0 or another OIDC provider with the MCP server, you would need to:

1. Implement a custom OAuth provider for FastMCP, or
2. Use a different authentication mechanism

The `AuthKitProvider` cannot be configured to work with Auth0 by simply changing the domain. If you need Auth0 support for the MCP server, consider staying on the previous branch or implementing a custom provider.

### Legacy Backend JWT Tokens

Existing HS256 backend tokens **without** `token_type="backend"` are automatically recognized as backend tokens if they have `iss="toolbridge-api"`. This ensures zero-downtime migration.

New tokens issued via `/auth/token-exchange` include `token_type="backend"` for explicit identification.

## Security Considerations

### Audience Validation (IMPORTANT)

**⚠️ SECURITY NOTICE**: When configuring upstream OIDC (`JWT_ISSUER` + `JWT_JWKS_URL`), you should **always configure `JWT_AUDIENCE`** or additional accepted audiences via `MCP_OAUTH_AUDIENCE`.

Without audience validation:
- The API will accept **any token** issued by the IdP, regardless of the `aud` claim
- This means tokens scoped to other applications in the same tenant will be accepted
- Example: A token meant for `https://other-app.example.com` would be accepted

**Recommended configuration**:
```yaml
api:
  jwt:
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: "https://toolbridgeapi.erauner.dev"  # ✅ Explicitly set

  mcp:
    enabled: true
    # WorkOS AuthKit DCR (recommended):
    oauthAudience: ""  # ✅ Empty = skip audience validation (DCR mode)
    # Static client registration:
    # oauthAudience: "https://toolbridge-mcp-staging.fly.dev/mcp"
```

The Go API will log a warning if OIDC is configured without audience validation.

### MCP Resource Audience (IMPORTANT)

**WorkOS AuthKit with Dynamic Client Registration (DCR)**:

When using WorkOS AuthKit with DCR, tokens have **unpredictable client IDs as the audience** (e.g., `client_01KABXHNQ09QGWEX4APPYG2AH5`), not the resource URL. Set `MCP_OAUTH_AUDIENCE=""` (empty) to skip audience validation. The Go API will validate issuer and signature instead.

See detailed documentation: [docs/workos-dcr-authentication.md](workos-dcr-authentication.md)

**Static Client Registration (non-DCR)**:

For static client registration, the Python MCP server (FastMCP + `AuthKitProvider`) exposes `/.well-known/oauth-protected-resource/{path}` and declares a `resource` URL. The Go API must configure `MCP_OAUTH_AUDIENCE` to match this resource value.

**Discovering the MCP resource:**
```bash
# Path-based discovery (FastMCP default for HTTP transport)
curl https://toolbridge-mcp-staging.fly.dev/.well-known/oauth-protected-resource/mcp

# Response contains the canonical resource ID:
{
  "resource": "https://toolbridge-mcp-staging.fly.dev/mcp",
  "authorization_servers": ["https://svelte-monolith-27-staging.authkit.app/"],
  ...
}
```

For static registration, the `resource` value **must** match `MCP_OAUTH_AUDIENCE` in the Go backend configuration.

### Option 2 (MCP-Issued RS256 JWTs) Requirements

If using Option 2 (MCP server issues RS256 backend JWTs), note that:

1. **JWKS Setup Required**: The Go API validates RS256 tokens exclusively against `JWT_JWKS_URL`
2. **Public Key Exposure**: You must expose the public half of `TOOLBRIDGE_JWT_SIGNING_KEY` via a JWKS endpoint
3. **Token Header**: RS256 tokens must include a `kid` (key ID) header matching a key in the JWKS
4. **Configuration**: Set `JWT_ISSUER` and `JWT_JWKS_URL` to point at your JWKS endpoint

**Recommendation**: Option 2 is primarily for advanced/custom setups. Most deployments should use Option 1 (backend `/auth/token-exchange` endpoint) which uses HS256 tokens and doesn't require JWKS management.

## Testing Plan

### 1. MCP Server (Python)
```bash
# Test MCP protected resource metadata (path-based discovery)
curl https://toolbridge-mcp-staging.fly.dev/.well-known/oauth-protected-resource/mcp

# Expected response (confirms the canonical resource ID):
{
  "resource": "https://toolbridge-mcp-staging.fly.dev/mcp",
  "authorization_servers": ["https://svelte-monolith-27-staging.authkit.app/"],
  "scopes_supported": [],
  "bearer_methods_supported": ["header"]
}

# NOTE: With WorkOS AuthKit DCR, the `resource` field is for metadata discovery only.
# Tokens will have unpredictable client IDs as audience, not this resource URL.
# Set MCP_OAUTH_AUDIENCE="" (empty) to skip audience validation.

# Test WorkOS AuthKit authorization server metadata (forwarded by FastMCP)
curl https://toolbridge-mcp-staging.fly.dev/.well-known/oauth-authorization-server

# Expected response:
{
  "authorization_endpoint": "https://svelte-monolith-27-staging.authkit.app/oauth2/authorize",
  "token_endpoint": "https://svelte-monolith-27-staging.authkit.app/oauth2/token",
  ...
}
```

**Note:** The `resource` field from the first response should match `MCP_OAUTH_AUDIENCE` in the Go backend configuration.

### 2. Go API Token Validation

**Test WorkOS AuthKit Token**:
```bash
# Get token from claude.ai (inspect network tab)
export MCP_TOKEN="your-workos-token"

curl https://toolbridgeapi.erauner.dev/v1/notes \
  -H "Authorization: Bearer $MCP_TOKEN"
```

**Test Token Exchange**:
```bash
curl -X POST https://toolbridgeapi.erauner.dev/auth/token-exchange \
  -H "Authorization: Bearer $MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
    "audience": "https://toolbridgeapi.erauner.dev"
  }'
```

### 3. End-to-End (Claude Desktop → MCP → Go API)

1. Add connector in claude.ai:
   - URL: `https://toolbridge-mcp-staging.fly.dev`
   - Complete OAuth flow via browser (WorkOS AuthKit)
2. Use MCP tools in claude.ai (e.g., list notes)
3. Verify backend API logs show per-user requests

## Deployment Steps

### 1. Deploy Python MCP (Fly.io)

```bash
# Update secrets with WorkOS AuthKit config
fly secrets set -a toolbridge-mcp-staging \
  TOOLBRIDGE_AUTHKIT_DOMAIN="svelte-monolith-27-staging.authkit.app" \
  TOOLBRIDGE_PUBLIC_BASE_URL="https://toolbridge-mcp-staging.fly.dev" \
  TOOLBRIDGE_BACKEND_API_AUDIENCE="https://toolbridgeapi.erauner.dev"

# Deploy
fly deploy --config fly.staging.toml -a toolbridge-mcp-staging

# Verify
fly logs -a toolbridge-mcp-staging --tail
```

### 2. Deploy Go API (Kubernetes)

```bash
# Update Helm values for production
# File: apps/toolbridge-api/helm-values-production.yaml
api:
  jwt:
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: ""  # Optional

  mcp:
    enabled: true
    oauthAudience: ""  # Empty for WorkOS AuthKit DCR (recommended)
    # For static registration: "https://toolbridge-mcp.fly.dev/mcp"

# Sync via ArgoCD
argocd app sync toolbridge-api-production

# Verify
kubectl logs -n toolbridge -l app=toolbridge-api --tail=50 | grep -i "oidc\|workos"
```

### 3. Update Claude Desktop Connector

1. Remove old Auth0 connector (if configured)
2. Add new connector:
   - URL: `https://toolbridge-mcp-staging.fly.dev`
   - Complete OAuth flow (WorkOS AuthKit)
3. Test MCP tools in claude.ai

## Rollback Plan

If issues arise, revert environment variables to Auth0:

### Fly.io (Python MCP)
```bash
fly secrets set -a toolbridge-mcp-staging \
  TOOLBRIDGE_AUTHKIT_DOMAIN="dev-zysv6k3xo7pkwmcb.us.auth0.com" \
  TOOLBRIDGE_PUBLIC_BASE_URL="https://toolbridge-mcp-staging.fly.dev"
```

### Kubernetes (Go API)
```bash
# Revert Helm values to Auth0
api:
  jwt:
    issuer: "https://dev-zysv6k3xo7pkwmcb.us.auth0.com"
    jwksUrl: "https://dev-zysv6k3xo7pkwmcb.us.auth0.com/.well-known/jwks.json"
    audience: "https://toolbridgeapi.erauner.dev"
```

## MCP Transport Migration: SSE → Streamable HTTP

**Migration Date**: 2025-11-22
**Status**: ✅ Complete

The MCP server transport has been migrated from SSE (Server-Sent Events) to Streamable HTTP with an explicit path.

### What Changed

**MCP Endpoint Path**:
- **Old**: `https://toolbridge-mcp-staging.fly.dev/sse` (SSE transport)
- **New**: `https://toolbridge-mcp-staging.fly.dev/mcp` (Streamable HTTP)

**Transport Configuration**:
```python
# Before
mcp.run(transport="sse", host=settings.host, port=settings.port)

# After
mcp.run(transport="http", host=settings.host, port=settings.port, path="/mcp")
```

### Why This Change?

1. **Standardization**: Streamable HTTP is the recommended transport for production MCP servers
2. **Better Integration**: Aligns with MCP best practices and tooling expectations
3. **Clearer Semantics**: Explicit `/mcp` path makes the endpoint purpose obvious
4. **Auth Metadata**: OAuth protected resource metadata now correctly reflects `/mcp` endpoint

### Impact on Clients

**⚠️ Breaking Change**: Existing MCP clients configured with the old `/sse` endpoint will need to update their connection URL.

**Claude Desktop / claude.ai Connectors**:
1. Remove old connector (if configured with `/sse`)
2. Add new connector with updated URL:
   - **Old**: `https://toolbridge-mcp-staging.fly.dev` or `.../sse`
   - **New**: `https://toolbridge-mcp-staging.fly.dev/mcp`
3. Re-authenticate via browser (WorkOS AuthKit OAuth flow)

**OAuth Metadata Location**:
- **Old**: `/.well-known/oauth-protected-resource`
- **New**: `/.well-known/oauth-protected-resource/mcp`

### Files Modified

- `mcp/toolbridge_mcp/server.py` - Transport switch and logging updates
- `mcp/toolbridge_mcp/mcp_instance.py` - Added clarifying comments about base_url
- `mcp/.env.example` - Added notes about `/mcp` endpoint
- `fly.staging.toml` - Updated deployment comments and example URLs

### Testing

Verify the new endpoint:
```bash
# Check OAuth metadata
curl https://toolbridge-mcp-staging.fly.dev/.well-known/oauth-protected-resource/mcp

# Connect with MCP client
# Update your client URL to: https://toolbridge-mcp-staging.fly.dev/mcp
```

## Next Steps

1. ✅ **Code Migration**: Complete (4 commits on `feat/migrate-auth0-to-workos-authkit`)
2. ✅ **Transport Migration**: Complete (SSE → HTTP at `/mcp`)
3. ⏳ **Deploy to Staging**: Test WorkOS AuthKit flow end-to-end with new `/mcp` endpoint
4. ⏳ **Production Deployment**: Update production Helm values and secrets
5. ⏳ **Update Claude Connectors**: Reconfigure claude.ai connectors with new `/mcp` URL
6. ⏳ **Archive Auth0 Docs**: Move Auth0-specific docs to `_archive/` (optional cleanup)

## Additional Resources

- **WorkOS AuthKit Docs**: https://workos.com/docs/authkit
- **FastMCP AuthKit Integration**: https://github.com/jlowin/fastmcp (see `auth` module)
- **RFC 8693 Token Exchange**: https://datatracker.ietf.org/doc/html/rfc8693
- **OIDC Discovery**: https://openid.net/specs/openid-connect-discovery-1_0.html

## Questions?

Contact: @erauner12
Branch: `feat/migrate-auth0-to-workos-authkit`
Commits: 3ef39ef, 8fb9403, c2515f7, f05270c
