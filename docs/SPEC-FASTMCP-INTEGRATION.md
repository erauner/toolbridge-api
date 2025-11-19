# SPEC: FastMCP Integration for ToolBridge

## Overview

Add a Python FastMCP layer inside each tenant-specific Fly.io app that proxies MCP tool calls to the existing Go REST API. This enables AI assistants to interact with ToolBridge using the Model Context Protocol while maintaining the Go API as the authoritative data layer.

## Architecture

### Process Layout

```
┌─────────────────────────────────────────────────┐
│  Fly.io App (per tenant)                        │
│  ┌───────────────────────────────────────────┐  │
│  │  Supervisor Process                       │  │
│  │  ┌─────────────┐    ┌──────────────────┐ │  │
│  │  │  FastMCP    │───▶│  Go REST API     │ │  │
│  │  │  (Python)   │    │  (toolbridge-api)│ │  │
│  │  │  :8001      │    │  :8080           │ │  │
│  │  └─────────────┘    └──────────────────┘ │  │
│  │        │                      │           │  │
│  │        │                      │           │  │
│  │        ▼                      ▼           │  │
│  │  Signed Headers         JWT + Headers    │  │
│  │  (X-TB-Tenant-*)        Validation       │  │
│  └───────────────────────────────────────────┘  │
│                                                  │
│  Fly Secrets: TENANT_HEADER_SECRET,             │
│              TENANT_ID, DATABASE_URL,           │
│              JWT_HS256_SECRET                   │
└─────────────────────────────────────────────────┘
```

### Request Flow

1. **MCP Client → Python MCP Service**
   - LLM makes MCP tool call (e.g., `list_notes()`)
   - FastMCP server receives request with Authorization header (JWT)

2. **Python → Go API (localhost)**
   - Python transport extracts JWT from MCP request headers
   - Computes HMAC-signed tenant headers using shared secret
   - Makes HTTP request to `http://localhost:8080/v1/notes`
   - Includes: Authorization (JWT) + X-TB-Tenant-* headers

3. **Go Middleware Validation**
   - JWT middleware validates Bearer token → extracts user_id
   - Tenant header middleware validates HMAC signature → extracts tenant_id
   - Request proceeds with both contexts available

4. **Service Layer Execution**
   - Go handlers use tenant_id for multi-tenant isolation
   - Returns JSON response to Python

5. **Python → MCP Client**
   - Transforms Go API response to MCP tool result
   - Returns structured data to LLM

## Security Model

### Dual Authentication Chain

**Layer 1: JWT (User Identity)**
- Standard Bearer token authentication
- Validates user has access to make requests
- Extracted by existing `auth.Middleware`

**Layer 2: Signed Tenant Headers (Tenant Isolation)**
- HMAC-SHA256 signed headers prevent cross-tenant access
- Only Python MCP service has the shared secret
- Prevents external callers from spoofing tenant context

**Header Format:**
```
X-TB-Tenant-ID: {tenant_id}
X-TB-Timestamp: {unix_timestamp_ms}
X-TB-Signature: {hmac_sha256_hex}
```

**Signature Computation:**
```python
# Python (signing)
message = f"{tenant_id}:{timestamp_ms}"
signature = hmac.new(
    key=secret.encode('utf-8'),
    msg=message.encode('utf-8'),
    digestmod=hashlib.sha256
).hexdigest()
```

```go
// Go (validation)
message := fmt.Sprintf("%s:%d", tenantID, timestamp)
mac := hmac.New(sha256.New, []byte(secret))
mac.Write([]byte(message))
expectedSig := hex.EncodeToString(mac.Sum(nil))
if !hmac.Equal([]byte(expectedSig), []byte(actualSig)) {
    return ErrInvalidSignature
}
```

**Timestamp Validation:**
- Accept requests within ±5 minutes of current time
- Prevents replay attacks with old signatures
- Tolerates reasonable clock skew

## Implementation Plan

### Phase 1: Python MCP Service Foundation

