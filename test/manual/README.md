# Manual Tests

This directory contains manual integration tests that demonstrate and verify the OIDC tenant resolution flow.

## Tests Overview

### `test_full_tenant_flow.go` ⭐ **START HERE**

**Purpose**: Complete end-to-end reference implementation of tenant resolution flow

**What it demonstrates**:
1. OIDC Discovery (`.well-known/openid-configuration`)
2. PKCE authentication flow (code verifier/challenge)
3. Authorization via browser
4. Token exchange (authorization code → ID token)
5. Backend tenant resolution (`/v1/auth/tenant` endpoint)
6. Complete flow with proper error handling

**Usage**:
```bash
# Test against local backend
go run test/manual/test_full_tenant_flow.go

# Test against production
BACKEND_URL=https://toolbridgeapi.erauner.dev go run test/manual/test_full_tenant_flow.go
```

**Output**:
```
=== Complete Tenant Resolution Flow Test ===
Step 1: Discovering OIDC endpoints...
   ✓ Authorization: https://...
Step 2: Generating PKCE parameters...
   ✓ Code verifier: ...
Step 3: Building authorization URL...
   ✓ Authorization URL ready
Step 4: Performing authorization (browser will open)...
   ✓ Authorization code: ...
Step 5: Exchanging authorization code for tokens...
   ✓ ID token obtained: ...
Step 6: Calling backend /v1/auth/tenant endpoint...
   Backend URL: https://toolbridgeapi.erauner.dev
   ✓ Tenant resolved successfully!

=== Tenant Resolution Result ===
Tenant ID: org_01KABXHNF45RMV9KBWF3SBGPP0
Organization Name: Test Organization
Requires Selection: false

=== Flow Complete ===
✅ SUCCESS - Full tenant resolution flow completed
```

**Use this as reference** when implementing the Flutter client!

---

### `test_rs256_backend_signing.go`

**Purpose**: Verify RS256 backend token signing and validation

