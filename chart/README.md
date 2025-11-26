# ToolBridge API Helm Chart

A Helm chart for deploying ToolBridge API - a cross-device MCP server bridge.

## Prerequisites

- Kubernetes 1.21+
- Helm 3.8+
- CloudNativePG Operator (for PostgreSQL management)
- cert-manager (for TLS certificates)
- Gateway API (for HTTPRoute ingress)

## Installation

### Using OCI Registry

```bash
# Install with default values
helm install toolbridge-api oci://ghcr.io/erauner12/charts/toolbridge-api \
  --version 0.1.0 \
  --namespace toolbridge \
  --create-namespace

# Install with custom values
helm install toolbridge-api oci://ghcr.io/erauner12/charts/toolbridge-api \
  --version 0.1.0 \
  --namespace toolbridge \
  --create-namespace \
  --values values-production.yaml
```

### From Source

```bash
# Install from local chart
helm install toolbridge-api ./chart \
  --namespace toolbridge \
  --create-namespace \
  --values values-production.yaml
```

## Configuration

### Required Values

When not using `secrets.existingSecret`, you must provide:

```yaml
secrets:
  jwtSecret: "your-jwt-secret-here"
  dbUsername: "toolbridge"
  dbPassword: "your-secure-password"
  databaseUrl: "postgres://toolbridge:password@host:5432/toolbridge?sslmode=require"

ingress:
  hostname: "toolbridge-api.example.com"
```

### Using Existing Secrets

To use externally managed secrets (SOPS, Sealed Secrets, External Secrets Operator):

```yaml
secrets:
  existingSecret: "toolbridge-secret"
```

The existing secret must contain these keys:
- `jwt-secret`: JWT signing secret
- `username`: Database username
- `password`: Database password
- `database-url`: Complete PostgreSQL connection URL

### Example Production Values

```yaml
# values-production.yaml
image:
  tag: "v0.1.0"

replicaCount: 2

postgresql:
  enabled: true
  instances: 2
  size: 20Gi
  storageClass: longhorn

ingress:
  enabled: true
  hostname: "toolbridge-api.example.com"

certificate:
  enabled: true
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer

secrets:
  existingSecret: "toolbridge-secret"  # Managed via SOPS/Sealed Secrets

api:
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi
```

## OAuth Client Configuration

### Overview

ToolBridge API supports multiple OAuth clients for different use cases:
- **User Client**: Flutter/web apps for end-user interactions
- **MCP Client**: Machine-to-machine integrations (AI agents, automation)

The chart provides an `oauthClients` registry to clearly define all clients that can authenticate with your API.

### Why Use the OAuth Client Registry?

The registry approach provides several benefits:
1. **Clarity**: Explicitly documents which clients exist and their purposes
2. **Validation**: Helm validates configuration at deployment time
3. **Visibility**: Client IDs appear in ConfigMap annotations for easy reference
4. **Flexibility**: Supports any OIDC provider (WorkOS AuthKit, Okta, Keycloak, etc.)

### Basic Configuration

```yaml
oauthClients:
  # User-facing client (Flutter app, web app, mobile apps)
  userClient:
    clientId: "client_01KAPCBQNQBWMZE9WNSEWY2J3Z"
    name: "User Applications"
    description: "Flutter, web, and mobile apps used by end users"

  # Machine-to-machine client (MCP server)
  mcpClient:
    clientId: "client_01KABXHNQ09QGWEX4APPYG2AH5"
    name: "MCP Server"
    description: "FastMCP server for AI agent integrations (Claude Code, etc.)"
    enabled: true  # Set to true when MCP integration is active

api:
  jwt:
    issuer: "https://your-app.authkit.app"
    jwksUrl: "https://your-app.authkit.app/oauth2/jwks"
    # audience: ""  # Leave empty to use userClient.clientId
    tenantClaim: "organization_id"

  mcp:
    enabled: true
    # oauthAudience: ""  # Leave empty to use mcpClient.clientId
```

### How Token Validation Works