**Files to Create:**
```
toolbridge-api/
├── mcp/
│   ├── pyproject.toml
│   ├── uv.lock
│   └── toolbridge_mcp/
│       ├── __init__.py
│       ├── server.py           # FastMCP server definition
│       ├── config.py           # Settings (tenant_id, secret, etc.)
│       ├── async_client.py     # Client factory pattern
│       ├── transports/
│       │   └── tenant_direct.py  # Custom httpx transport
│       ├── utils/
│       │   ├── headers.py      # HMAC signing utilities
│       │   └── requests.py     # call_get, call_post, etc.
│       └── tools/
│           ├── __init__.py
│           ├── notes.py        # MCP tools for notes
│           ├── tasks.py        # MCP tools for tasks
│           ├── comments.py     # MCP tools for comments
│           ├── chats.py        # MCP tools for chats
│           └── chat_messages.py # MCP tools for chat messages
```

**Dependencies (pyproject.toml):**
```toml
[project]
name = "toolbridge-mcp"
version = "0.1.0"
requires-python = ">=3.12"

dependencies = [
    "fastmcp>=2.10.0",
    "httpx>=0.27.0",
    "pydantic>=2.0.0",
    "pydantic-settings>=2.0.0",
    "loguru>=0.7.0",
    "uvicorn[standard]>=0.30.0",
]
```

**Key Implementation:**

`mcp/toolbridge_mcp/config.py`:
```python
from pydantic_settings import BaseSettings

class Settings(BaseSettings):
    tenant_id: str
    tenant_header_secret: str
    go_api_base_url: str = "http://localhost:8080"
    log_level: str = "INFO"
    
    class Config:
        env_prefix = "TOOLBRIDGE_"
        env_file = ".env"

settings = Settings()
```

`mcp/toolbridge_mcp/server.py`:
```python
from fastmcp import FastMCP

mcp = FastMCP(
    name="ToolBridge",
    version="0.1.0",
    description="MCP server for ToolBridge note-taking and task management"
)

# Tools registered via imports
from toolbridge_mcp.tools import notes, tasks, comments, chats, chat_messages
```

### Phase 2: Tenant Header Signing (Python)

**File:** `mcp/toolbridge_mcp/utils/headers.py`

```python
import hmac
import hashlib
import time
from typing import Dict

class TenantHeaderSigner:
    """Signs outbound requests to Go API with HMAC tenant headers."""
    
    def __init__(self, secret: str, tenant_id: str, skew_seconds: int = 300):
        self.secret = secret
        self.tenant_id = tenant_id
        self.skew_seconds = skew_seconds
    
    def sign(self) -> Dict[str, str]:
        """Generate signed tenant headers for current timestamp."""
        timestamp_ms = int(time.time() * 1000)
        message = f"{self.tenant_id}:{timestamp_ms}"
        
        signature = hmac.new(
            key=self.secret.encode('utf-8'),
            msg=message.encode('utf-8'),
            digestmod=hashlib.sha256
        ).hexdigest()
        
        return {
            "X-TB-Tenant-ID": self.tenant_id,
            "X-TB-Timestamp": str(timestamp_ms),
            "X-TB-Signature": signature,
        }
```

### Phase 3: Custom Transport (Python)

**File:** `mcp/toolbridge_mcp/transports/tenant_direct.py`

