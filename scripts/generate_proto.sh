#!/bin/bash
set -e

# Generate Go code from proto files
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc

PROTO_DIR="proto"
OUT_DIR="gen/go"

echo "Generating Go protobuf code..."

# Create output directory
mkdir -p "$OUT_DIR"

# Generate code
protoc \
  --go_out="$OUT_DIR" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$OUT_DIR" \
  --go-grpc_opt=paths=source_relative \
  -I="$PROTO_DIR" \
  "$PROTO_DIR/sync/v1/sync.proto"

echo "✅ Protobuf generation complete"
echo "Generated files in: $OUT_DIR/sync/v1/"
