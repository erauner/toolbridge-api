# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build MCP bridge binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/mcpbridge ./cmd/mcpbridge

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/mcpbridge /app/mcpbridge

# Non-root user
RUN addgroup -g 1000 app && \
    adduser -D -u 1000 -G app app && \
    chown -R app:app /app

USER app

# Health check (MCP server runs on port 8082 by default)
# Note: Actual bind address controlled by command-line args or will use default :8082
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8082/healthz || exit 1

# Expose MCP HTTP port
EXPOSE 8082

# Default command (can be overridden with --dev, --debug, etc.)
ENTRYPOINT ["/app/mcpbridge"]