```python
from contextlib import asynccontextmanager
from typing import AsyncGenerator
import httpx
from fastmcp.server.dependencies import get_http_headers
from toolbridge_mcp.config import settings
from toolbridge_mcp.utils.headers import TenantHeaderSigner

class TenantDirectTransport(httpx.AsyncBaseTransport):
    """Transport that adds tenant headers to outbound Go API requests."""
    
    def __init__(self):
        self.signer = TenantHeaderSigner(
            secret=settings.tenant_header_secret,
            tenant_id=settings.tenant_id
        )
        # Create underlying HTTP transport for actual requests
        self._transport = httpx.AsyncHTTPTransport()
    
    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        # Add tenant headers to request
        tenant_headers = self.signer.sign()
        for key, value in tenant_headers.items():
            request.headers[key] = value
        
        # Forward to Go API
        return await self._transport.handle_async_request(request)
    
    async def aclose(self):
        await self._transport.aclose()

@asynccontextmanager
async def get_client() -> AsyncGenerator[httpx.AsyncClient, None]:
    """Context manager that provides httpx client with tenant transport."""
    async with httpx.AsyncClient(
        transport=TenantDirectTransport(),
        base_url=settings.go_api_base_url,
        timeout=httpx.Timeout(30.0)
    ) as client:
        yield client
```

### Phase 4: Request Helpers (Python)

**File:** `mcp/toolbridge_mcp/utils/requests.py`

```python
from typing import Any, Dict, Optional
import httpx
from fastmcp.server.dependencies import get_http_headers
from loguru import logger

async def get_auth_header() -> str:
    """Extract Authorization header from current MCP request context."""
    headers = get_http_headers()
    auth = headers.get("authorization") or headers.get("Authorization")
    if not auth:
        raise ValueError("Missing Authorization header in MCP request")
    return auth

async def call_get(
    client: httpx.AsyncClient,
    path: str,
    params: Optional[Dict[str, Any]] = None
) -> httpx.Response:
    """Make GET request to Go API with auth header."""
    auth_header = await get_auth_header()
    headers = {"Authorization": auth_header}
    
    logger.debug(f"GET {path} params={params}")
    response = await client.get(path, params=params, headers=headers)
    response.raise_for_status()
    return response

async def call_post(
    client: httpx.AsyncClient,
    path: str,
    json: Optional[Dict[str, Any]] = None
) -> httpx.Response:
    """Make POST request to Go API with auth header."""
    auth_header = await get_auth_header()
    headers = {"Authorization": auth_header}
    
    logger.debug(f"POST {path} json={json}")
    response = await client.post(path, json=json, headers=headers)
    response.raise_for_status()
    return response

# Similar helpers for PUT, PATCH, DELETE...
```

### Phase 5: MCP Tool Implementation (Python)

**File:** `mcp/toolbridge_mcp/tools/notes.py`

```python
from typing import Annotated, List, Optional
from pydantic import BaseModel, Field
from fastmcp import tool
from toolbridge_mcp.transports.tenant_direct import get_client
from toolbridge_mcp.utils.requests import call_get, call_post, call_delete

class Note(BaseModel):
    uid: str
    version: int
    updated_at: str
    deleted_at: Optional[str] = None
    payload: dict

class NotesListResponse(BaseModel):
    items: List[Note]
    next_cursor: Optional[str] = None

@tool(
    name="list_notes",
    description="List notes with pagination. Returns notes for the authenticated tenant."
)
async def list_notes(
    limit: Annotated[int, Field(ge=1, le=1000)] = 100,
    cursor: Optional[str] = None,
    include_deleted: bool = False
) -> NotesListResponse:
    """List notes from ToolBridge API."""
    async with get_client() as client:
        params = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        if include_deleted:
            params["includeDeleted"] = "true"
        
        response = await call_get(client, "/v1/notes", params=params)
        data = response.json()
        return NotesListResponse(**data)

@tool(
    name="create_note",
    description="Create a new note. Server generates UID automatically."
)
async def create_note(
    title: str,
    content: str,
    tags: Optional[List[str]] = None
) -> Note:
    """Create a new note in ToolBridge."""
    async with get_client() as client:
        payload = {
            "title": title,
            "content": content,
        }
        if tags:
            payload["tags"] = tags
        
        response = await call_post(client, "/v1/notes", json=payload)
        data = response.json()
        return Note(**data)

@tool(
    name="get_note",
    description="Retrieve a single note by UID."
)
async def get_note(uid: str) -> Note:
    """Get a note by UID from ToolBridge."""
    async with get_client() as client:
        response = await call_get(client, f"/v1/notes/{uid}")
        data = response.json()
        return Note(**data)

@tool(
    name="delete_note",
    description="Soft delete a note by UID. Note remains in database but marked deleted."
)
async def delete_note(uid: str) -> Note:
    """Delete a note in ToolBridge."""
    async with get_client() as client:
        response = await call_delete(client, f"/v1/notes/{uid}")
        data = response.json()
        return Note(**data)

# Similar tools for update_note, archive_note, etc.
```

