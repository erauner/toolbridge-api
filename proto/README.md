# Protocol Buffer Setup

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

## What Gets Generated

The generated code includes:
- Message types (ServerInfo, PushRequest, PullResponse, etc.)
- gRPC client and server interfaces
- Type-safe service implementations

These generated files are consumed by:
- `internal/grpcapi/server.go` - gRPC server implementation
- `internal/grpcapi/interceptors.go` - Middleware (auth, sessions, etc.)
