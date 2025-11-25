# Backend-Driven Tenant Resolution

## Overview

This document describes the backend-driven tenant resolution system implemented for WorkOS AuthKit integration. This approach solves the problem of determining which organization/tenant a user belongs to when using standard OIDC authentication flows.

## Problem Statement

When using WorkOS AuthKit with standard OIDC authentication (PKCE flow), ID tokens and access tokens do not contain an `organization_id` claim. This creates a challenge for multi-tenant applications that need to know which tenant a user belongs to before making API calls.

### Why Standard OIDC Doesn't Include Organization ID

WorkOS AuthKit uses standard OIDC flows that return JWT tokens conforming to the OIDC spec. The `organization_id` is organization-specific metadata that isn't part of the standard claims (`sub`, `iss`, `aud`, `exp`, etc.). While WorkOS's User Management API *does* provide organization information, it requires client secrets which cannot be used in public clients (Flutter mobile apps, browser-based applications).

## Solution Architecture

We implement a **backend tenant resolution** pattern where:

1. **Public clients** (Flutter, Python MCP) use standard OIDC/PKCE for authentication
2. After authentication, clients call a **backend API endpoint** with their ID token
3. The backend validates the token and uses a **server-side API key** to query WorkOS
4. The backend returns the `tenant_id` to the client
5. Clients cache the tenant ID and use it in subsequent requests

### B2C/B2B Hybrid Pattern (Pattern 3)

**Product Context**: ThinkPen is a **B2C note-taking application** for individual consumers. Users don't need to belong to an organization to use the app. A future B2B product (ThinkPort) will add organization/workspace features.

**Implementation**: We use **Pattern 3 (Hybrid)** to support both B2C and B2B users:

- **B2C Users** (no organization memberships) → Receive default tenant `tenant_thinkpen_b2c`
- **B2B Users** (single organization) → Receive their organization ID as tenant
- **Multi-org Users** (multiple organizations) → Must select which organization to access

This pattern allows:
- ✅ Individual consumers to sign up and use ThinkPen immediately (no org required)
- ✅ Future B2B customers to use organization-based tenancy
- ✅ Database isolation via (`tenant_id`, `user_id`) compound keys
- ✅ Seamless user experience for the primary B2C use case

**Configuration**: The default B2C tenant ID is configurable via the `DEFAULT_TENANT_ID` environment variable (defaults to `tenant_thinkpen_b2c`).

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Client    │  OIDC   │   Backend   │  API    │   WorkOS    │
│ (Flutter/   │ ──────> │     API     │ ──────> │             │
│  Python)    │ PKCE    │             │ Secret  │             │
└─────────────┘         └─────────────┘         └─────────────┘
      │                       │                       │
      │ 1. Standard OIDC Auth │                       │
      │ <──────────────────────────────────────────> │
      │                       │                       │
      │ 2. ID Token           │                       │
      │ <────────────────────────────────────────── │
      │                       │                       │
      │ 3. GET /v1/auth/tenant│                       │
      │    Bearer <id_token>  │                       │
      │ ──────────────────> │                       │
      │                       │                       │
      │                       │ 4. ListOrganization   │
      │                       │    Memberships(sub)   │
      │                       │ ──────────────────> │
      │                       │                       │
      │                       │ 5. organization_id    │
      │                       │ <────────────────── │
      │                       │                       │
      │ 6. {tenant_id: "..."}│                       │
      │ <──────────────────   │                       │
```

## Implementation

### Backend API Endpoint

**Endpoint**: `GET /v1/auth/tenant`

**Authentication**: Bearer token (ID token from OIDC)

**B2C Response** (user has no organization memberships):
```json
{
  "tenant_id": "tenant_thinkpen_b2c",
  "organization_name": "ThinkPen",
  "requires_selection": false
}
```

**B2B Response** (user belongs to single organization):
```json
{
  "tenant_id": "org_01KABXHNF45RMV9KBWF3SBGPP0",
  "organization_name": "Test Organization",
  "requires_selection": false
}
```

**Multi-Organization Response** (user belongs to multiple organizations):
```json
{
  "organizations": [
    {"id": "org_01...", "name": "Acme Corp"},
    {"id": "org_02...", "name": "Globex Inc"}
  ],
  "requires_selection": true
}
```

### Code Implementation

**Handler**: `internal/httpapi/tenant_resolve.go:41`

Key aspects:
- Validates ID token from `Authorization` header
- Extracts `sub` (user ID) from token claims
- Calls WorkOS `ListOrganizationMemberships` API with server-side API key
- Returns organization ID(s) for the user

**Router Configuration**: `internal/httpapi/router.go:139`

The endpoint is placed **before** tenant header middleware to avoid chicken-and-egg problem:
```go
// Bootstrap endpoints that don't require tenant headers
r.Get("/v1/auth/tenant", s.ResolveTenant)

