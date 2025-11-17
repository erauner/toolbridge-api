# MCP Bridge Production Deployment Guide

This guide covers deploying the MCP bridge to production using Kubernetes and Helm.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Auth0 Configuration](#auth0-configuration)
3. [Helm Deployment](#helm-deployment)
4. [Configuration Reference](#configuration-reference)
5. [Verification](#verification)
6. [Troubleshooting](#troubleshooting)

## Prerequisites

Before deploying the MCP bridge, ensure you have:

- Kubernetes cluster with Gateway API support (for HTTPRoute)
- Helm 3.x installed
- `kubectl` configured to access your cluster
- Cert-manager installed (for TLS certificates)
- ToolBridge REST API deployed and accessible
- Auth0 tenant configured

## Auth0 Configuration

### 1. Create Auth0 Application

1. **Log in to Auth0 Dashboard**
   - Navigate to Applications → Applications
   - Click "Create Application"

2. **Configure Application Settings**
   - **Name**: ToolBridge MCP Bridge
   - **Type**: Single Page Application (for web clients) or Native (for CLI/desktop)
   - Click "Create"

3. **Note Application Details**
   - Copy the **Domain** (e.g., `your-tenant.us.auth0.com`)
   - Copy the **Client ID**
   - You may need to create multiple applications for different client types (web, native, macOS)

4. **Configure Application URIs** (for web/native clients)
   - **Allowed Callback URLs**: Add your callback URLs
   - **Allowed Logout URLs**: Add your logout URLs
   - **Allowed Web Origins**: Add your application origins
   - **Allowed Origins (CORS)**: Add your application origins

### 2. Create or Configure Auth0 API (Sync API)

1. **Navigate to Applications → APIs**
   - If you already have a ToolBridge API configured, use that
   - Otherwise, click "Create API"

2. **API Configuration**
   - **Name**: ToolBridge Sync API
   - **Identifier**: `https://api.toolbridge.yourdomain.com` (this is your audience)
   - **Signing Algorithm**: RS256
   - Click "Create"

3. **Note the API Identifier**
   - This will be used as `AUTH0_SYNC_API_AUDIENCE`

### 3. Gather Configuration Values

You'll need these values for Helm deployment:

| Value | Example | Where to Find |
|-------|---------|---------------|
| `AUTH0_DOMAIN` | `your-tenant.us.auth0.com` | Auth0 Dashboard → Applications → Settings |
| `AUTH0_CLIENT_ID_WEB` | `abc123xyz...` | Web application Client ID |
| `AUTH0_CLIENT_ID_NATIVE` | `def456uvw...` | Native application Client ID (optional) |
| `AUTH0_CLIENT_ID_NATIVE_MACOS` | `ghi789rst...` | macOS application Client ID (optional) |
| `AUTH0_SYNC_API_AUDIENCE` | `https://api.toolbridge.yourdomain.com` | API Identifier |

## Helm Deployment

### 1. Build and Push Docker Image

```bash
# Build and push multi-architecture image
make docker-release-mcp VERSION=v0.1.0

# Or build for local platform only
make docker-build-mcp-local
```

### 2. Create Values Override File

Create a file `values-production.yaml`:

```yaml
# Image configuration
image:
  repository: ghcr.io/yourusername/toolbridge-mcpbridge
  tag: "v0.1.0"

# MCP bridge configuration
mcpbridge:
  # API Base URL (internal Kubernetes service or external URL)
  apiBaseUrl: "http://toolbridge-api.toolbridge.svc.cluster.local"

  # Production mode (Auth0 enabled)
  devMode: false

  # Log level
  logLevel: "info"

  # Allowed origins for CORS/DNS rebinding protection
  # IMPORTANT: Configure this for production!
  allowedOrigins: "https://app.toolbridge.com,https://web.toolbridge.com"

  # Auth0 configuration
  auth0:
    domain: "your-tenant.us.auth0.com"
    syncApiAudience: "https://api.toolbridge.yourdomain.com"

# Service configuration
service:
  type: ClusterIP
  port: 80

# Ingress configuration
ingress:
  enabled: true
  gateway:
    name: envoy-public
    namespace: network
  hostname: "mcp.toolbridge.com"

# Certificate configuration
certificate:
  enabled: true
  issuerRef:
    name: letsencrypt-cloudflare-prod
    kind: ClusterIssuer
  dnsNames:
    - mcp.toolbridge.com

# Secrets (use external secret management in production)
secrets:
  # Client IDs can be public but often managed as secrets
  auth0ClientIdNative: "your-native-client-id"
  auth0ClientIdWeb: "your-web-client-id"
  auth0SyncApiAudience: "https://api.toolbridge.yourdomain.com"
```

### 3. Deploy with Helm

```bash
# Create namespace
kubectl create namespace toolbridge-mcp

# Install chart
helm install toolbridge-mcpbridge ./chart-mcpbridge \
  --namespace toolbridge-mcp \
  --values values-production.yaml

# Or upgrade if already installed
helm upgrade toolbridge-mcpbridge ./chart-mcpbridge \
  --namespace toolbridge-mcp \
  --values values-production.yaml
```

### 4. Using External Secrets (Recommended)

For production, use Kubernetes Secret management (Sealed Secrets, External Secrets Operator, SOPS, etc.):

```yaml
# values-production.yaml
secrets:
  # Reference existing secret instead of inline values
  existingSecret: "toolbridge-mcp-secrets"
```

Then create the secret separately:

```bash
# Example using kubectl (replace with your secret management solution)
kubectl create secret generic toolbridge-mcp-secrets \
  --namespace toolbridge-mcp \
  --from-literal=auth0-client-id-native=your-native-client-id \
  --from-literal=auth0-client-id-web=your-web-client-id \
  --from-literal=auth0-sync-api-audience=https://api.toolbridge.yourdomain.com
```

## Configuration Reference

### Environment Variables

The MCP bridge container recognizes these environment variables:

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `MCP_API_BASE_URL` | ToolBridge REST API base URL | Yes | - |
| `MCP_DEV_MODE` | Enable dev mode (bypass Auth0) | No | `false` |
| `MCP_DEBUG` | Enable debug logging | No | `false` |
| `MCP_LOG_LEVEL` | Log level (debug, info, warn, error) | No | `info` |
| `MCP_ALLOWED_ORIGINS` | Comma-separated allowed origins | Prod only | - |
| `AUTH0_DOMAIN` | Auth0 tenant domain | Prod only | - |
| `AUTH0_SYNC_API_AUDIENCE` | Sync API audience | Prod only | - |
| `AUTH0_CLIENT_ID_NATIVE` | Native app client ID | Optional | - |
| `AUTH0_CLIENT_ID_WEB` | Web app client ID | Optional | - |
| `AUTH0_CLIENT_ID_NATIVE_MACOS` | macOS app client ID | Optional | - |

### Helm Values

Key values in `values.yaml`:

```yaml
mcpbridge:
  apiBaseUrl: ""           # Required: ToolBridge API URL
  devMode: false           # Set true for development
  debug: false             # Enable debug logs
  logLevel: "info"         # Log level
  allowedOrigins: ""       # Required for production

  auth0:
    domain: ""             # Required for production
    syncApiAudience: ""    # Required for production

  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

### Health Probes

The chart configures readiness and liveness probes:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8082
  initialDelaySeconds: 10
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 8082
  initialDelaySeconds: 5
  periodSeconds: 5
```

- `/healthz`: Returns 200 if process is alive (no dependencies checked)
- `/readyz`: Returns 200 if server is ready (checks JWT validator readiness)

## Verification

### 1. Check Pod Status

```bash
kubectl get pods -n toolbridge-mcp
```

Expected output:
```
NAME                                      READY   STATUS    RESTARTS   AGE
toolbridge-mcpbridge-xxx-yyy              1/1     Running   0          2m
```

### 2. Check Health Endpoints

```bash
# Port-forward to pod
kubectl port-forward -n toolbridge-mcp deployment/toolbridge-mcpbridge 8082:8082

# Test health endpoint
curl http://localhost:8082/healthz
# Expected: {"status":"ok"}

# Test readiness endpoint
curl http://localhost:8082/readyz
# Expected: {"status":"ready","jwtValidator":"ready"}
```

### 3. Check Logs

```bash
kubectl logs -n toolbridge-mcp deployment/toolbridge-mcpbridge --follow
```

Look for:
- `"message":"Starting MCP Bridge Server"`
- `"message":"JWT validator warmed up successfully"`
- `"message":"Starting MCP server"`

### 4. Test MCP Endpoints

```bash
# Test initialize endpoint (requires valid JWT)
curl -X POST https://mcp.toolbridge.com/mcp \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -H "Mcp-Protocol-Version: 2025-03-26" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
```

### 5. Test OAuth Discovery

```bash
curl https://mcp.toolbridge.com/.well-known/oauth-authorization-server
```

Should return OAuth metadata including authorization and token endpoints.

## Troubleshooting

### Pod Not Ready

**Symptom**: Pod shows `0/1 READY` or readiness probe fails

**Diagnosis**:
```bash
kubectl logs -n toolbridge-mcp deployment/toolbridge-mcpbridge
kubectl describe pod -n toolbridge-mcp <pod-name>
```

**Common causes**:
1. JWT validator not ready (JWKS fetch failed)
   - Check Auth0 domain is correct
   - Verify network connectivity to Auth0
   - Check logs for JWKS fetch errors

2. Missing configuration
   - Verify ConfigMap and Secret are created
   - Check environment variables are set correctly

**Solution**:
```bash
# Check ConfigMap
kubectl get configmap -n toolbridge-mcp toolbridge-mcpbridge-config -o yaml

# Check Secret
kubectl get secret -n toolbridge-mcp toolbridge-mcpbridge-secret -o yaml

# Restart deployment
kubectl rollout restart deployment/toolbridge-mcpbridge -n toolbridge-mcp
```

### JWT Validation Fails

**Symptom**: Requests return 401 or "invalid token"

**Diagnosis**:
```bash
kubectl logs -n toolbridge-mcp deployment/toolbridge-mcpbridge | grep "JWT validation failed"
```

**Common causes**:
1. Incorrect Auth0 domain or audience
2. Token expired
3. Token signed with wrong key

**Solution**:
- Verify Auth0 domain matches token issuer
- Verify audience matches sync API audience
- Check token expiration (`exp` claim)
- Obtain fresh token from Auth0

### Origin Validation Fails

**Symptom**: Requests return 403 "origin not allowed"

**Diagnosis**:
Check logs for origin validation messages

**Solution**:
Update `allowedOrigins` in values file:
```yaml
mcpbridge:
  allowedOrigins: "https://app.toolbridge.com,https://web.toolbridge.com"
```

Then upgrade deployment:
```bash
helm upgrade toolbridge-mcpbridge ./chart-mcpbridge \
  --namespace toolbridge-mcp \
  --values values-production.yaml
```

### JWKS Fetch Fails

**Symptom**: Logs show "Failed to warm up JWT validator"

**Common causes**:
1. Network connectivity to Auth0
2. Firewall blocking HTTPS
3. Invalid Auth0 domain

**Solution**:
```bash
# Test connectivity from pod
kubectl exec -n toolbridge-mcp deployment/toolbridge-mcpbridge -- \
  wget -O- https://your-tenant.us.auth0.com/.well-known/jwks.json

# Check network policies
kubectl get networkpolicies -n toolbridge-mcp
```

### High Memory Usage

**Symptom**: Pod OOMKilled or high memory consumption

**Solution**:
Increase resource limits:
```yaml
mcpbridge:
  resources:
    limits:
      memory: 1Gi
```

### Certificate Issues

**Symptom**: HTTPRoute not working, TLS errors

**Diagnosis**:
```bash
kubectl get certificate -n toolbridge-mcp
kubectl describe certificate -n toolbridge-mcp toolbridge-mcpbridge-tls
```

**Solution**:
- Verify cert-manager is installed
- Check DNS is configured correctly
- Verify issuer (ClusterIssuer) exists

## Production Checklist

Before going to production:

- [ ] Auth0 tenant configured with production domain
- [ ] Client IDs created for all required platforms
- [ ] Sync API audience configured
- [ ] TLS certificate issued and valid
- [ ] DNS pointing to ingress
- [ ] Allowed origins configured (not empty!)
- [ ] Resource limits set appropriately
- [ ] Monitoring and alerting configured
- [ ] Backup and disaster recovery plan in place
- [ ] Secrets managed externally (not in values file)
- [ ] Network policies configured
- [ ] Pod security policies applied

## Additional Resources

- [MCP Bridge README](../README.md)
- [MCP Specification](https://spec.modelcontextprotocol.io/)
- [Auth0 Documentation](https://auth0.com/docs)
- [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/)
- [Helm Documentation](https://helm.sh/docs/)
