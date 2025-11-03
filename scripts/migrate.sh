#!/bin/bash
set -e

# Migration script for ToolBridge API
# Applies all pending SQL migrations in order

# Configuration
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-toolbridge}"
DB_NAME="${DB_NAME:-toolbridge}"
DB_PASSWORD="${DB_PASSWORD:-dev-password}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-migrations}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

function log_info() {
    echo -e "${GREEN}✓${NC} $1"
}

function log_warn() {
    echo -e "${YELLOW}▶${NC} $1"
}

function log_error() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

# Check if PostgreSQL is accessible
log_warn "Checking database connection..."
if ! docker exec toolbridge-postgres pg_isready -U "$DB_USER" > /dev/null 2>&1; then
    log_error "Cannot connect to PostgreSQL. Is the container running?"
fi
log_info "Database connection OK"

# Create migrations tracking table if it doesn't exist
log_warn "Ensuring migrations table exists..."
docker exec -i toolbridge-postgres psql -U "$DB_USER" -d "$DB_NAME" <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    id SERIAL PRIMARY KEY,
    migration VARCHAR(255) UNIQUE NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL
log_info "Migrations table ready"

# Get list of applied migrations
applied_migrations=$(docker exec toolbridge-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT migration FROM schema_migrations ORDER BY migration")

# Apply pending migrations
log_warn "Checking for pending migrations..."
pending_count=0

for migration_file in $(ls -1 "$MIGRATIONS_DIR"/*.sql | sort); do
    migration_name=$(basename "$migration_file")

    # Check if migration was already applied
    if echo "$applied_migrations" | grep -q "$migration_name"; then
        log_info "Already applied: $migration_name"
        continue
    fi

    # Apply migration
    log_warn "Applying migration: $migration_name"
    if docker exec -i toolbridge-postgres psql -U "$DB_USER" -d "$DB_NAME" < "$migration_file"; then
        # Record successful migration
        docker exec -i toolbridge-postgres psql -U "$DB_USER" -d "$DB_NAME" <<SQL
INSERT INTO schema_migrations (migration) VALUES ('$migration_name');
SQL
        log_info "Successfully applied: $migration_name"
        ((pending_count++))
    else
        log_error "Failed to apply migration: $migration_name"
    fi
done

if [ $pending_count -eq 0 ]; then
    log_info "No pending migrations - database is up to date!"
else
    log_info "Applied $pending_count migration(s)"
fi

# Show current migration status
log_warn "Current migration status:"
docker exec toolbridge-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT migration, applied_at FROM schema_migrations ORDER BY applied_at"
