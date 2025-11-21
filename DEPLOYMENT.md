# ToolBridge API Deployment Guide

## Production Transport: REST API Only

**ToolBridge API is deployed with REST API only.** gRPC is NOT used in production.

### Production Endpoint
- **Base URL**: `https://toolbridgeapi.erauner.dev/v1`
- **Protocol**: REST (HTTP/JSON)
- **Port**: 8080 (HTTP service)

### Build Command (Production)
```bash
# Standard build - REST API only (no gRPC)
go build -o bin/toolbridge-api cmd/server/main.go
```

**Do NOT use `-tags grpc` in production builds.**

## Why REST Only?

After thorough evaluation of gRPC for ToolBridge:
- ✅ ToolBridge sync is periodic CRUD operations (not real-time streaming)
- ✅ REST is more reliable for mobile + background sync
- ✅ REST has better debugging, caching, and observability
- ✅ Cloudflare Tunnel limitation: native gRPC not supported for public hostnames

**Decision**: REST API is the production standard.

## gRPC Experimental Branch

gRPC-Web server implementation exists in `feat/grpc-web-experimental` branch:
- **Status**: Experimental, NOT deployed
- **Purpose**: Preserved for future if streaming features are needed
- **Client code**: Exists in ToolBridge Flutter app with experimental warnings
- **Server endpoint**: Does NOT exist in production

See branch [feat/grpc-web-experimental](https://github.com/erauner12/toolbridge-api/tree/feat/grpc-web-experimental) and `GRPC_WEB_EXPERIMENTAL.md` for details.

## Deployment Configuration

### Docker Build
```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY . .
# Build WITHOUT gRPC tags (REST only)
RUN go build -o server cmd/server/main.go

FROM alpine:latest
COPY --from=builder /build/server /app/server
CMD ["/app/server"]
```

### Kubernetes Deployment

**Helm Values** (`apps/toolbridge-api/helm-values-production.yaml`):
```yaml
# gRPC configuration
grpc:
  enabled: false  # gRPC is DISABLED
```

**HTTPRoute** (`apps/toolbridge-api/production-overlays/http-route.yaml`):
```yaml
kind: HTTPRoute  # Using HTTPRoute, not GRPCRoute
metadata:
  name: toolbridge-api
spec:
  hostnames:
    - "toolbridgeapi.erauner.dev"
  rules:
    - backendRefs:
        - name: toolbridge-api
          port: 8080  # REST API port
```

### Environment Variables
```bash
# Required
DATABASE_URL=postgres://user:pass@host/db
ENV=production  # Disables X-Debug-Sub header
JWT_HS256_SECRET=<strong-secret>  # Required in production (even with upstream OIDC)

# Upstream OIDC RS256 JWT validation (recommended for production)
# Supports WorkOS AuthKit, Auth0, Okta, or any OIDC provider
JWT_ISSUER=https://your-app.authkit.app  # Or: https://your-tenant.auth0.com
JWT_JWKS_URL=https://your-app.authkit.app/oauth2/jwks  # Or: https://your-tenant.auth0.com/.well-known/jwks.json
JWT_AUDIENCE=https://toolbridgeapi.erauner.dev  # Optional

# Optional
HTTP_ADDR=:8080  # Default REST API port
```

**Security Note**: `JWT_HS256_SECRET` is required in production. The server will refuse to start if:
- `ENV != dev` (production mode)
- AND `JWT_HS256_SECRET` is unset or set to the default value

Generate a strong secret: `openssl rand -base64 32`

**Do NOT set `GRPC_ADDR` - it's not used in production.**

## API Endpoints

### REST API (Production)
```
Base: https://toolbridgeapi.erauner.dev/v1

# Sync endpoints
POST /sync/notes/push
GET  /sync/notes/pull
POST /sync/tasks/push
GET  /sync/tasks/pull
POST /sync/comments/push
GET  /sync/comments/pull
POST /sync/chats/push
GET  /sync/chats/pull
POST /sync/chat-messages/push
GET  /sync/chat-messages/pull

# Session management
POST /sync/session/begin
POST /sync/session/end

# Info & state
GET  /sync/info
GET  /sync/state
POST /sync/wipe
```

## Client Configuration

### Flutter App (Production)
```dart
// lib/config/api_config.dart or settings
const API_URL = 'https://toolbridgeapi.erauner.dev/v1';
const TRANSPORT = SyncTransport.rest;  // Default and recommended
```

**Do NOT use gRPC transport** unless explicitly testing experimental features.

## Monitoring

### Health Check
```bash
curl https://toolbridgeapi.erauner.dev/v1/sync/info
```

Expected response:
```json
{
  "version": "1.0.0",
  "environment": "production"
}
```

### Logs
```bash
kubectl logs -n toolbridge -l app=toolbridge-api --tail=100 -f
```

Look for:
- ✅ `"starting HTTP server" addr=":8080"`
- ❌ NO `"starting gRPC server"` messages

## Troubleshooting

### If gRPC Server Starts in Production
This indicates the binary was built with `-tags grpc` by mistake.

**Fix**:
1. Rebuild without gRPC tags: `go build -o bin/toolbridge-api cmd/server/main.go`
2. Rebuild Docker image
3. Redeploy

### If Clients Can't Connect
1. Verify DNS: `nslookup toolbridgeapi.erauner.dev`
2. Check HTTPRoute status: `kubectl get httproute -n toolbridge`
3. Verify pods running: `kubectl get pods -n toolbridge`
4. Check logs for errors

## Summary

- ✅ **Production**: REST API only on `https://toolbridgeapi.erauner.dev/v1`
- ✅ **Build**: Standard Go build (no `-tags grpc`)
- ✅ **Deploy**: `grpc.enabled: false` in Helm values
- ✅ **Client**: Use `SyncTransport.rest`
- ⚠️ **gRPC**: Experimental branch only, NOT deployed

---

*Last Updated: 2025-01-09*
*Production Standard: REST API*
