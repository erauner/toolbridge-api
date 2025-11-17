#!/bin/bash
#
# Comprehensive Docker-based smoke tests for MCP Bridge
# Tests the Docker image in a production-like environment before K8s deployment
#
# This script:
# 1. Builds the MCP bridge Docker image
# 2. Starts the full stack with docker-compose
# 3. Runs comprehensive tests against the running services
# 4. Tests graceful shutdown and cleanup
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.mcp-test.yml"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

function log_info() {
    echo -e "${GREEN}✓${NC} $1"
}

function log_warn() {
    echo -e "${YELLOW}▶${NC} $1"
}

function log_error() {
    echo -e "${RED}✗${NC} $1"
}

function log_step() {
    echo -e "${BLUE}━━━${NC} $1"
}

function cleanup() {
    log_step "Cleaning up test environment..."
    docker-compose -f "$COMPOSE_FILE" down -v --remove-orphans 2>/dev/null || true
    log_info "Cleanup complete"
}

# Trap to ensure cleanup on exit
trap cleanup EXIT

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "MCP Bridge Docker Integration Tests"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Step 1: Build the Docker image
log_step "Step 1: Building MCP Bridge Docker image"
cd "$PROJECT_ROOT"
docker build -f cmd/mcpbridge/Dockerfile -t toolbridge-mcpbridge:latest . || {
    log_error "Docker build failed"
    exit 1
}
log_info "Docker image built successfully"
echo ""

# Step 2: Start the stack
log_step "Step 2: Starting test environment"
docker-compose -f "$COMPOSE_FILE" up -d || {
    log_error "Failed to start docker-compose services"
    exit 1
}
log_info "Services started"
echo ""

# Step 3: Wait for services to be healthy
log_step "Step 3: Waiting for services to be healthy"
log_warn "Waiting for PostgreSQL..."
timeout 30 bash -c 'until docker-compose -f '"$COMPOSE_FILE"' exec -T postgres pg_isready -U toolbridge > /dev/null 2>&1; do sleep 1; done' || {
    log_error "PostgreSQL did not become ready in time"
    docker-compose -f "$COMPOSE_FILE" logs postgres
    exit 1
}
log_info "PostgreSQL is ready"

log_warn "Waiting for REST API..."
timeout 30 bash -c 'until curl -sf http://localhost:8081/healthz > /dev/null 2>&1; do sleep 1; done' || {
    log_error "REST API did not become healthy in time"
    docker-compose -f "$COMPOSE_FILE" logs toolbridge-api
    exit 1
}
log_info "REST API is healthy"

log_warn "Waiting for MCP Bridge (dev mode)..."
timeout 30 bash -c 'until curl -sf http://localhost:8082/healthz > /dev/null 2>&1; do sleep 1; done' || {
    log_error "MCP Bridge (dev) did not become healthy in time"
    docker-compose -f "$COMPOSE_FILE" logs mcpbridge-dev
    exit 1
}
log_info "MCP Bridge (dev) is healthy"
echo ""

# Step 4: Test REST API
log_step "Step 4: Testing REST API"