1. **User Client** (Flutter/Web):
   - Token audience (`aud` claim) must match `oauthClients.userClient.clientId`
   - Used for end-user authentication flows
   - Validated via `JWT_AUDIENCE` environment variable

2. **MCP Client** (Machine-to-machine):
   - Token audience must match `oauthClients.mcpClient.clientId`
   - Used for server-side automation and AI agents
   - Validated via `MCP_OAUTH_AUDIENCE` environment variable

3. **Backend Validation**:
   - The Go API accepts tokens from ANY registered client
   - Each client's `clientId` becomes an accepted audience
   - Tokens are validated using JWKS from your OIDC provider

### OIDC Provider Compatibility

The chart works with any OIDC-compliant provider:

**WorkOS AuthKit**:
```yaml
api:
  jwt:
    issuer: "https://your-app.authkit.app"
    jwksUrl: "https://your-app.authkit.app/oauth2/jwks"
    tenantClaim: "organization_id"
```

**Okta**:
```yaml
api:
  jwt:
    issuer: "https://your-tenant.okta.com"
    jwksUrl: "https://your-tenant.okta.com/oauth2/v1/keys"
    tenantClaim: "org_id"
```

**Keycloak**:
```yaml
api:
  jwt:
    issuer: "https://keycloak.example.com/realms/your-realm"
    jwksUrl: "https://keycloak.example.com/realms/your-realm/protocol/openid-connect/certs"
    tenantClaim: "organization_id"
```

### Legacy Configuration (Backward Compatible)

For backward compatibility, you can still set audiences directly:

```yaml
api:
  jwt:
    audience: "client_01KAPCBQNQBWMZE9WNSEWY2J3Z"  # Override userClient

  mcp:
    oauthAudience: "https://toolbridge-mcp.fly.dev/mcp"  # Override mcpClient
```

However, the **recommended approach** is to use the `oauthClients` registry and leave the direct audience fields empty.

### Migration from Legacy Configuration

If you currently have:

```yaml
# OLD (legacy approach)
api:
  jwt:
    audience: "client_01KAPCBQNQBWMZE9WNSEWY2J3Z"
  mcp:
    oauthAudience: "client_01KABXHNQ09QGWEX4APPYG2AH5"
```

Migrate to:

```yaml
# NEW (recommended approach)
oauthClients:
  userClient:
    clientId: "client_01KAPCBQNQBWMZE9WNSEWY2J3Z"
    name: "User Applications"
  mcpClient:
    clientId: "client_01KABXHNQ09QGWEX4APPYG2AH5"
    name: "MCP Server"
    enabled: true

api:
  jwt:
    # audience: ""  # Remove or leave empty
  mcp:
    # oauthAudience: ""  # Remove or leave empty
```

### Configuration Validation

The chart validates your OAuth configuration at deployment time:

- **When `jwt.issuer` is set**: You MUST provide either `api.jwt.audience` OR `oauthClients.userClient.clientId`
- **When MCP is enabled with `mcpClient.enabled=true`**: You MUST provide `oauthClients.mcpClient.clientId`

Invalid configurations will fail during `helm install` or `helm upgrade` with a helpful error message.

### Checking Deployed Configuration

After deployment, you can verify which clients are registered:

