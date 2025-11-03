#!/bin/sh
set -e

# Migration script for ToolBridge API
# Applies all pending SQL migrations in order
# Works both locally (using docker exec) and inside Docker (using psql directly)
# POSIX-compatible (works with sh, bash, dash, ash)

# Configuration
MIGRATIONS_DIR="${MIGRATIONS_DIR:-migrations}"

# Detect if we're running inside Docker or locally
if [ -f /.dockerenv ] || [ -n "$DATABASE_URL" ]; then
    # Running inside Docker - use psql directly with DATABASE_URL
    IN_DOCKER=true
    if [ -z "$DATABASE_URL" ]; then
        log_error "DATABASE_URL not set"
    fi
else
    # Running locally - use docker exec
    IN_DOCKER=false
    DB_HOST="${DB_HOST:-localhost}"
    DB_PORT="${DB_PORT:-5432}"
    DB_USER="${DB_USER:-toolbridge}"
    DB_NAME="${DB_NAME:-toolbridge}"
    DB_PASSWORD="${DB_PASSWORD:-dev-password}"
fi

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

# Function to run psql commands
function run_psql() {
    if [ "$IN_DOCKER" = true ]; then
        # Inside Docker - use psql with DATABASE_URL
        psql "$DATABASE_URL" "$@"
    else
        # Locally - use docker exec
        docker exec -i toolbridge-postgres psql -U "$DB_USER" -d "$DB_NAME" "$@"
    fi
}

# Function to check if database is ready
function check_db() {
    if [ "$IN_DOCKER" = true ]; then
        # Inside Docker - parse DATABASE_URL and use pg_isready
        # Extract host from DATABASE_URL (format: postgres://user:pass@host:port/db)
        DB_HOST_PARSED=$(echo "$DATABASE_URL" | sed -n 's|.*@\([^:]*\):.*|\1|p')
        pg_isready -h "$DB_HOST_PARSED" -U toolbridge > /dev/null 2>&1
    else
        # Locally - use docker exec
        docker exec toolbridge-postgres pg_isready -U "$DB_USER" > /dev/null 2>&1
    fi
}

# Check if PostgreSQL is accessible
log_warn "Checking database connection..."
if ! check_db; then
    log_error "Cannot connect to PostgreSQL. Is the container running?"
fi
log_info "Database connection OK"

# Create migrations tracking table if it doesn't exist
log_warn "Ensuring migrations table exists..."
run_psql <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    id SERIAL PRIMARY KEY,
    migration VARCHAR(255) UNIQUE NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL
log_info "Migrations table ready"

# Get list of applied migrations
applied_migrations=$(run_psql -t -c "SELECT migration FROM schema_migrations ORDER BY migration")

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
    if run_psql < "$migration_file"; then
        # Record successful migration
        run_psql <<SQL
INSERT INTO schema_migrations (migration) VALUES ('$migration_name');
SQL
        log_info "Successfully applied: $migration_name"
        pending_count=$((pending_count + 1))
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
run_psql -c "SELECT migration, applied_at FROM schema_migrations ORDER BY applied_at"