### Phase 6: Go Tenant Header Validation Middleware

**File:** `internal/auth/tenant_headers.go`

```go
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// Tenant context key for storing validated tenant ID
type tenantCtxKey string

const TenantIDKey tenantCtxKey = "tenant_id"

var (
	ErrMissingTenantID    = errors.New("missing X-TB-Tenant-ID header")
	ErrMissingTimestamp   = errors.New("missing X-TB-Timestamp header")
	ErrMissingSignature   = errors.New("missing X-TB-Signature header")
	ErrInvalidTimestamp   = errors.New("invalid timestamp format")
	ErrTimestampSkew      = errors.New("timestamp outside acceptable window")
	ErrInvalidSignature   = errors.New("invalid HMAC signature")
)

// TenantHeaders contains validated tenant context
type TenantHeaders struct {
	TenantID  string
	Timestamp time.Time
}

// ValidateTenantHeaders validates HMAC-signed tenant headers
func ValidateTenantHeaders(r *http.Request, secret string, maxSkewSeconds int64) (*TenantHeaders, error) {
	// Extract headers
	tenantID := r.Header.Get("X-TB-Tenant-ID")
	timestampStr := r.Header.Get("X-TB-Timestamp")
	signature := r.Header.Get("X-TB-Signature")

	if tenantID == "" {
		return nil, ErrMissingTenantID
	}
	if timestampStr == "" {
		return nil, ErrMissingTimestamp
	}
	if signature == "" {
		return nil, ErrMissingSignature
	}

	// Parse timestamp
	timestampMs, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, ErrInvalidTimestamp
	}

	// Validate timestamp within acceptable window
	now := time.Now()
	requestTime := time.UnixMilli(timestampMs)
	skew := now.Sub(requestTime).Abs().Seconds()

	if skew > float64(maxSkewSeconds) {
		log.Warn().
			Str("tenant_id", tenantID).
			Int64("timestamp_ms", timestampMs).
			Float64("skew_seconds", skew).
			Msg("tenant header timestamp outside acceptable window")
		return nil, ErrTimestampSkew
	}

	// Verify HMAC signature
	message := fmt.Sprintf("%s:%s", tenantID, timestampStr)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison to prevent timing attacks
	if !hmac.Equal([]byte(expectedSig), []byte(signature)) {
		log.Warn().
			Str("tenant_id", tenantID).
			Str("expected", expectedSig).
			Str("actual", signature).
			Msg("tenant header signature mismatch")
		return nil, ErrInvalidSignature
	}

	return &TenantHeaders{
		TenantID:  tenantID,
		Timestamp: requestTime,
	}, nil
}

// TenantHeaderMiddleware validates tenant headers on all requests
func TenantHeaderMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Validate tenant headers
			headers, err := ValidateTenantHeaders(r, secret, 300) // 5 minute window
			if err != nil {
				log.Error().
					Err(err).
					Str("path", r.URL.Path).
					Msg("tenant header validation failed")
				http.Error(w, "Unauthorized: invalid tenant headers", http.StatusUnauthorized)
				return
			}

			// Store tenant context
			ctx := context.WithValue(r.Context(), TenantIDKey, headers.TenantID)
			
			log.Debug().
				Str("tenant_id", headers.TenantID).
				Str("path", r.URL.Path).
				Msg("tenant headers validated")

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantID extracts tenant ID from request context
func TenantID(ctx context.Context) string {
	if tenantID, ok := ctx.Value(TenantIDKey).(string); ok {
		return tenantID
	}
	return ""
}
```

