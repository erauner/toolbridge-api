# ToolBridge Secrets Reference

This document details all secrets and environment variables required for ToolBridge deployments across different environments.

## Overview

ToolBridge uses authentication via:
1. **JWT tokens** for user identity validation (RS256 via OIDC JWKS or HS256 for testing)
2. **WorkOS API** for tenant authorization validation (organization membership checks)

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

  # JWT configuration (HS256 for backend tokens, OIDC RS256 for production)
  jwt-secret: <generated-hs256-secret>  # Required for backend token signing

  # WorkOS API key for tenant authorization (multi-tenant mode)
  workos-api-key: <your-workos-api-key>  # Optional - required for B2B tenant validation
```

### Generating Secrets

```bash
# PostgreSQL password
openssl rand -base64 32

# JWT HS256 secret (required for backend token signing)
openssl rand -base64 32
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

- **JWT HS256 secret:** Every 90 days (if using HS256)
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

**Note:** Invalidates all existing tokens. Users must re-authenticate.

```bash
# Step 1: Generate new secret
NEW_JWT_SECRET=$(openssl rand -base64 32)

# Step 2: Update K8s secret
sops toolbridge-secret.sops.yaml
# Update jwt-secret, commit, push

# Step 3: Wait for deployment
kubectl rollout status deployment/toolbridge-api -n toolbridge

# Step 4: Notify users to re-authenticate (all tokens now invalid)
```

#### 3. Rotate WorkOS API Key

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
