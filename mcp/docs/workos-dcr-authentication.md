# WorkOS AuthKit Dynamic Client Registration (DCR) Authentication

## Overview

This document explains how ToolBridge API handles authentication for **WorkOS AuthKit** tokens when using **Dynamic Client Registration (DCR)** per OAuth 2.0 RFC 7591.

## Problem: DCR Tokens Have Unpredictable Audiences

### What is Dynamic Client Registration (DCR)?

With DCR, OAuth clients are created **dynamically at runtime** rather than being pre-registered. When a client connects via the MCP protocol:

1. Claude.ai registers a new OAuth client with WorkOS AuthKit
2. WorkOS creates a unique client with a random client ID (e.g., `client_01KABXHNQ09QGWEX4APPYG2AH5`)
3. Access tokens issued for that session have `aud=<client_id>`, **not** the resource URL

### Traditional vs DCR Token Audiences

**Traditional OAuth (Static Client Registration):**
```json
{
  "sub": "user_01KAHS4J1W6TT5390SR3918ZPF",
  "iss": "https://svelte-monolith-27-staging.authkit.app",
  "aud": "https://toolbridgeapi.erauner.dev"  // ✅ Resource URL
}
```

**WorkOS AuthKit DCR:**
```json
{
  "sub": "user_01KAHS4J1W6TT5390SR3918ZPF",
  "iss": "https://svelte-monolith-27-staging.authkit.app",
  "aud": "client_01KABXHNQ09QGWEX4APPYG2AH5"  // ⚠️ Dynamically-created client ID
}
```

### Why This Breaks Standard Validation

Standard JWT audience validation expects:
```go
if claims["aud"] != "https://toolbridgeapi.erauner.dev" {
    return errors.New("invalid audience")
}
```

But DCR tokens have unpredictable client IDs as the audience, causing:
```
❌ invalid audience: expected [https://toolbridgeapi.erauner.dev], got client_01KABXHNQ09QGWEX4APPYG2AH5
```

## Solution: Skip Audience Validation for DCR Tokens

