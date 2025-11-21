#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
GO_API_PORT=8080
MCP_PORT=8001
GO_API_PID=""
MCP_PID=""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  ToolBridge MCP End-to-End Test Suite"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "${YELLOW}Cleaning up...${NC}"

    if [ ! -z "$MCP_PID" ]; then
        echo "Stopping MCP service (PID: $MCP_PID)..."
        kill $MCP_PID 2>/dev/null || true
    fi

    if [ ! -z "$GO_API_PID" ]; then
        echo "Stopping Go API (PID: $GO_API_PID)..."
        kill $GO_API_PID 2>/dev/null || true
    fi

    # Kill any remaining processes on the ports
    lsof -ti:$GO_API_PORT | xargs kill -9 2>/dev/null || true
    lsof -ti:$MCP_PORT | xargs kill -9 2>/dev/null || true

    echo "${GREEN}✓ Cleanup complete${NC}"
}

# Set trap to cleanup on exit
trap cleanup EXIT INT TERM

# Step 1: Start Go API
echo "Step 1: Starting Go REST API..."
cd "$(dirname "$0")/.."

# Check if postgres is running
if ! docker-compose ps postgres | grep -q "Up"; then
    echo "${YELLOW}Starting PostgreSQL...${NC}"
    docker-compose up -d postgres
    sleep 3
fi

# Start Go API in background
DATABASE_URL="postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable" \
JWT_HS256_SECRET="dev-secret" \
ENV=dev \
go run ./cmd/server &
GO_API_PID=$!

# Wait for Go API to be ready
echo "Waiting for Go API to start on port $GO_API_PORT..."
for i in {1..30}; do
    if lsof -ti:$GO_API_PORT >/dev/null 2>&1; then
        echo "${GREEN}✓ Go API started (PID: $GO_API_PID)${NC}"
        break
    fi
    sleep 1
    if [ $i -eq 30 ]; then
        echo "${RED}✗ Go API failed to start${NC}"
        exit 1
    fi
done

# Step 2: Start Python MCP Service
echo ""
echo "Step 2: Starting Python MCP Service..."
cd mcp

# Start MCP service in background
uv run python -m toolbridge_mcp.server > /tmp/mcp.log 2>&1 &
MCP_PID=$!

# Wait for MCP service to be ready
echo "Waiting for MCP service to start on port $MCP_PORT..."
for i in {1..30}; do
    if lsof -ti:$MCP_PORT >/dev/null 2>&1; then
        echo "${GREEN}✓ MCP service started (PID: $MCP_PID)${NC}"
        break
    fi
    sleep 1
    if [ $i -eq 30 ]; then
        echo "${RED}✗ MCP service failed to start${NC}"
        cat /tmp/mcp.log
        exit 1
    fi
done

# Step 3: Run MCP integration tests
echo ""
echo "Step 3: Running MCP integration tests..."
cd ..

# Run Python test script (uses the mcp venv but runs from repo root)
uv run --directory mcp python ../scripts/test-mcp-integration.py

# Check exit code
if [ $? -eq 0 ]; then
    echo ""
    echo "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo "${GREEN}  ✓ All MCP E2E Tests PASSED!${NC}"
    echo "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    exit 0
else
    echo ""
    echo "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo "${RED}  ✗ MCP E2E Tests Failed${NC}"
    echo "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    exit 1
fi
