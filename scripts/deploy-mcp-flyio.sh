#!/bin/bash
# ToolBridge MCP Fly.io Deployment Helper Script
# This script guides you through deploying the MCP-only service to Fly.io

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
FLY_APP_NAME="toolbridge-mcp-staging"
FLY_REGION="ord"
K8S_NAMESPACE="toolbridge"
K8S_SECRET_NAME="toolbridge-secret"
GO_API_URL="https://toolbridgeapi.erauner.dev"
TENANT_ID="staging-tenant-001"

echo -e "${BLUE}================================================${NC}"
echo -e "${BLUE}ToolBridge MCP Fly.io Deployment${NC}"
echo -e "${BLUE}================================================${NC}"
echo ""

# Step 1: Verify prerequisites
echo -e "${YELLOW}Step 1: Verifying prerequisites...${NC}"

# Check kubectl
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl not found. Please install kubectl first.${NC}"
    exit 1
fi
echo -e "${GREEN}✓ kubectl found${NC}"

# Check fly CLI
if ! command -v fly &> /dev/null; then
    echo -e "${RED}❌ fly CLI not found. Install with: brew install flyctl${NC}"
    exit 1
fi
echo -e "${GREEN}✓ fly CLI found${NC}"

# Check Go API health
echo -e "Checking Go API health..."
if curl -sf "${GO_API_URL}/healthz" > /dev/null; then
    echo -e "${GREEN}✓ Go API is healthy${NC}"
else
    echo -e "${RED}❌ Go API is not accessible at ${GO_API_URL}${NC}"
    exit 1
fi

# Step 2: Check K8s secrets
echo ""
echo -e "${YELLOW}Step 2: Checking K8s secret...${NC}"

if kubectl get secret ${K8S_SECRET_NAME} -n ${K8S_NAMESPACE} &> /dev/null; then
    echo -e "${GREEN}✓ K8s secret exists${NC}"
else
    echo -e "${RED}❌ K8s secret not found. Please ensure K8s deployment is complete.${NC}"
    exit 1
fi

# Step 3: Test Docker build locally
echo ""
echo -e "${YELLOW}Step 3: Testing Docker build locally...${NC}"
read -p "Do you want to test the Docker build locally first? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Building Docker image..."
    docker build -f Dockerfile.mcp-only -t toolbridge-mcp:local .
    
    echo ""
    echo "Running container locally..."
    echo -e "${BLUE}Starting container on port 8001...${NC}"
    docker run --rm -d --name toolbridge-mcp-test \
        -p 8001:8001 \
        -e TOOLBRIDGE_GO_API_BASE_URL="${GO_API_URL}" \
        -e TOOLBRIDGE_LOG_LEVEL="DEBUG" \
        toolbridge-mcp:local
    
    echo "Waiting for container to start..."
    sleep 5
    
    echo "Testing health endpoint..."
    if curl -sf http://localhost:8001/ > /dev/null; then
        echo -e "${GREEN}✓ Local container is healthy${NC}"
    else
        echo -e "${YELLOW}⚠ Container started but health check failed (this may be expected)${NC}"
    fi
    
    echo ""
    echo "Container logs:"
    docker logs toolbridge-mcp-test
    
    echo ""
    read -p "Stop local container? (Y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        docker stop toolbridge-mcp-test
        echo -e "${GREEN}✓ Local container stopped${NC}"
    fi
fi

# Step 4: Create Fly.io app
echo ""
echo -e "${YELLOW}Step 4: Creating Fly.io app...${NC}"

if fly apps list | grep -q "${FLY_APP_NAME}"; then
    echo -e "${GREEN}✓ Fly.io app already exists: ${FLY_APP_NAME}${NC}"
else
    echo "Creating Fly.io app: ${FLY_APP_NAME}"
    fly apps create ${FLY_APP_NAME} --org personal
    echo -e "${GREEN}✓ App created${NC}"
fi

# Step 5: Set Fly.io secrets
echo ""
echo -e "${YELLOW}Step 5: Configuring Fly.io secrets...${NC}"