```bash
# View ConfigMap annotations showing registered clients
kubectl get configmap -n toolbridge toolbridge-api-config -o jsonpath='{.metadata.annotations}'

# Example output:
{
  "toolbridge.dev/oauth-user-client": "client_01KAPCBQNQBWMZE9WNSEWY2J3Z",
  "toolbridge.dev/oauth-user-client-name": "User Applications",
  "toolbridge.dev/oauth-mcp-client": "client_01KABXHNQ09QGWEX4APPYG2AH5",
  "toolbridge.dev/oauth-mcp-client-name": "MCP Server"
}

# View environment variables
kubectl get configmap -n toolbridge toolbridge-api-config -o yaml | grep -E "(JWT_AUDIENCE|MCP_OAUTH_AUDIENCE)"
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `image.repository` | string | `ghcr.io/erauner12/toolbridge-api` | Image repository |
| `image.tag` | string | `v0.1.0` | Image tag |
| `replicaCount` | int | `2` | Number of API replicas |
| `postgresql.enabled` | bool | `true` | Enable PostgreSQL deployment |
| `postgresql.instances` | int | `2` | Number of PostgreSQL instances |
| `postgresql.size` | string | `10Gi` | PostgreSQL storage size |
| `postgresql.storageClass` | string | `longhorn` | Storage class for PostgreSQL |
| `ingress.enabled` | bool | `true` | Enable HTTPRoute ingress |
| `ingress.hostname` | string | `""` | Hostname for ingress (required) |
| `certificate.enabled` | bool | `true` | Enable cert-manager certificate |
| `secrets.existingSecret` | string | `""` | Use existing secret instead of creating |

See [values.yaml](./values.yaml) for all available options.

## Secrets Management

### Option 1: SOPS (Recommended for GitOps)

```bash
# Create encrypted secret
sops --encrypt secret.yaml > secret.sops.yaml

# Use with Helm via KSOPS
helm secrets install toolbridge-api ./chart \
  --namespace toolbridge \
  --values values.yaml \
  --values secret.sops.yaml
```

### Option 2: Sealed Secrets

```bash
# Create sealed secret
kubeseal --format yaml < secret.yaml > sealed-secret.yaml

# Apply sealed secret
kubectl apply -f sealed-secret.yaml

# Install chart referencing sealed secret
helm install toolbridge-api ./chart \
  --set secrets.existingSecret=toolbridge-secret
```

### Option 3: External Secrets Operator

```yaml
# externalsecret.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: toolbridge-secret
spec:
  secretStoreRef:
    name: vault
    kind: SecretStore
  target:
    name: toolbridge-secret
  data:
    - secretKey: jwt-secret
      remoteRef:
        key: toolbridge/jwt-secret
    - secretKey: password
      remoteRef:
        key: toolbridge/db-password
```

## Upgrading

```bash
# Upgrade to new version
helm upgrade toolbridge-api oci://ghcr.io/erauner12/charts/toolbridge-api \
  --version 0.2.0 \
  --namespace toolbridge \
  --values values-production.yaml
```

## Uninstalling

```bash
helm uninstall toolbridge-api --namespace toolbridge
```

Note: This will delete the application but preserve the PostgreSQL cluster and PVCs. To fully remove:

```bash
kubectl delete cluster toolbridge-api-postgres -n toolbridge
kubectl delete pvc -l app.kubernetes.io/instance=toolbridge-api -n toolbridge
```

## Development

### Testing Locally

```bash
# Lint chart
helm lint ./chart

# Template chart (dry-run)
helm template toolbridge-api ./chart \
  --values chart/values.yaml \
  --set secrets.jwtSecret=test \
  --set secrets.dbPassword=test \
  --set secrets.databaseUrl=test \
  --set ingress.hostname=test.local

# Install in test cluster
helm install toolbridge-api ./chart \
  --namespace toolbridge-test \
  --create-namespace \
  --values test-values.yaml
```

### Packaging

```bash
# Package chart
helm package ./chart

# Push to OCI registry
helm push toolbridge-api-0.1.0.tgz oci://ghcr.io/erauner12/charts
```

## Troubleshooting

### Migration Job Fails

Check the migration job logs:
```bash
kubectl logs -n toolbridge job/toolbridge-api-migrate
```

Common issues:
- Database not ready: Wait for PostgreSQL cluster to be fully initialized
- Wrong credentials: Verify secret contains correct `password` and `database-url`
- Connection refused: Check `config.dbHost` points to correct service

### API Pods Not Starting

Check init container logs:
```bash
kubectl logs -n toolbridge <pod-name> -c wait-for-migrations
```

The API pods wait for the migration job to complete before starting.

### HTTPRoute Not Working

Verify Gateway and HTTPRoute:
```bash
kubectl get gateway -n network
kubectl get httproute -n toolbridge
kubectl describe httproute toolbridge-api -n toolbridge
```

## License

See [LICENSE](../LICENSE) file in repository root.
