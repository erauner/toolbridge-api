# gRPC-Web Experimental Implementation

> **⚠️ EXPERIMENTAL BRANCH - NOT FOR PRODUCTION**
> This branch contains server-side gRPC-Web implementation that is NOT merged to main.
> Use REST API for production. This is preserved for future reference.

## Branch Purpose

This branch (`feat/grpc-web-experimental`) contains the server-side gRPC-Web implementation for ToolBridge API. It wraps the existing gRPC server to support gRPC-Web clients (Flutter, web browsers).

## Why Not in Main?

After thorough analysis, we determined:
- **ToolBridge's sync workload is periodic CRUD**, not real-time streaming
- **REST API is more reliable** for mobile + background sync
- **gRPC-Web adds complexity** without meaningful benefits for our use case
- **Cloudflare Tunnel limitation**: Native gRPC not supported, only gRPC-Web

**Decision**: Keep REST as production transport, park gRPC-Web implementation for future.

## What's Implemented

### Server Changes:

**Files Modified:**
- `cmd/server/grpc_setup.go` - Added gRPC-Web wrapper
- `cmd/server/grpc_noop.go` - Added stub for non-gRPC builds
- `cmd/server/main.go` - Route gRPC-Web requests through HTTP server
- `go.mod` / `go.sum` - Added `improbable-eng/grpc-web` dependency

**Implementation:**
```go
// Wrap existing gRPC server
grpcWebWrapper = grpcweb.WrapServer(
    grpcServerInstance,
    grpcweb.WithOriginFunc(func(origin string) bool {
        return true // CORS: allow all origins
    }),
)

// Route requests: gRPC-Web → wrapper, others → REST
httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if ServeGRPCWeb(w, r) {
        return // Handled as gRPC-Web
    }
    restHandler.ServeHTTP(w, r) // Fall back to REST
})
```

## How It Works

1. **Client sends gRPC-Web request** to `https://toolbridge-grpc.erauner.dev:443`
2. **Cloudflare Tunnel** forwards to LoadBalancer `http://10.10.0.1:80`
3. **Envoy Gateway** routes to toolbridge-api pod on port `8081`
4. **HTTP server** detects gRPC-Web headers (`Content-Type: application/grpc-web+proto`)
5. **grpcweb.WrapServer** translates gRPC-Web → native gRPC
6. **gRPC server** processes request normally
7. **Response flows back**: gRPC → gRPC-Web → HTTP → Cloudflare → Client

## Building & Running

```bash
# Build with gRPC tag to enable gRPC-Web
go build -tags grpc -o bin/toolbridge-api cmd/server/main.go

# Run (with gRPC-Web enabled)
DATABASE_URL=postgres://... \
ENV=dev \
./bin/toolbridge-api

# Or run with go run
go run -tags grpc cmd/server/main.go
```

**Without `-tags grpc`**: Builds REST-only server (production default)

## Testing

```bash
# Test gRPC-Web endpoint with curl
curl -X POST http://localhost:8081/toolbridge.sync.v1.SyncService/GetServerInfo \
  -H "Content-Type: application/grpc-web+proto" \
  -H "X-Grpc-Web: 1" \
  -H "X-Debug-Sub: test-user" \
  --data-binary "" \
  -v

# Expected: HTTP 200 with binary gRPC-Web response
```

## Client-Side

Flutter client code exists in main ToolBridge repo with:
- ✅ gRPC-Web channel implementation (`GrpcWebClientChannel.xhr()`)
- ✅ UI warnings marking it as experimental
- ✅ Confirmation dialog before enabling
- ✅ Defaults to REST (recommended)

**Note**: Selecting gRPC in the app won't work until this server code is deployed.

## When to Deploy This

Consider deploying if you need:
- ✅ Real-time streaming features (>1 update/sec sustained)
- ✅ Server-side streaming for live updates
- ✅ Low-latency requirements (<200ms for updates)

**Don't deploy if:**
- ❌ Just doing periodic sync (use REST)
- ❌ Mobile-first app with background sync (REST is better)
- ❌ No specific streaming requirements

## Deployment Steps (If Needed)

1. **Merge this branch to main** (or deploy from branch)
2. **Build Docker image** with `-tags grpc`:
   ```dockerfile
   RUN go build -tags grpc -o /app/server cmd/server/main.go
   ```
3. **Update Kubernetes deployment** (no changes needed, same port 8081)
4. **Verify**: Check logs for "gRPC-Web servers running in parallel"
5. **Test**: Use Flutter app or curl to test gRPC-Web endpoint

## Infrastructure Requirements

**Already configured (no changes needed):**
- ✅ Cloudflare Tunnel catch-all HTTP rule
- ✅ Envoy Gateway HTTP listener (port 80 → 8081)
- ✅ GRPCRoute pointing to port 8081

## Comparison: REST vs gRPC-Web

| Feature | REST | gRPC-Web | Winner |
|---------|------|----------|--------|
| Periodic Sync | ✅ Perfect | ✅ Works | REST (simpler) |
| Real-time Streaming | ❌ Polling only | ✅ Server-streaming | gRPC-Web |
| Mobile Battery | ✅ Efficient | ⚠️ Depends | REST (for periodic) |
| Background Mode | ✅ Perfect | ⚠️ Problematic | REST |
| Debugging | ✅ Easy (JSON logs) | ❌ Hard (binary) | REST |
| Caching | ✅ Native HTTP | ⚠️ Manual | REST |
| Latency (unary) | ~5-10ms | ~5-10ms | Tie |

**Verdict**: For ToolBridge's current workload, REST wins.

## References

- [Transport Selection Guide](../ToolBridge/docs/TRANSPORT_SELECTION_GUIDE.md)
- [gRPC-Web Experimental Status](../ToolBridge/docs/GRPC_WEB_EXPERIMENTAL_STATUS.md)
- [improbable-eng/grpc-web](https://github.com/improbable-eng/grpc-web)
- [Cloudflare gRPC Limitations](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/use-cases/grpc/)

## Maintenance

This branch should be kept in sync with main periodically to avoid drift. Rebase or merge main into this branch to keep it current.

```bash
# Update this branch with latest main
git checkout feat/grpc-web-experimental
git rebase main
# or
git merge main
```

## Summary

- ✅ **Code complete** and functional
- ⚠️ **Not deployed** to production
- ⚠️ **Not recommended** for current use case
- 📦 **Preserved** for future streaming features
- 🎯 **Use REST** for production sync

---

*Branch created: 2025-01-09*
*Status: Experimental - Parked for Future Use*