// Routes that require tenant header validation
r.Group(func(r chi.Router) {
    // tenant header middleware applied here
    // all other endpoints...
})
```

### Environment Configuration

**Backend Configuration**:
- `WORKOS_API_KEY`: Server-side API key for calling WorkOS API
- `DEFAULT_TENANT_ID`: Default tenant ID for B2C users without organization memberships (default: `tenant_thinkpen_b2c`)
- `JWT_ISSUER`: WorkOS AuthKit issuer URL (for token validation)
- `JWT_JWKS_URL`: JWKS endpoint for signature verification
- `JWT_AUDIENCE`: Expected audience (optional, DCR mode skips this)

**Kubernetes Deployment**:
```yaml
# chart/templates/deployment.yaml
- name: WORKOS_API_KEY
  valueFrom:
    secretKeyRef:
      name: toolbridge-secret
      key: workos-api-key
      optional: true
```

## Client Implementation

### Reference Implementation

See `test/manual/test_full_tenant_flow.go` for a complete Go example that demonstrates:

1. **OIDC Discovery**: Fetch `.well-known/openid-configuration`
2. **PKCE Flow**: Generate code verifier/challenge, build authorization URL
3. **Authorization**: User authenticates via browser
4. **Token Exchange**: Exchange authorization code for ID token
5. **Tenant Resolution**: Call `/v1/auth/tenant` with ID token
6. **Result**: Receive and cache `tenant_id`

Run the test:
```bash
BACKEND_URL=https://toolbridgeapi.erauner.dev go run test/manual/test_full_tenant_flow.go
```

### Flutter Client Implementation

**Recommended Packages**:
- `flutter_appauth`: OIDC/PKCE authentication
- `flutter_secure_storage`: Secure token storage
- `http`: HTTP client for API calls

**Implementation Steps**:

1. **OIDC Authentication** (B2C mode - no organization required):
```dart
final AuthorizationTokenResponse result = await appAuth.authorizeAndExchangeCode(
  AuthorizationTokenRequest(
    clientId,
    redirectUrl,
    issuer: issuerUrl,
    scopes: ['openid', 'profile', 'email'],
    promptValues: ['login'],
    // Note: No organization_id parameter needed for B2C mode
    // Backend will assign default tenant for users without org memberships
  ),
);
String idToken = result.idToken;
```

2. **Tenant Resolution**:
```dart
final response = await http.get(
  Uri.parse('$backendUrl/v1/auth/tenant'),
  headers: {
    'Authorization': 'Bearer $idToken',
    'Accept': 'application/json',
  },
);

if (response.statusCode == 200) {
  final data = jsonDecode(response.body);
  String tenantId = data['tenant_id'];

  // Cache tenant ID for subsequent requests
  await storage.write(key: 'tenant_id', value: tenantId);
}
```

3. **Using Tenant ID in Requests**:
```dart
// All authenticated API calls include tenant_id
final response = await http.get(
  Uri.parse('$backendUrl/v1/notes'),
  headers: {
    'Authorization': 'Bearer $accessToken',
    'X-Tenant-ID': tenantId, // HMAC-signed header (if using MCP proxy)
  },
);
```

## Testing

### Manual Testing

1. **Get ID Token**:
```bash
go run test/manual/standard_oidc_tenant.go
# Copy the exported ID_TOKEN
```

2. **Test Endpoint**:
```bash
curl -H "Authorization: Bearer $ID_TOKEN" \
     https://toolbridgeapi.erauner.dev/v1/auth/tenant
```

3. **Expected Response**:
```json
{
  "tenant_id": "org_01KABXHNF45RMV9KBWF3SBGPP0",
  "organization_name": "Test Organization",
  "requires_selection": false
}
```

### Integration Testing

Run the complete flow test:
```bash
# Local backend
go run test/manual/test_full_tenant_flow.go

