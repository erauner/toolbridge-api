# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies (wget for healthcheck, bash for scripts)
RUN apk add --no-cache ca-certificates tzdata wget bash postgresql-client

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server /app/server

# Copy migrations and scripts for migration runner
COPY migrations /app/migrations
COPY scripts /app/scripts

# Make scripts executable
RUN chmod +x /app/scripts/*.sh

# Non-root user
RUN addgroup -g 1000 app && \
    adduser -D -u 1000 -G app app && \
    chown -R app:app /app

USER app

# Health check
# Note: Server binds to HTTP_ADDR (default :8081), but we set it to :8080 in deployment configs
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

# Expose standard HTTP port
# Note: Actual bind address is controlled by HTTP_ADDR environment variable
EXPOSE 8080

ENTRYPOINT ["/app/server"]
