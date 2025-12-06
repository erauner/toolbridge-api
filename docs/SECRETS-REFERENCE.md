# ToolBridge Secrets Reference

This document details all secrets and environment variables required for ToolBridge deployments across different environments.

## Overview

ToolBridge uses authentication via:
1. **JWT tokens** for user identity validation (RS256 via OIDC JWKS or HS256 for testing)
2. **WorkOS API** for tenant authorization validation (organization membership checks)

## Token Architecture

ToolBridge handles two distinct types of JWT tokens. Understanding this separation is important for configuration and troubleshooting.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            TOKEN FLOW DIAGRAM                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐     ┌─────────────────┐     ┌──────────────────────────┐  │
│  │   WorkOS     │     │   MCP Server    │     │     ToolBridge API       │  │
│  │   AuthKit    │     │   (Fly.io)      │     │     (Kubernetes)         │  │
│  └──────┬───────┘     └────────┬────────┘     └────────────┬─────────────┘  │
│         │                      │                           │                 │
│         │  1. User Login       │                           │                 │
│         │◄─────────────────────┤                           │                 │
│         │                      │                           │                 │
│         │  2. WorkOS Token     │                           │                 │
│         │  (RS256, WorkOS key) │                           │                 │
│         ├─────────────────────►│                           │                 │
│         │                      │                           │                 │
│         │                      │  3. Token Exchange        │                 │
│         │                      │  POST /auth/token-exchange│                 │
│         │                      ├──────────────────────────►│                 │
│         │                      │                           │                 │
│         │                      │                    ┌──────┴──────┐         │
│         │                      │                    │ Validate    │         │
│         │                      │                    │ WorkOS token│         │
│         │                      │                    │ via JWKS    │         │
│         │                      │                    └──────┬──────┘         │
│         │                      │                           │                 │
│         │                      │  4. Backend Token         │                 │
│         │                      │  (RS256, OUR key)         │                 │
│         │                      │◄──────────────────────────┤                 │
│         │                      │                           │                 │
│         │                      │  5. API calls with        │                 │
│         │                      │  Backend Token            │                 │
│         │                      ├──────────────────────────►│                 │
│         │                      │                           │                 │
│         │                      │                    ┌──────┴──────┐         │
│         │                      │                    │ Validate    │         │
│         │                      │                    │ Backend token│        │
│         │                      │                    │ via OUR key │         │
│         │                      │                    └─────────────┘         │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Token Types

| Token Type | Issuer | Signed With | Validated With | Purpose |
|------------|--------|-------------|----------------|---------|
| **WorkOS Token** | WorkOS AuthKit | WorkOS RSA key | WorkOS JWKS endpoint | Initial user authentication |
| **Backend Token** | `toolbridge-api` | Our RSA key (or HS256) | Our public key | Internal API calls after exchange |

### Key Points

1. **No WorkOS changes needed** for backend token configuration - WorkOS tokens and backend tokens are completely separate
2. **Backend tokens** are issued by `/auth/token-exchange` after validating an incoming WorkOS token
3. **The `kid` header** distinguishes token types during validation:
   - `kid` matching `JWT_BACKEND_KEY_ID` → validate with backend public key
   - Any other `kid` → validate via WorkOS JWKS endpoint
4. **RS256 backend signing is optional** - falls back to HS256 if not configured

### Public Key Distribution

Currently, toolbridge-api operates in a **closed-loop model**: it both signs and validates backend tokens using the same key pair. The public key is derived from the private key at startup via `InitBackendSigner`.

```
┌─────────────────────────────────────────────────────────────────────┐
│                 Current: Closed-Loop Validation                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   toolbridge-api                                                    │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  Private Key (from secret)                                  │   │
│   │       │                                                     │   │
│   │       ├──► Sign backend tokens (/auth/token-exchange)       │   │
│   │       │                                                     │   │
│   │       └──► Derive public key at init                        │   │
│   │                 │                                           │   │
│   │                 └──► Validate backend tokens (API calls)    │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**When would you need to distribute the public key?**

If you add microservices that need to validate backend tokens independently (without calling toolbridge-api), you have two options:

#### Option 1: Manual Public Key Distribution

Extract and distribute the public key to downstream services:

```bash
# Extract public key from private key
openssl rsa -in backend-private.pem -pubout -out backend-public.pem