echo "Setting secrets..."
fly secrets set \
    TOOLBRIDGE_GO_API_BASE_URL="${GO_API_URL}" \
    TOOLBRIDGE_LOG_LEVEL="INFO" \
    -a ${FLY_APP_NAME}

echo -e "${GREEN}✓ Secrets configured${NC}"

# Step 6: Deploy to Fly.io
echo ""
echo -e "${YELLOW}Step 6: Deploying to Fly.io...${NC}"
read -p "Deploy to Fly.io now? (Y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    fly deploy --config fly.staging.toml -a ${FLY_APP_NAME}
    echo -e "${GREEN}✓ Deployment complete${NC}"
else
    echo -e "${YELLOW}Skipping deployment. To deploy later, run:${NC}"
    echo -e "${BLUE}fly deploy --config fly.staging.toml -a ${FLY_APP_NAME}${NC}"
    exit 0
fi

# Step 7: Verify deployment
echo ""
echo -e "${YELLOW}Step 7: Verifying deployment...${NC}"

echo "Waiting for deployment to stabilize..."
sleep 10

echo "Checking app status..."
fly status -a ${FLY_APP_NAME}

echo ""
echo "Recent logs:"
fly logs -a ${FLY_APP_NAME} | tail -20

# Step 8: Run integration tests
echo ""
echo -e "${YELLOW}Step 8: Running integration tests...${NC}"
read -p "Run integration tests? (Y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    # Get the Fly.io app URL
    FLY_APP_URL="https://${FLY_APP_NAME}.fly.dev"
    
    echo "Running integration tests..."
    cd "$(dirname "$0")/.."

    export MCP_BASE_URL="${FLY_APP_URL}"
    export GO_API_BASE_URL="${GO_API_URL}"
    export TENANT_ID="${TENANT_ID}"

    # Try to load JWT_SECRET from .env, otherwise fetch from K8s
    if [ -f ".env" ]; then
        echo "Loading JWT_SECRET from .env..."
        # Use safer parsing that handles special characters
        export JWT_SECRET=$(grep "^JWT_SECRET=" .env | cut -d= -f2-)
    else
        echo "Fetching JWT_SECRET from K8s..."
        export JWT_SECRET=$(kubectl get secret toolbridge-secret -n toolbridge -o jsonpath='{.data.jwt-secret}' 2>/dev/null | base64 -d)
    fi

    if [ -z "$JWT_SECRET" ]; then
        echo -e "${YELLOW}⚠ JWT_SECRET not found. Integration tests will likely fail.${NC}"
        echo "Create .env file or ensure kubectl access to K8s cluster."
    fi

    if [ -f "scripts/test-mcp-staging.py" ]; then
        python3 scripts/test-mcp-staging.py
    else
        echo -e "${YELLOW}⚠ scripts/test-mcp-staging.py not found${NC}"
    fi
fi

# Summary
echo ""
echo -e "${BLUE}================================================${NC}"
echo -e "${GREEN}✓ Deployment Complete!${NC}"
echo -e "${BLUE}================================================${NC}"
echo ""
echo "App URL: https://${FLY_APP_NAME}.fly.dev"
echo ""
echo "Useful commands:"
echo -e "  ${BLUE}fly status -a ${FLY_APP_NAME}${NC}          - Check status"
echo -e "  ${BLUE}fly logs -a ${FLY_APP_NAME}${NC}            - View logs"
echo -e "  ${BLUE}fly ssh console -a ${FLY_APP_NAME}${NC}     - SSH into container"
echo -e "  ${BLUE}fly dashboard metrics -a ${FLY_APP_NAME}${NC} - View metrics"
echo ""
echo "Next steps:"
echo "1. Test with MCP Inspector:"
echo -e "   ${BLUE}npx @modelcontextprotocol/inspector https://${FLY_APP_NAME}.fly.dev${NC}"
echo ""
echo "2. Configure Claude Desktop to use this MCP server"
echo ""