**What it tests**:
1. RSA key pair generation (PKCS#8 format)
2. Backend signer initialization via `InitBackendSigner`
3. Token signing with RS256 via `SignBackendToken`
4. Token validation with correct `kid` routing
5. HS256 fallback when RS256 not configured
6. Wrong `kid` correctly routes to JWKS (and fails without cache)

**Usage**:
```bash
go run test/manual/test_rs256_backend_signing.go
```

**Expected Output**:
```
======================================================================
  RS256 Backend Signing Integration Test
======================================================================

Test 1: Initialize backend signer with generated RSA key
----------------------------------------------------------------------
  OK: Generated 2048-bit RSA key pair
  OK: Encoded as PKCS#8 PEM format
  OK: Backend signer initialized successfully

Test 2: Sign backend token with RS256
----------------------------------------------------------------------
  OK: Token signed successfully
  Token Header: {"alg":"RS256","kid":"integration-test-key-1","typ":"JWT"}
  OK: Algorithm is RS256
  OK: Key ID (kid) is correct

...

======================================================================
  Results: 5 passed, 0 failed
======================================================================
```

**No database or server required** - this test runs entirely in-process.

---

### `prove_oidc_tokens_lack_org_id.go`

**Purpose**: Prove that standard OIDC tokens do NOT contain `organization_id` claim

**What it demonstrates**:
- Standard OIDC/PKCE authentication flow (identical to what Flutter clients use)
- Explicit token inspection showing JWT claims
- Proof that `organization_id` is missing from both ID token and access token
- Validates the architectural necessity of backend-driven tenant resolution

**Why this test exists**:
This test provides concrete proof that client-side JWT inspection cannot determine tenant ID. The "FAIL" result is the **expected and correct** behavior that validates our backend tenant resolution architecture.

**Usage**:
```bash
go run test/manual/prove_oidc_tokens_lack_org_id.go
```

**Expected Output**:
```
--- ID Token ---
   Claims: {
     "aud": "client_01KAPCBQNQBWMZE9WNSEWY2J3Z",
     "email": "raunerevan@gmail.com",
     "exp": 1764004121,
     "iat": 1764000521,
     "iss": "https://svelte-monolith-27-staging.authkit.app",
     "name": "Evan Rauner",
     "sub": "user_01KAHS4J1W6TT5390SR3918ZPF"
   }

✗ FAIL: No organization_id claim found in ID token
```

The "FAIL" message indicates the absence of `organization_id`, which proves that client-side token inspection cannot determine tenant ID and backend resolution is required.

---

## Configuration

All tests use the following WorkOS AuthKit configuration:

```go
const (
    issuerURL      = "https://svelte-monolith-27-staging.authkit.app"
    clientID       = "client_01KAPCBQNQBWMZE9WNSEWY2J3Z"
    redirectURI    = "http://localhost:3000/callback"
    // organizationID = "org_01KABXHNF45RMV9KBWF3SBGPP0" // Optional: Omit for B2C mode
)
```

### B2C vs B2B Modes

**B2C Mode (Default)**: Users authenticate without specifying an organization. The backend assigns the default tenant (`tenant_thinkpen_b2c`) for users who are not members of any organization. This is the primary use case for ThinkPen.

**B2B Mode**: Users who belong to organizations receive their organization ID as the tenant. Organization membership is determined by the backend via WorkOS API, not by client-side token inspection.

To test B2C mode, simply omit the `organization_id` parameter from the authorization request (see `test_full_tenant_flow.go` with the parameter commented out).

### Environment Variables

- `BACKEND_URL`: Backend API base URL (default: `http://localhost:8080`)
  - Production: `https://toolbridgeapi.erauner.dev`
  - Local: `http://localhost:8080`

- `ID_TOKEN`: ID token for testing tenant resolution endpoint
  - Get from `prove_oidc_tokens_lack_org_id.go` output

## Expected Behavior

### B2C User (No Organization Membership)

When a user is not a member of any organization (default B2C flow):
```json
{
  "tenant_id": "tenant_thinkpen_b2c",
  "organization_name": "ThinkPen",
  "requires_selection": false
}
```

This is the **primary use case** for ThinkPen's individual consumer audience.

### B2B User (Single Organization)

When a user belongs to one organization:
```json
{
  "tenant_id": "org_01KABXHNF45RMV9KBWF3SBGPP0",
  "organization_name": "Test Organization",
  "requires_selection": false
}
```

### B2B User (Multiple Organizations)

When a user belongs to multiple organizations:
```json
{
  "organizations": [
    {"id": "org_01...", "name": "Acme Corp"},
    {"id": "org_02...", "name": "Globex Inc"}
  ],
  "requires_selection": true
}
```

Client must present selection UI (not yet implemented in Flutter).

## Troubleshooting

### "Token is expired" Error

ID tokens expire after 1 hour. Get a fresh token:
```bash
go run test/manual/prove_oidc_tokens_lack_org_id.go
```

### "WorkOS tenant resolution not configured"

Backend missing `WORKOS_API_KEY` environment variable. Check:
```bash
kubectl get secret toolbridge-secret -n toolbridge -o jsonpath='{.data.workos-api-key}' | base64 -d
```

### "Invalid token"

Token validation failed. Possible causes:
- Token expired (get fresh token)
- Token audience mismatch (check JWT_AUDIENCE config)
- JWKS fetch failed (check network/DNS)

## Implementation Notes for Flutter

Key takeaways from these tests:

1. **Use flutter_appauth** for OIDC/PKCE flow (mirrors Go implementation)
2. **Store tokens securely** using flutter_secure_storage
3. **Call `/v1/auth/tenant`** immediately after authentication
4. **Cache tenant_id** for use in subsequent API requests
5. **Handle multi-org case** by showing selection UI (future work)

See `docs/tenant-resolution.md` for complete implementation guide.

## Related Documentation

- Main documentation: `/docs/tenant-resolution.md`
- WorkOS AuthKit: https://workos.com/docs/authkit
- OIDC Spec: https://openid.net/specs/openid-connect-core-1_0.html
- PKCE RFC: https://datatracker.ietf.org/doc/html/rfc7636