**Update:** `internal/httpapi/router.go`

```go
// Add tenant header validation middleware after JWT
r.Group(func(r chi.Router) {
	r.Use(auth.Middleware(s.DB, jwt))
	r.Use(auth.TenantHeaderMiddleware(os.Getenv("TENANT_HEADER_SECRET")))
	
	// ... existing routes
})
```

### Phase 7: Multi-Stage Dockerfile

**File:** `Dockerfile.mcp`

```dockerfile
# Stage 1: Build Go binary
FROM golang:1.24-alpine AS go-builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/toolbridge-api ./cmd/server

# Stage 2: Install Python dependencies
FROM python:3.12-slim AS python-builder
COPY --from=ghcr.io/astral-sh/uv:latest /uv /bin/
WORKDIR /app/mcp
COPY mcp/pyproject.toml mcp/uv.lock ./
RUN uv sync --frozen --no-dev

# Stage 3: Runtime
FROM python:3.12-slim
RUN apt-get update && apt-get install -y \
    ca-certificates \
    wget \
    supervisor \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy Go binary
COPY --from=go-builder /app/toolbridge-api /app/toolbridge-api

# Copy Python MCP service
COPY --from=python-builder /app/mcp/.venv /app/mcp/.venv
COPY mcp /app/mcp

# Copy supervisor config
COPY deployment/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Non-root user
RUN useradd -m -u 1000 app && chown -R app:app /app
USER app

# Environment
ENV PATH="/app/mcp/.venv/bin:$PATH" \
    PYTHONUNBUFFERED=1

# Health check (Go API)
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

EXPOSE 8080 8001

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
```

**File:** `deployment/supervisord.conf`

```ini
[supervisord]
nodaemon=true
user=app
logfile=/dev/stdout
logfile_maxbytes=0
loglevel=info

[program:toolbridge-api]
command=/app/toolbridge-api
autostart=true
autorestart=true
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0
environment=HTTP_ADDR=":8080"

[program:toolbridge-mcp]
command=uvicorn toolbridge_mcp.server:app --host 0.0.0.0 --port 8001 --log-level info
directory=/app/mcp
autostart=true
autorestart=true
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0
```

### Phase 8: Fly.io Configuration

**File:** `fly.mcp.toml` (template for tenant apps)

```toml
app = "toolbridge-tenant-{TENANT_ID}"
primary_region = "ord"

[build]
  dockerfile = "Dockerfile.mcp"

[env]
  ENV = "production"
  HTTP_ADDR = ":8080"
  TOOLBRIDGE_GO_API_BASE_URL = "http://localhost:8080"
  TOOLBRIDGE_LOG_LEVEL = "INFO"

[[services]]
  internal_port = 8080
  protocol = "tcp"

  [[services.ports]]
    handlers = ["http"]
    port = 80

  [[services.ports]]
    handlers = ["tls", "http"]
    port = 443

  [services.concurrency]
    type = "connections"
    hard_limit = 100
    soft_limit = 80

  [[services.tcp_checks]]
    interval = "15s"
    timeout = "2s"
    grace_period = "5s"

[[services]]
  internal_port = 8001
  protocol = "tcp"

  [[services.ports]]
    handlers = ["http"]
    port = 8001

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = true
  auto_start_machines = true
  min_machines_running = 0

[checks]
  [checks.api_health]
    type = "http"
    interval = "30s"
    timeout = "5s"
    grace_period = "10s"
    method = "GET"
    path = "/healthz"
    port = 8080

# Secrets to set via `fly secrets set`:
# - DATABASE_URL
# - JWT_HS256_SECRET
# - TENANT_HEADER_SECRET
# - TOOLBRIDGE_TENANT_ID
```

### Phase 9: Testing & Validation

**Integration Tests:**