# Production backend
BACKEND_URL=https://toolbridgeapi.erauner.dev go run test/manual/test_full_tenant_flow.go
```

## Security Considerations

### Token Validation

The backend performs full JWT validation:
- **Signature**: Verifies RS256 signature using JWKS from WorkOS
- **Issuer**: Validates `iss` claim matches expected issuer
- **Expiration**: Validates `exp` claim
- **Audience**: DCR mode (empty `AcceptedAudiences`) skips audience validation for flexibility

### API Key Security

- `WORKOS_API_KEY` is stored in Kubernetes secrets (SOPS-encrypted)
- Never exposed to public clients
- Used only server-side for WorkOS API calls

### Tenant Authorization

This endpoint resolves **which tenant** a user belongs to. It does NOT authorize access to that tenant's data. Authorization is handled separately via:
- Session management (`X-Sync-Session` header)
- Tenant header validation (for MCP deployments)
- Row-level security in database queries

### Fail-Closed Security Model

The tenant authorization middleware uses a **fail-closed** security model:

**When `WORKOS_API_KEY` is configured (production multi-tenant mode):**
- B2C users (no org memberships) → Access only the default tenant (`DEFAULT_TENANT_ID`)
- B2B users → Access only organizations they belong to (validated via WorkOS API)
- All other tenant access attempts are **denied**

**When `WORKOS_API_KEY` is NOT configured (single-tenant/smoke-test mode):**
- Users can **only** access the default tenant (`DEFAULT_TENANT_ID`)
- All other tenant access attempts are **denied**
- This prevents misconfigured deployments from allowing arbitrary cross-tenant access

This fail-closed design ensures that:
- A missing or misconfigured `WORKOS_API_KEY` does NOT grant unrestricted access
- Production deployments MUST set `WORKOS_API_KEY` for proper B2B organization validation
- Single-tenant deployments are restricted to the configured default tenant

## Multi-Organization Support

When a user belongs to multiple organizations:

1. Backend returns `requires_selection: true`
2. Response includes array of all organizations
3. Client must present organization selection UI
4. User selects which organization to access
5. Client caches the selected `tenant_id`

**Current Status**: Multi-org UI not yet implemented in Flutter client. Users with multiple organizations will see a warning and cannot proceed.

## Migration Notes

### For Existing Deployments

1. **Add WorkOS API Key**:
   - Obtain API key from WorkOS dashboard
   - Add to Kubernetes secret: `workos-api-key`

2. **Update Helm Chart**:
   - Add `WORKOS_API_KEY` environment variable
   - Already included in current deployment

3. **Deploy**:
   - Helm automatically injects the secret
   - Endpoint becomes available at `/v1/auth/tenant`

### For Client Applications

1. **Add Tenant Resolution Call**:
   - After OIDC authentication, call `/v1/auth/tenant`
   - Cache the returned `tenant_id`

2. **Update API Calls**:
   - Include cached `tenant_id` in subsequent requests
   - Use appropriate header format (depends on deployment mode)

## Related Documentation

- WorkOS AuthKit Docs: https://workos.com/docs/authkit
- WorkOS User Management API: https://workos.com/docs/user-management/user-management-api
- OIDC Spec: https://openid.net/specs/openid-connect-core-1_0.html
- PKCE RFC: https://datatracker.ietf.org/doc/html/rfc7636

## Files Changed

### Backend
- `internal/httpapi/tenant_resolve.go` - New endpoint implementation
- `internal/httpapi/router.go` - Router configuration
- `cmd/server/main.go` - WorkOS client initialization

### Infrastructure
- `chart/templates/deployment.yaml` - Environment variable injection
- `homelab-k8s/apps/toolbridge-api/production-overlays/toolbridge-secret.sops.yaml` - API key secret

### Tests
- `test/manual/standard_oidc_tenant.go` - Proves standard OIDC doesn't include org_id
- `test/manual/test_full_tenant_flow.go` - Complete flow reference implementation
- `test/manual/test_tenant_resolution.go` - Simple endpoint test

### Client (Future)
- `lib/services/tenant_resolver.dart` - Already updated to call backend API
- Multi-org selection UI - Not yet implemented
