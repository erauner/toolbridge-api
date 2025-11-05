.PHONY: help dev test test-unit test-integration test-smoke test-all test-e2e ci build docker-build docker-up docker-down clean

# Default target
help:
	@echo "ToolBridge API - Development Commands"
	@echo ""
	@echo "Development:"
	@echo "  make dev              - Start local dev server"
	@echo "  make build            - Build binary"
	@echo ""
	@echo "Testing:"
	@echo "  make test             - Run all tests (unit + integration)"
	@echo "  make test-unit        - Run unit tests only (fast, no DB)"
	@echo "  make test-integration - Run integration tests (requires DB)"
	@echo "  make test-smoke       - Run smoke tests against running server"
	@echo "  make test-e2e         - Run end-to-end tests (starts server, runs smoke, stops)"
	@echo "  make test-all         - Run complete test suite (unit + integration + e2e)"
	@echo "  make ci               - Run CI pipeline locally (lint + test-all)"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build     - Build Docker image"
	@echo "  make docker-up        - Start Postgres via docker-compose"
	@echo "  make docker-down      - Stop Postgres"
	@echo ""
	@echo "Database:"
	@echo "  make migrate          - Run migrations against local DB"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean            - Clean build artifacts"

# Local development server
dev:
	@echo "Starting dev server..."
	DATABASE_URL=postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable \
	JWT_HS256_SECRET=dev-secret \
	ENV=dev \
	go run ./cmd/server

# Run all tests (unit + integration)
test:
	@echo "Running all tests..."
	TEST_DATABASE_URL=postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable \
	go test -v -race -cover ./...

# Run unit tests only (no database required)
test-unit:
	@echo "Running unit tests..."
	go test -v -short -race -cover ./...

# Run integration tests (requires database)
test-integration:
	@echo "Running integration tests..."
	TEST_DATABASE_URL=postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable \
	go test -v -race -cover ./internal/httpapi/...

# Run smoke tests against running server
test-smoke:
	@echo "Running smoke tests..."
	@./scripts/smoke-test.sh

# Run end-to-end tests (full stack: start server, run smoke tests, stop server)
test-e2e:
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "Running End-to-End Tests"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "Step 1: Starting Postgres..."
	@$(MAKE) docker-up
	@echo ""
	@echo "Step 2: Running migrations..."
	@./scripts/migrate.sh
	@echo ""
	@echo "Step 3: Starting API server in background..."
	@DATABASE_URL=postgres://toolbridge:dev-password@localhost:5432/toolbridge?sslmode=disable \
	 JWT_HS256_SECRET=dev-secret \
	 ENV=dev \
	 go run ./cmd/server > /tmp/toolbridge-api-e2e.log 2>&1 & \
	 echo $$! > /tmp/toolbridge-api-e2e.pid
	@echo "Waiting for server to start..."
	@sleep 3
	@echo ""
	@echo "Step 4: Running smoke tests..."
	@./scripts/smoke-test.sh || (kill `cat /tmp/toolbridge-api-e2e.pid` 2>/dev/null; rm -f /tmp/toolbridge-api-e2e.pid; exit 1)
	@echo ""
	@echo "Step 5: Stopping API server..."
	@kill `cat /tmp/toolbridge-api-e2e.pid` 2>/dev/null || true
	@rm -f /tmp/toolbridge-api-e2e.pid
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "✓ End-to-End Tests Passed"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Run complete test suite (unit + integration + e2e)
test-all:
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "Running Complete Test Suite"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "Phase 1: Unit Tests (no DB required)"
	@echo "────────────────────────────────────────────"
	@$(MAKE) test-unit
	@echo ""
	@echo "Phase 2: Integration Tests (requires DB)"
	@echo "────────────────────────────────────────────"
	@$(MAKE) test-integration
	@echo ""
	@echo "Phase 3: End-to-End Tests (full stack)"
	@echo "────────────────────────────────────────────"
	@$(MAKE) test-e2e
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "✓ All Tests Passed!"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Run CI pipeline locally (lint + format check + test-all)
ci:
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "Running CI Pipeline Locally"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "Step 1: Code Quality Checks"
	@echo "────────────────────────────────────────────"
	@echo "Running go vet..."
	@go vet ./...
	@echo "✓ go vet passed"
	@echo ""
	@echo "Running go fmt check..."
	@test -z "$$(gofmt -l .)" || (echo "Code not formatted. Run: go fmt ./..." && exit 1)
	@echo "✓ go fmt check passed"
	@echo ""
	@echo "Step 2: Complete Test Suite"
	@echo "────────────────────────────────────────────"
	@$(MAKE) test-all
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "✓ CI Pipeline Passed!"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Build binary
build:
	@echo "Building server..."
	CGO_ENABLED=0 go build -o bin/server ./cmd/server

# Build Docker image
docker-build:
	docker build -t toolbridge-api:latest .

# Start Postgres with docker-compose
docker-up:
	docker-compose up -d postgres
	@echo "Waiting for Postgres to be ready..."
	@sleep 3
	@echo "Postgres is ready at localhost:5432"

# Stop docker-compose services
docker-down:
	docker-compose down

# Run migrations manually (requires psql)
migrate:
	@echo "Running migrations..."
	PGPASSWORD=dev-password psql -h localhost -U toolbridge -d toolbridge -f migrations/0001_init.sql

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Install dependencies
deps:
	go mod download
	go mod tidy