1. **Test tenant header validation in Go**
   ```go
   func TestTenantHeaderValidation(t *testing.T) {
       secret := "test-secret"
       tenantID := "tenant-123"
       
       // Valid signature
       headers := signTenantHeaders(secret, tenantID, time.Now())
       req := httptest.NewRequest("GET", "/test", nil)
       for k, v := range headers {
           req.Header.Set(k, v)
       }
       
       validated, err := auth.ValidateTenantHeaders(req, secret, 300)
       assert.NoError(t, err)
       assert.Equal(t, tenantID, validated.TenantID)
       
       // Invalid signature
       req.Header.Set("X-TB-Signature", "invalid")
       _, err = auth.ValidateTenantHeaders(req, secret, 300)
       assert.Error(t, err)
   }
   ```

2. **Test Python MCP tools**
   ```python
   @pytest.mark.asyncio
   async def test_list_notes():
       async with get_client() as client:
           response = await call_get(client, "/v1/notes", params={"limit": 10})
           assert response.status_code == 200
           data = response.json()
           assert "items" in data
   ```

3. **End-to-end test**
   ```bash
   # Start local services
   docker-compose up -d postgres
   supervisord -c deployment/supervisord.conf
   
   # Test MCP tool
   curl -X POST http://localhost:8001/mcp/list_notes \
     -H "Authorization: Bearer $JWT_TOKEN" \
     -d '{"limit": 10}'
   ```

## Migration & Deployment

### Deployment Sequence

1. **Build new Docker image**
   ```bash
   docker build -f Dockerfile.mcp -t toolbridge-mcp:latest .
   ```

2. **Deploy to Fly.io**
   ```bash
   # Set secrets for tenant app
   fly secrets set \
     DATABASE_URL="postgres://..." \
     JWT_HS256_SECRET="..." \
     TENANT_HEADER_SECRET="randomly-generated-256-bit-secret" \
     TOOLBRIDGE_TENANT_ID="tenant-123" \
     -a toolbridge-tenant-123
   
   # Deploy
   fly deploy -c fly.mcp.toml -a toolbridge-tenant-123
   ```

3. **Verify health**
   ```bash
   # Check Go API
   curl https://toolbridge-tenant-123.fly.dev/healthz
   
   # Check MCP service (via MCP inspector)
   npx @modelcontextprotocol/inspector \
     --url https://toolbridge-tenant-123.fly.dev:8001/mcp
   ```

### Rollback Plan

If issues arise:
1. Revert to Go-only Docker image
2. Update Fly.io deployment config
3. Document issues for future mitigation

## Success Criteria

- [ ] Python MCP service starts successfully alongside Go API
- [ ] Tenant header validation blocks unauthorized requests
- [ ] MCP tools successfully proxy to Go REST endpoints
- [ ] All 5 entity types (notes, tasks, comments, chats, chat_messages) have functional MCP tools
- [ ] Performance acceptable (<100ms overhead for MCP → Go hop)
- [ ] Container startup time <10 seconds
- [ ] Health checks pass for both services
- [ ] No security regressions (dual auth maintained)
- [ ] Documentation complete and accurate

## Open Questions

1. **Observability:** How to correlate logs between Python and Go services?
   - **Answer:** Use correlation IDs in headers, structured logging in both
2. **Resource sizing:** What Fly.io VM size needed for both processes?
   - **Answer:** Start with `shared-cpu-2x` (2048MB), monitor and adjust
3. **Session management:** Should we move to Redis for multi-replica future?
   - **Answer:** Document Redis plan but defer implementation (single-replica sufficient for now)
4. **Tool discovery:** How do LLMs discover available MCP tools?
   - **Answer:** FastMCP provides automatic tool listing via MCP protocol

## References

- Basic Memory SPEC-16: MCP Cloud Service Consolidation
- Basic Memory SPEC-9: Signed Header Tenant Information
- FastMCP Documentation: https://github.com/jlowin/fastmcp
- ToolBridge REST API: `/Users/erauner/git/side/toolbridge-api/README.md`