# Distribute backend-public.pem to services that need to validate tokens
```

Each service loads the public key and validates tokens locally. Simple but requires manual key rotation coordination.

#### Option 2: Expose a Backend JWKS Endpoint (Future)

Similar to how WorkOS exposes `/.well-known/jwks.json`, toolbridge-api could expose its own JWKS endpoint for backend tokens:

```
GET /.well-known/backend-jwks.json

{
  "keys": [
    {
      "kty": "RSA",
      "kid": "toolbridge-backend-1",
      "use": "sig",
      "alg": "RS256",
      "n": "<base64url-encoded-modulus>",
      "e": "AQAB"
    }
  ]
}
```

**Benefits:**
- Standard OIDC/OAuth2 pattern (same as WorkOS, Auth0, Okta, etc.)
- Automatic key rotation support (multiple keys in JWKS)
- Services can cache and refresh keys automatically
- No manual public key distribution needed

**Implementation notes (not yet implemented):**
- Add `GET /.well-known/backend-jwks.json` endpoint
- Convert `backendSigner.PublicKey` to JWK format
- Consider caching headers (`Cache-Control: max-age=3600`)
- Document the endpoint for downstream service integration

For now, the closed-loop model is sufficient since only toolbridge-api validates backend tokens.

## Kubernetes Deployment (Go API + PostgreSQL)

### Required Secrets

Location: `homelab-k8s/apps/toolbridge-api/production-overlays/toolbridge-secret.sops.yaml`

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: toolbridge-secret
  namespace: toolbridge
type: Opaque
stringData:
  # PostgreSQL configuration
  username: toolbridge
  password: <generated-postgres-password>
  database-url: postgres://toolbridge:<password>@toolbridge-api-postgres-rw.toolbridge.svc.cluster.local:5432/toolbridge?sslmode=require

  # JWT configuration
  # - HS256: required for legacy/backend tokens and dev/test (defense-in-depth)
  # - RS256: optional for backend tokens (recommended for multi-service deployments)
  jwt-secret: <generated-hs256-secret>  # Required (defense-in-depth & legacy support)

  # Optional: RS256 backend token signing (recommended for production multi-service deployments)
  # When configured, /auth/token-exchange issues RS256 tokens with kid=jwt-backend-key-id
  # Validators use the embedded public key (no external JWKS required for backend tokens)
  jwt-backend-rs256-private-key: |-
    -----BEGIN PRIVATE KEY-----
    ...
    -----END PRIVATE KEY-----
  jwt-backend-key-id: "toolbridge-backend-1"

  # WorkOS API key for tenant authorization (multi-tenant mode)
  workos-api-key: <your-workos-api-key>  # Optional - required for B2B tenant validation
```

### Generating Secrets

```bash
# PostgreSQL password
openssl rand -base64 32

# JWT HS256 secret (required for defense-in-depth & legacy support)
openssl rand -base64 32

# RS256 backend key pair (optional - recommended for production multi-service)
# Generate a 2048-bit RSA key pair:
openssl genrsa -out backend-private.pem 2048
openssl rsa -in backend-private.pem -pubout -out backend-public.pem
# Use backend-private.pem content for jwt-backend-rs256-private-key
# Distribute backend-public.pem to downstream services for token validation
```

### Managing K8s Secrets

```bash
# Decrypt and edit with SOPS
cd homelab-k8s/apps/toolbridge-api/production-overlays
sops toolbridge-secret.sops.yaml

# After editing, commit (file stays encrypted)
git add toolbridge-secret.sops.yaml
git commit -m "chore: update secrets"
git push

# Retrieve secret value
kubectl get secret toolbridge-secret -n toolbridge \
  -o jsonpath='{.data.workos-api-key}' | base64 -d
```

### Helm Values

Location: `homelab-k8s/apps/toolbridge-api/helm-values-production.yaml`

Non-secret configuration:

