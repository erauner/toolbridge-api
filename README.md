# ToolBridge API

Delta sync backend for ToolBridge. Implements Last-Write-Wins (LWW) conflict resolution with cursor-based pagination.

> **Production Transport: REST API Only**
> This service is deployed with REST API. gRPC is NOT used in production.
> See [DEPLOYMENT.md](./DEPLOYMENT.md) for details. gRPC code exists in experimental branch only.

## Architecture

**Tech Stack:**
- Go 1.22
- PostgreSQL 16
- JWT authentication (HS256 + Auth0 RS256)
- Chi HTTP router
- REST API (production)

**Sync Protocol:**
- **Push**: Client sends local changes → Server applies with LWW
- **Pull**: Client fetches server changes using cursor pagination
- **Cursor**: Base64-encoded `<timestamp_ms>|<uuid>` for deterministic ordering
- **Conflict Resolution**: Last-Write-Wins based on `updated_at_ms`
- **Idempotency**: Duplicate pushes with same timestamp don't bump version

## Project Structure

```
toolbridge-api/
├── cmd/
│   └── server/           # Main entry point
├── internal/
│   ├── auth/            # JWT authentication middleware
│   ├── db/              # Postgres connection pool
│   ├── httpapi/         # HTTP handlers (push/pull endpoints)
│   └── syncx/           # Sync utilities (cursor, extraction)
├── migrations/          # Database schema
├── docker-compose.yml   # Local Postgres
├── Dockerfile           # Production image
├── Makefile             # Dev commands
└── go.mod
```

## Quick Start

### 1. Start Local Postgres

```bash
make docker-up
# Postgres will be available at localhost:5432
# Database: toolbridge
# User: toolbridge
# Password: dev-password
```

Migrations run automatically on first start (via docker-compose `initdb.d`).

### 2. Run API Server

```bash
make dev
# Server starts at http://localhost:8081
```

### 3. Test Sync

**Create a test user:**
```bash
curl -X POST http://localhost:8081/v1/sync/notes/push \
  -H 'X-Debug-Sub: demo-user' \
  -H 'Content-Type: application/json' \
  -d '{
    "items": [{
      "uid": "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
      "title": "Test Note",
      "content": "Hello from API",
      "sync": {
        "version": 1,
        "isDeleted": false
      },
      "updatedTs": "2025-11-03T10:00:00Z"
    }]
  }'
```

**Pull notes:**
```bash
curl 'http://localhost:8081/v1/sync/notes/pull?limit=100' \
  -H 'X-Debug-Sub: demo-user'
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | (required) | Postgres connection string |
| `JWT_HS256_SECRET` | `dev-secret-change-in-production` | JWT signing secret |
| `HTTP_ADDR` | `:8081` | HTTP server address |
| `ENV` | `dev` | Environment (`dev` enables pretty logs) |

## Authentication

Two modes:

1. **Production**: Bearer token with JWT
   ```bash
   curl -H 'Authorization: Bearer <jwt-token>' ...
   ```

2. **Development**: Debug header (bypasses JWT)
   ```bash
   curl -H 'X-Debug-Sub: demo-user' ...
   ```

JWT must contain `sub` claim (user identifier). User is created automatically on first auth.

## API Endpoints

### Health Check
```
GET /healthz
```

### Push Notes
```
POST /v1/sync/notes/push
Content-Type: application/json
Authorization: Bearer <token>

{
  "items": [
    {
      "uid": "<uuid>",
      "title": "...",
      "content": "...",
      "sync": {
        "version": 1,
        "isDeleted": false
      },
      "updatedTs": "<RFC3339>"
    }
  ]
}
```

**Response:**
```json
[
  {
    "uid": "<uuid>",
    "version": 2,
    "updatedAt": "2025-11-03T10:00:00.123Z"
  }
]
```

### Pull Notes
```
GET /v1/sync/notes/pull?limit=500&cursor=<opaque>
Authorization: Bearer <token>
```

**Response:**
```json
{
  "upserts": [
    { /* full note JSON */ }
  ],
  "deletes": [
    {
      "uid": "<uuid>",
      "deletedAt": "2025-11-03T10:00:00.123Z"
    }
  ],
  "nextCursor": "<opaque-base64-string>"
}
```

## Development

**Install dependencies:**
```bash
go mod download
```

**Run tests:**
```bash
make test
```

**Build binary:**
```bash
make build
# Binary at ./bin/server
```

**Stop Postgres:**
```bash
make docker-down
```

## Database Schema

See `migrations/0001_init.sql` for the complete schema.

**Key tables:**
- `app_user`: User accounts (mapped from JWT `sub`)
- `note`: Notes with delta sync columns

**Delta sync columns:**
- `uid`: UUID primary key
- `owner_id`: Tenant isolation
- `updated_at_ms`: Unix milliseconds for cursor pagination
- `deleted_at_ms`: Tombstone (NULL = active)
- `version`: Server-controlled version number
- `payload_json`: Original client JSON (preserved)

## Deployment

**Build Docker image:**
```bash
make docker-build
```

**Run with Docker:**
```bash
docker run -p 8081:8081 \
  -e DATABASE_URL=postgres://user:pass@host:5432/db \
  -e JWT_HS256_SECRET=your-secret \
  toolbridge-api:latest
```

**Kubernetes manifests:** Coming soon (will integrate with CloudNativePG)

## Conflict Resolution (LWW)

- **Winner**: Entity with highest `updated_at_ms` wins
- **Version**: Increments only on strictly newer updates (`WHERE updated_at_ms > old`)
- **Idempotency**: Duplicate push with same timestamp → no version bump
- **Tombstones**: Deleted entities marked with `deleted_at_ms` (preserved for sync)

## Cursor Format

Base64-encoded: `<updated_at_ms>|<uuid>`

Example:
```
Input:  { Ms: 1730635200000, UID: "c1d9b7dc-..." }
Output: "MTczMDYzNTIwMDAwMHxjMWQ5YjdkYy0uLi4"
```

Ensures lexicographically ordered, deterministic pagination.

## Troubleshooting

**Connection refused:**
```bash
# Check Postgres is running
docker ps | grep toolbridge-postgres

# Check logs
docker logs toolbridge-postgres
```

**Migrations didn't run:**
```bash
# Run manually
make migrate
```

**Token validation fails:**
```bash
# Use debug header for local testing
curl -H 'X-Debug-Sub: test-user' ...
```

## Roadmap

- [x] Notes sync (push/pull)
- [ ] Tasks sync
- [ ] Comments sync (with parent validation)
- [ ] Chats sync
- [ ] Chat messages sync
- [ ] gRPC API (optional)
- [ ] Kubernetes manifests
- [ ] CI/CD pipeline

## License

MIT