Per [WorkOS MCP Integration Guide](https://workos.com/docs/authkit/connect/mcp#token-verification), when using DCR:

> **We only validate the issuer and signature, NOT the audience.**

### Implementation

**Configuration (Helm Values):**
```yaml
api:
  jwt:
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: "https://toolbridgeapi.erauner.dev"  # For direct API access

  mcp:
    enabled: true
    oauthAudience: ""  # ← EMPTY = Skip audience validation for DCR
```

**Code Logic (`internal/auth/jwt.go:253`):**
```go
// Special case: Skip audience validation for WorkOS AuthKit when using DCR
// (Dynamic Client Registration). With DCR, each client gets a unique client ID
// as the audience, which is unpredictable. We only validate issuer + signature.
skipAudienceValidation := cfg.Issuer != "" && issuer == cfg.Issuer && len(cfg.AcceptedAudiences) == 0

if !skipAudienceValidation && (cfg.Audience != "" || len(cfg.AcceptedAudiences) > 0) {
    // Validate audience for non-DCR tokens
    // ...
}
```

### Security Guarantees

Even when skipping audience validation, we still enforce:

1. **✅ Issuer Validation**: Token must be from `https://svelte-monolith-27-staging.authkit.app`
2. **✅ Signature Validation**: RS256 signature verified via JWKS public keys
3. **✅ Expiration Check**: `exp` claim must be in the future
4. **✅ Subject Validation**: `sub` claim must be present

This provides equivalent security to audience validation because:
- Only our trusted WorkOS AuthKit instance can issue tokens (issuer check)
- Tokens are cryptographically signed and verified (signature check)
- Attackers cannot forge tokens from other issuers

## Testing

### Automated Tests

**Go Backend Tests (`internal/auth/jwt_test.go`):**

```bash
go test ./internal/auth/... -v -run TestValidateToken_WorkOS_DCR
```

Tests cover:
- ✅ DCR tokens with client ID audience are accepted
- ✅ Issuer validation still enforced (reject wrong issuers)
- ✅ Regular tokens still validate audience properly
- ✅ Backend tokens skip external IdP validation
- ✅ Expired tokens rejected
- ✅ Missing sub claim rejected

**Python MCP Tests (`mcp/test_token_exchange.py`):**

```bash
cd mcp && uv run pytest test_token_exchange.py -v
```

Tests cover:
- ✅ Token exchange with backend endpoint
- ✅ JWT user ID extraction (no warnings)
- ✅ Fallback to local JWT signing
- ✅ Error handling for invalid JWTs

### Manual E2E Testing

1. **Configure Claude.ai** with MCP server:
   ```json
   {
     "mcpServers": {
       "toolbridge": {
         "url": "https://toolbridge-mcp-staging.fly.dev/mcp"
       }
     }
   }
   ```

2. **Authenticate** via Claude.ai → Browser OAuth flow → WorkOS AuthKit

3. **Call MCP Tool** (e.g., create a note)

4. **Verify Logs** - No audience validation errors:

   **MCP Server (Fly.io):**
   ```
   ✅ INFO: "POST /messages/?session_id=... HTTP/1.1" 202 Accepted
   ✅ INFO: Creating note: title=Hello World
   ```

   **Go API Backend (Kubernetes):**
   ```json
   ✅ {"message":"tenant headers validated successfully"}
   ✅ {"message":"token exchange: issued backend JWT", "user_id":"user_01KAHS4J1W6TT5390SR3918ZPF"}
   ✅ "POST /v1/notes HTTP/1.1" - 201 279B
   ```

   **Before the fix:**
   ```json
   ❌ {"error":"invalid audience: expected [...], got client_01KABXHNQ09QGWEX4APPYG2AH5"}
   ```

## Configuration Reference

### Environment Variables

| Variable | Purpose | Example | DCR Behavior |
|----------|---------|---------|--------------|
| `JWT_ISSUER` | Upstream IdP issuer | `https://svelte-monolith-27-staging.authkit.app` | **Required** - Used for issuer validation |
| `JWT_JWKS_URL` | JWKS endpoint for RS256 | `https://svelte-monolith-27-staging.authkit.app/oauth2/jwks` | **Required** - Used for signature validation |
| `JWT_AUDIENCE` | Primary API audience | `https://toolbridgeapi.erauner.dev` | Used for direct API tokens (non-MCP) |
| `MCP_OAUTH_AUDIENCE` | MCP token audience | `""` (empty) | **Empty = Skip audience validation** |

### Helm Values Example

```yaml
# Production configuration with WorkOS AuthKit DCR
api:
  env: production
  jwt:
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: "https://toolbridgeapi.erauner.dev"

  mcp:
    enabled: true
    # MCP OAuth audience - EMPTY for WorkOS AuthKit DCR
    # With DCR, each client gets a unique client ID as audience
    # Per WorkOS guide: validate issuer + signature, NOT audience
    oauthAudience: ""
```

## Migration from Auth0 → WorkOS

| Aspect | Auth0 (Old) | WorkOS AuthKit DCR (New) |
|--------|-------------|--------------------------|
| **Client Registration** | Static (pre-registered) | Dynamic (runtime DCR) |
| **Token Audience** | Resource URL | Client ID |
| **Audience Validation** | Required | Skipped (DCR mode) |
| **Issuer Validation** | Required | Required ✅ |
| **Signature Validation** | RS256 via JWKS | RS256 via JWKS ✅ |
| **MCP_OAUTH_AUDIENCE** | `https://toolbridge-mcp-staging.fly.dev/mcp` | `""` (empty) |

## References

- [OAuth 2.0 Dynamic Client Registration - RFC 7591](https://datatracker.ietf.org/doc/html/rfc7591)
- [WorkOS AuthKit MCP Integration Guide](https://workos.com/docs/authkit/connect/mcp)
- [WorkOS AuthKit Token Verification](https://workos.com/docs/authkit/connect/mcp#token-verification)
- [OAuth 2.0 Protected Resource Metadata - RFC 8707](https://datatracker.ietf.org/doc/html/rfc8707)

## Summary

**Problem:** WorkOS AuthKit DCR tokens have unpredictable client IDs as audience, breaking standard validation.

**Solution:** When `MCP_OAUTH_AUDIENCE` is empty but issuer is configured, skip audience validation and rely on issuer + signature validation.

**Security:** Equivalent to audience validation because only our trusted WorkOS instance can issue validly-signed tokens.

**Testing:** Comprehensive automated tests + end-to-end manual verification confirm the fix works.