```yaml
api:
  env: production
  jwt:
    # OIDC RS256 JWT validation - compatible with any OIDC provider
    # Examples: WorkOS AuthKit, Okta, Keycloak, Auth0
    issuer: "https://svelte-monolith-27-staging.authkit.app"
    jwksUrl: "https://svelte-monolith-27-staging.authkit.app/oauth2/jwks"
    audience: "https://toolbridgeapi.erauner.dev"
    tenantClaim: "organization_id"  # Claim key for tenant extraction

secrets:
  existingSecret: "toolbridge-secret"  # References SOPS secret above
```

## Fly.io Deployment (MCP Proxy Only)

### Required Secrets

Set via `fly secrets set -a <app-name>`:

```bash
# External Go API endpoint (K8s ingress)
TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"

# Optional: Logging level
TOOLBRIDGE_LOG_LEVEL="INFO"  # DEBUG, INFO, WARNING, ERROR
```

### Setting Secrets

```bash
# Initial setup
fly secrets set -a toolbridge-mcp-staging \
  TOOLBRIDGE_GO_API_BASE_URL="https://toolbridgeapi.erauner.dev"

# Update a single secret
fly secrets set TOOLBRIDGE_LOG_LEVEL="DEBUG" -a toolbridge-mcp-staging

# List configured secrets (values hidden)
fly secrets list -a toolbridge-mcp-staging

# Unset a secret
fly secrets unset TOOLBRIDGE_LOG_LEVEL -a toolbridge-mcp-staging
```

## Local Development

### Go API (.env)

Location: `toolbridge-api/.env`

```bash
# Database
DATABASE_URL=postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable

# JWT authentication
JWT_HS256_SECRET=dev-secret-change-in-production
ENV=dev  # Enables X-Debug-Sub header bypass

# HTTP server
HTTP_ADDR=:8080

# Optional: WorkOS API key for tenant validation (multi-tenant mode)
# WORKOS_API_KEY=sk_test_...

# Default tenant ID for B2C users
DEFAULT_TENANT_ID=tenant_thinkpen_b2c

# Optional: RS256 backend token signing
# If set, JWT_BACKEND_KEY_ID must also be provided
# JWT_BACKEND_RS256_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
# JWT_BACKEND_KEY_ID="toolbridge-backend-1"
```

### Python MCP Service (mcp/.env)

Location: `toolbridge-api/mcp/.env`

```bash
# Go API connection
TOOLBRIDGE_GO_API_BASE_URL=http://localhost:8080

# Logging
TOOLBRIDGE_LOG_LEVEL=DEBUG

# Server config (default values)
TOOLBRIDGE_HOST=0.0.0.0
TOOLBRIDGE_PORT=8001
```

## Secret Rotation

### When to Rotate

- **JWT HS256 secret:** Every 90 days (if using HS256 for backend tokens)
- **JWT RS256 backend key:** Every 180 days (if using RS256 for backend tokens)
- **PostgreSQL password:** Every 180 days or on suspected compromise
- **WorkOS API key:** On suspected compromise (rotate via WorkOS dashboard)

### Rotation Process

#### 1. Rotate PostgreSQL Password

**Note:** Requires brief downtime or connection pool drain.

```bash
# Step 1: Generate new password
NEW_PG_PASSWORD=$(openssl rand -base64 32)

# Step 2: Update PostgreSQL user password
kubectl exec -it -n toolbridge toolbridge-api-postgres-1 -- psql -U postgres
# In psql:
ALTER USER toolbridge PASSWORD 'new-password-here';
\q

# Step 3: Update K8s secret
sops toolbridge-secret.sops.yaml
# Update both 'password' and 'database-url', commit, push

# Step 4: Restart Go API pods to pick up new connection string
kubectl rollout restart deployment/toolbridge-api -n toolbridge
```

#### 2. Rotate JWT HS256 Secret (if using)

**Note:** Invalidates all existing HS256 tokens. Users must re-authenticate.

```bash
# Step 1: Generate new secret
NEW_JWT_SECRET=$(openssl rand -base64 32)

# Step 2: Update K8s secret
sops toolbridge-secret.sops.yaml
# Update jwt-secret, commit, push

# Step 3: Wait for deployment
kubectl rollout status deployment/toolbridge-api -n toolbridge

# Step 4: Notify users to re-authenticate (all HS256 tokens now invalid)
```

#### 3. Rotate JWT RS256 Backend Key (if using)

**Note:** Invalidates all existing RS256 backend tokens. MCP clients must re-exchange tokens.