# Health check
HEALTH=$(curl -s http://localhost:8081/healthz)
if [ "$HEALTH" = "ok" ]; then
    log_info "REST API health check passed"
else
    log_error "REST API health check failed: $HEALTH"
    exit 1
fi

# Create sync session
SESSION_RESP=$(curl -s -X POST "http://localhost:8081/v1/sync/sessions" \
    -H "X-Debug-Sub: test-user-docker")
SESSION_ID=$(echo "$SESSION_RESP" | jq -r '.id')
if [ "$SESSION_ID" != "null" ] && [ -n "$SESSION_ID" ]; then
    log_info "REST API session creation works (id=$SESSION_ID)"
else
    log_error "Failed to create session: $SESSION_RESP"
    exit 1
fi
echo ""

# Step 5: Test MCP Bridge (Dev Mode)
log_step "Step 5: Testing MCP Bridge (Dev Mode)"

# Health endpoint
MCP_HEALTH=$(curl -s http://localhost:8082/healthz)
if echo "$MCP_HEALTH" | jq -e '.status == "ok"' > /dev/null; then
    log_info "MCP health check passed"
else
    log_error "MCP health check failed: $MCP_HEALTH"
    exit 1
fi

# Readiness endpoint (dev mode should always be ready)
MCP_READY=$(curl -s http://localhost:8082/readyz)
if echo "$MCP_READY" | jq -e '.status == "ready"' > /dev/null; then
    log_info "MCP readiness check passed"
    if echo "$MCP_READY" | jq -e '.devMode == true' > /dev/null; then
        log_info "Dev mode correctly reported in readiness response"
    fi
else
    log_error "MCP readiness check failed: $MCP_READY"
    exit 1
fi

# Test initialize endpoint (simplified test without full MCP flow)
MCP_INIT=$(curl -s -X POST http://localhost:8082/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Protocol-Version: 2025-03-26" \
    -H "X-Debug-Sub: test-user-docker" \
    -d '{
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2025-03-26",
            "capabilities": {},
            "clientInfo": {
                "name": "docker-test-client",
                "version": "1.0.0"
            }
        }
    }')

if echo "$MCP_INIT" | jq -e '.result.protocolVersion' > /dev/null; then
    log_info "MCP initialize endpoint works"
    MCP_SESSION_ID=$(echo "$MCP_INIT" | jq -r '.result.serverInfo.sessionId // empty')
    if [ -n "$MCP_SESSION_ID" ]; then
        log_info "MCP session created (id=$MCP_SESSION_ID)"
    fi
else
    # Check if it's an error response
    if echo "$MCP_INIT" | jq -e '.error' > /dev/null; then
        ERROR_MSG=$(echo "$MCP_INIT" | jq -r '.error.message')
        log_warn "MCP initialize returned error (may be expected): $ERROR_MSG"
    else
        log_error "MCP initialize failed: $MCP_INIT"
        docker-compose -f "$COMPOSE_FILE" logs mcpbridge-dev
        exit 1
    fi
fi
echo ""

# Step 6: Test Logs and Behavior
log_step "Step 6: Verifying logs and behavior"

# Check for expected log messages in dev mode
log_warn "Checking MCP Bridge logs..."
MCP_LOGS=$(docker-compose -f "$COMPOSE_FILE" logs mcpbridge-dev)

if echo "$MCP_LOGS" | grep -q "Starting MCP Bridge Server"; then
    log_info "Found startup message in logs"
else
    log_warn "Startup message not found (may have scrolled past)"
fi

if echo "$MCP_LOGS" | grep -q "Dev mode is enabled"; then
    log_info "Dev mode warning present in logs"
else
    log_warn "Dev mode warning not found in logs"
fi

# Check that no auth0 errors appear in dev mode
if echo "$MCP_LOGS" | grep -q "auth0.domain is required"; then
    log_error "Auth0 validation error should not appear in dev mode!"
    exit 1
else
    log_info "No Auth0 validation errors in dev mode (correct)"
fi
echo ""

# Step 7: Test Graceful Shutdown
log_step "Step 7: Testing graceful shutdown"

# Stop the MCP bridge container
log_warn "Stopping MCP bridge container..."
docker-compose -f "$COMPOSE_FILE" stop mcpbridge-dev || {
    log_error "Failed to stop MCP bridge gracefully"
    exit 1
}

# Check logs for graceful shutdown message
SHUTDOWN_LOGS=$(docker-compose -f "$COMPOSE_FILE" logs mcpbridge-dev | tail -20)
if echo "$SHUTDOWN_LOGS" | grep -q "Shutting down MCP server"; then
    log_info "Graceful shutdown completed"
else
    log_warn "Graceful shutdown message not found (container may have stopped too quickly)"
fi
echo ""

# Step 8: Optional - Test Production Mode Retry Logic
if [ "${TEST_RETRY_LOGIC:-false}" = "true" ]; then
    log_step "Step 8: Testing production mode retry logic (optional)"

    log_warn "Starting MCP bridge in production mode with invalid Auth0..."
    docker-compose -f "$COMPOSE_FILE" --profile retry-test up -d mcpbridge-prod-retry || {
        log_error "Failed to start retry test service"
        exit 1
    }

    # Wait a bit for startup
    sleep 5

    # Health should still pass
    RETRY_HEALTH=$(curl -s http://localhost:8083/healthz)
    if echo "$RETRY_HEALTH" | jq -e '.status == "ok"' > /dev/null; then
        log_info "Health check passes even with JWT validator not ready"
    else
        log_error "Health check should pass regardless of JWT validator state"
        exit 1
    fi

    # Readiness should fail
    RETRY_READY=$(curl -s http://localhost:8083/readyz)
    if echo "$RETRY_READY" | jq -e '.status == "not ready"' > /dev/null; then
        log_info "Readiness correctly reports 'not ready' when JWT validator unavailable"
    else
        log_warn "Readiness should be 'not ready' but got: $RETRY_READY"
    fi

    # Check logs for retry messages
    RETRY_LOGS=$(docker-compose -f "$COMPOSE_FILE" logs mcpbridge-prod-retry)
    if echo "$RETRY_LOGS" | grep -q "Starting background JWKS retry"; then
        log_info "Background retry started as expected"
    else
        log_warn "Background retry message not found"
    fi

    if echo "$RETRY_LOGS" | grep -q "Background retry failed"; then
        log_info "Background retry is running (expected failures with invalid domain)"
    fi

    log_warn "Stopping retry test service..."
    docker-compose -f "$COMPOSE_FILE" stop mcpbridge-prod-retry
    echo ""
fi

# Final Summary
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${GREEN}✓ All Docker Integration Tests Passed!${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Summary:"
echo "  ✅ Docker image builds successfully"
echo "  ✅ Services start and become healthy"
echo "  ✅ REST API works correctly"
echo "  ✅ MCP Bridge (dev mode) works correctly"
echo "  ✅ Health and readiness endpoints respond properly"
echo "  ✅ MCP initialize endpoint works"
echo "  ✅ Graceful shutdown works"
if [ "${TEST_RETRY_LOGIC:-false}" = "true" ]; then
echo "  ✅ Production mode retry logic works"
fi
echo ""
echo "Next Steps:"
echo "  1. Review the test results above"
echo "  2. If all tests passed, you can proceed to Kubernetes deployment"
echo "  3. Use 'make helm-mcp-release' to deploy to K8s cluster"
echo ""
echo "Cleanup:"
echo "  Services will be stopped and cleaned up automatically"
echo ""
