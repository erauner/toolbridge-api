# Protocol Buffer Setup

## Current State

**The gRPC package is currently disabled via build tags** to allow tests to pass without proto generation. This is intentional for Phase 1 PR review.

The `internal/grpcapi` package uses the `//go:build grpc` build tag, which means:
- ✅ Unit tests pass without proto generation
- ✅ HTTP API remains fully functional
- ⚠️ gRPC server won't compile until after proto generation

## Prerequisites

Install protoc and Go plugins:

```bash
# Install protoc (MacOS)
brew install protobuf

# Install Go plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Ensure $GOPATH/bin is in your PATH
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Generate Code

```bash
# From repo root
./scripts/generate_proto.sh
```

This will generate:
- `gen/go/sync/v1/sync.pb.go` - Protocol buffer message definitions
- `gen/go/sync/v1/sync_grpc.pb.go` - gRPC service stubs

## Building with gRPC Support

After proto generation, build with the `grpc` tag:

```bash
# Build with gRPC support
go build -tags grpc ./...

# Run tests with gRPC support
go test -tags grpc ./...

# Run server with gRPC support (after uncommenting in main.go)
go run -tags grpc cmd/server/main.go
```

## What Gets Generated

The generated code includes:
- Message types (ServerInfo, PushRequest, PullResponse, etc.)
- gRPC client and server interfaces
- Type-safe service implementations

These generated files are consumed by:
- `internal/grpcapi/server.go` - gRPC server implementation (build tag: `grpc`)
- `internal/grpcapi/interceptors.go` - Middleware (build tag: `grpc`)

## Why Build Tags?

Build tags allow us to:
1. ✅ **Review the PR** without requiring proto generation
2. ✅ **Pass CI tests** on the Phase 1 foundation
3. ✅ **Keep HTTP API working** independently
4. ⚠️ **Enable gRPC** only after proto generation is complete