```bash
# Step 1: Generate new RSA key pair
openssl genrsa -out backend-private-new.pem 2048
openssl rsa -in backend-private-new.pem -pubout -out backend-public-new.pem

# Step 2: Update K8s secret with new key and NEW key ID (to distinguish from old tokens)
sops toolbridge-secret.sops.yaml
# Update jwt-backend-rs256-private-key with content of backend-private-new.pem
# Update jwt-backend-key-id to a new value (e.g., "toolbridge-backend-2")
# Commit, push

# Step 3: Wait for deployment
kubectl rollout status deployment/toolbridge-api -n toolbridge

# Step 4: Distribute new public key to downstream services (if any)
# MCP clients will automatically get new tokens via token exchange
```

#### 4. Rotate WorkOS API Key

```bash
# Step 1: Generate new API key in WorkOS dashboard
# https://dashboard.workos.com/api-keys

# Step 2: Update K8s secret
sops toolbridge-secret.sops.yaml
# Update workos-api-key, commit, push

# Step 3: Wait for deployment
kubectl rollout status deployment/toolbridge-api -n toolbridge
```

## Secret Validation

### Test K8s → Go API

```bash
# Test with debug header (dev only)
curl -H "X-Debug-Sub: test-user" https://toolbridgeapi.erauner.dev/healthz

# Test with JWT token
TOKEN=$(python -c "import jwt; print(jwt.encode({'sub': 'test-user'}, 'your-jwt-secret', algorithm='HS256'))")
curl -H "Authorization: Bearer $TOKEN" https://toolbridgeapi.erauner.dev/v1/notes
```

### Test Tenant Resolution

```bash
# After OIDC authentication, call tenant resolution endpoint
curl -H "Authorization: Bearer $ID_TOKEN" \
     https://toolbridgeapi.erauner.dev/v1/auth/tenant

# Expected response (B2C user):
# {"tenant_id": "tenant_thinkpen_b2c", "organization_name": "ThinkPen", "requires_selection": false}

# Expected response (B2B user):
# {"tenant_id": "org_01ABC...", "organization_name": "Acme Corp", "requires_selection": false}
```

## Security Best Practices

### Secret Storage

- ✅ **K8s:** Use SOPS with age encryption
- ✅ **Fly.io:** Use `fly secrets` (encrypted at rest)
- ✅ **Local:** Use `.env` files (never commit to git)
- ❌ **Never:** Store secrets in code, config files, or CI logs

### Secret Sharing

- ✅ Use password managers (1Password, Bitwarden) for team sharing
- ✅ Use encrypted channels (Signal, encrypted email) for one-time sharing
- ❌ Never share via Slack, email, or text messages

### Secret Access Control

- **K8s secrets:** Only cluster admins and ArgoCD
- **Fly.io secrets:** Only app deployers
- **SOPS age keys:** Only platform team members

### Monitoring

Set up alerts for:
- Failed JWT validation attempts (potential token theft)
- Unusual tenant access patterns
- Expired/expiring secrets

## Troubleshooting

### "JWT validation failed" Error

```bash
# Check JWT secret is correct
kubectl get secret toolbridge-secret -n toolbridge \
  -o jsonpath='{.data.jwt-secret}' | base64 -d

# For OIDC tokens, verify issuer and JWKS URL in helm values
```

### "Not authorized for requested tenant" Error

```bash
# Verify user's organization membership via WorkOS dashboard
# Check that WORKOS_API_KEY is configured correctly
kubectl get secret toolbridge-secret -n toolbridge \
  -o jsonpath='{.data.workos-api-key}' | base64 -d
```

### "Connection refused" to Go API

```bash
# Verify TOOLBRIDGE_GO_API_BASE_URL is correct
fly secrets list -a toolbridge-mcp-staging

# Test Go API directly
curl https://toolbridgeapi.erauner.dev/healthz
```

## References

- **SOPS Documentation:** https://github.com/mozilla/sops
- **Fly.io Secrets:** https://fly.io/docs/reference/secrets/
- **JWT Best Practices:** https://tools.ietf.org/html/rfc8725
- **OWASP Secret Management:** https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html
- **WorkOS API Keys:** https://workos.com/docs/api-keys
