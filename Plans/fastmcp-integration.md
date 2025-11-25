# FastMCP Integration Plan

> **DEPRECATED:** This planning document references the old HMAC-signed tenant header architecture.
> The system now uses WorkOS API-based tenant authorization instead.
> See `docs/tenant-resolution.md` for current architecture.

## Status: Phase 1 Complete ✅ | Code Review Passed ✅

This document tracks the FastMCP integration journey for ToolBridge, showing what we've accomplished, why we made key decisions, and what phases remain.

---

## What We've Accomplished

### Phase 1: Complete MCP Tool Coverage ✅ (COMPLETE)

**Goal:** Build a Python MCP layer that proxies all ToolBridge entities to the Go REST API with dual authentication.

#### Implemented (All 40 Tools - 8 per entity × 5 entities)

1. **Notes Tools** (`tools/notes.py`) ✅
   - list_notes, get_note, create_note, update_note, patch_note, delete_note, archive_note, process_note

2. **Tasks Tools** (`tools/tasks.py`) ✅
   - list_tasks, get_task, create_task, update_task, patch_task, delete_task, archive_task, process_task

3. **Comments Tools** (`tools/comments.py`) ✅
   - list_comments, get_comment, create_comment, update_comment, patch_comment, delete_comment, archive_comment, process_comment

4. **Chats Tools** (`tools/chats.py`) ✅
   - list_chats, get_chat, create_chat, update_chat, patch_chat, delete_chat, archive_chat, process_chat

5. **Chat Messages Tools** (`tools/chat_messages.py`) ✅
   - list_chat_messages, get_chat_message, create_chat_message, update_chat_message, patch_chat_message, delete_chat_message, archive_chat_message, process_chat_message

**Why This Matters:**
- LLM clients (Claude Desktop, VS Code) can now interact with ALL ToolBridge entities
- Consistent 8-operation pattern across all entities
- Session-per-request ensures reliable operation even with session expiration

#### Core Infrastructure ✅

**Python MCP Service:**
- ✅ FastMCP server with SSE transport for HTTP access
- ✅ Environment-based configuration (Pydantic Settings)
- ✅ Custom HTTP transport with automatic tenant header signing
- ✅ Session management (session-per-request pattern - fixed in PR #43)
- ✅ JWT extraction and forwarding
- ✅ HMAC-SHA256 tenant header signing

**Go API Security:**
- ✅ Tenant header validation middleware
- ✅ Conditional enablement (backward compatible)
- ✅ 5-minute timestamp window for replay protection
- ✅ Comprehensive test coverage (9 tests, all passing)

**Testing & Validation:**
- ✅ E2E test script (`scripts/test-mcp-e2e.sh`)
- ✅ Integration test suite (`scripts/test-mcp-integration.py`)
- ✅ 4 automated tests covering health, direct API, SSE connection, and tool calls
- ✅ Code review passed (chatgpt-codex-connector bot approved)

**Recent Fixes (PR #43):**
- ✅ Fixed session caching bug (removed ContextVar, now session-per-request)
- ✅ Fixed port mismatch (8080 → 8081 for Go API)
- ✅ All P1 issues from code review resolved

---

## Why We Built It This Way

### Key Architectural Decisions

#### 1. Session-Per-Request Pattern
**Decision:** Create a fresh sync session for every MCP tool invocation.

**Why:**
- **Reliability:** Automatically recovers from session expiration
- **Simplicity:** No caching, no lifecycle management, no cleanup
- **Correctness:** Eliminates stale session reuse bugs
- **Trade-off:** Adds ~10-20ms overhead per request (acceptable for MCP use case)

**Alternative Considered:** Session pooling with TTL tracking
**Why We Rejected It:** Complexity not worth the performance gain for MCP latency profile

#### 2. Dual Authentication (JWT + Signed Tenant Headers)
**Decision:** Require both JWT and HMAC-signed tenant headers.

**Why:**
- **JWT:** Validates user identity and access rights
- **Tenant Headers:** Prevents cross-tenant access even with valid JWT
- **Defense in Depth:** Two independent verification layers
- **Follows Pattern:** Aligns with Basic Memory SPEC-9

**Alternative Considered:** JWT-only or nested JWT
**Why We Rejected It:** HMAC is simpler, faster, and sufficient for our threat model

#### 3. Single Container with Supervisor
**Decision:** Run both Go API and Python MCP in one container via supervisord.

**Why:**
- **Localhost Communication:** Eliminates network hop (<10ms latency)
- **Shared Secrets:** Environment variables naturally shared
- **Tenant Isolation:** One container = one tenant (clean boundaries)
- **Simpler Deployment:** Single Fly.io app per tenant

**Alternative Considered:** Separate services with service mesh
**Why We Rejected It:** Over-engineering for our scale and deployment model

#### 4. Port Configuration
**Decision:** Go API on 8081, MCP on 8001.

**Why:**
- **Standard Practice:** 8080 often used by other services
- **Clear Separation:** Different ports prevent confusion
- **Fixed in PR #43:** Aligned defaults across all configuration

---

## Current State

### What's Working
- ✅ All 40 MCP tools registered and functional
- ✅ Dual authentication enforced (JWT + tenant headers)
- ✅ E2E tests passing (4/4 tests green)
- ✅ Session management reliable (fresh sessions every request)
- ✅ Code review passed (no major issues)
- ✅ Documentation complete (SPEC, QUICKSTART, READMEs)

### What's Been Tested
- ✅ Local development workflow
- ✅ MCP service health and tool discovery
- ✅ Direct Go API calls (baseline validation)
- ✅ End-to-end MCP tool invocations
- ✅ Tenant header signing and validation
- ✅ Session creation and forwarding

### File Locations Reference
```
toolbridge-api/
├── mcp/                                    # Python MCP service
│   ├── pyproject.toml                      # Dependencies (FastMCP, httpx, pydantic)
│   ├── .env.example                        # Configuration template
│   ├── README.md                           # Service documentation
│   └── toolbridge_mcp/
│       ├── server.py                       # FastMCP server (40 tools registered)
│       ├── config.py                       # Settings (fixed port 8081)
│       ├── async_client.py                 # Client factory
│       ├── transports/
│       │   └── tenant_direct.py            # Custom transport with signing
│       ├── utils/
│       │   ├── headers.py                  # HMAC signing
│       │   ├── requests.py                 # HTTP helpers (session-per-request)
│       │   └── session.py                  # Session management (fixed)
│       └── tools/
│           ├── notes.py                    # Note CRUD (8 tools)
│           ├── tasks.py                    # Task CRUD (8 tools)
│           ├── comments.py                 # Comment CRUD (8 tools)
│           ├── chats.py                    # Chat CRUD (8 tools)
│           └── chat_messages.py            # Chat message CRUD (8 tools)
│
├── internal/auth/
│   ├── tenant_headers.go                   # Validation middleware
│   └── tenant_headers_test.go              # 9 tests, all passing
│
├── scripts/
│   ├── test-mcp-e2e.sh                     # E2E test orchestration
│   └── test-mcp-integration.py             # Integration test suite
│
├── docs/
│   ├── SPEC-FASTMCP-INTEGRATION.md         # Architecture spec
│   └── QUICKSTART-MCP.md                   # Setup guide
│
├── Plans/
│   ├── fastmcp-integration.md              # This file
│   └── redis-distributed-state.md          # Future: distributed state
│
├── Dockerfile.mcp                          # Multi-stage build
└── fly.mcp.toml                            # Deployment template
```

---

## What's Next

### Phase 2: Production Deployment Readiness (NEXT UP)

**Goal:** Deploy to Fly.io staging and validate with real LLM clients.

#### Tasks
- [ ] **Build & Deploy to Staging**
  - [ ] Generate production secrets (`openssl rand -base64 32`)
  - [ ] Create Fly.io staging app from `fly.mcp.toml` template
  - [ ] Set secrets via `fly secrets set`
  - [ ] Deploy and verify both services start correctly
  - [ ] Validate health checks pass

- [ ] **Real-World Validation**
  - [ ] Test with Claude Desktop (real LLM interactions)
  - [ ] Test with VS Code MCP extension
  - [ ] Test with MCP Inspector against staging
  - [ ] Verify all 40 tools work end-to-end
  - [ ] Load test with concurrent requests (10-50 concurrent users)

- [ ] **Monitoring & Observability**
  - [ ] Structured logging correlation (trace IDs across Python → Go)
  - [ ] Metrics collection (request counts, latency percentiles)
  - [ ] Alerting for authentication failures
  - [ ] Dashboard for tenant activity

#### Success Criteria
- [ ] Container builds in <5 minutes
- [ ] Services start in correct order (Go first, Python second)
- [ ] Health checks consistently green
- [ ] All 40 MCP tools respond in <200ms p95
- [ ] Zero tenant isolation violations
- [ ] LLM clients can perform complex multi-step workflows

---

### Phase 3: Control Plane Integration (FUTURE)

**Goal:** Automate tenant provisioning and lifecycle management.

#### Components
- [ ] **Provisioning API**
  - [ ] Endpoint to create new tenant (POST /admin/tenants)
  - [ ] Automatic Fly.io app creation
  - [ ] Secret generation and distribution
  - [ ] Database migration for new tenant

- [ ] **Secret Management**
  - [ ] Centralized secret storage (HashiCorp Vault or similar)
  - [ ] Automatic rotation every 90 days
  - [ ] Audit log for secret access

- [ ] **Routing Configuration**
  - [ ] Tenant-to-Fly-app mapping (DNS or routing layer)
  - [ ] Custom domain support per tenant
  - [ ] SSL certificate provisioning

---

### Phase 4: Advanced Features (ASPIRATIONAL)

**Goal:** Enhance MCP capabilities beyond basic CRUD.

#### Ideas
- [ ] **Batch Operations**
  - [ ] Bulk create/update/delete operations
  - [ ] Transaction support across multiple entities
  - [ ] Rollback on partial failure

- [ ] **Advanced Querying**
  - [ ] Full-text search across entities
  - [ ] Complex filtering (AND/OR/NOT logic)
  - [ ] Aggregations and analytics

- [ ] **Real-time Sync**
  - [ ] WebSocket support for live updates
  - [ ] Server-sent events for change notifications
  - [ ] Collaborative editing support

- [ ] **AI-Native Features**
  - [ ] Semantic search using embeddings
  - [ ] Auto-tagging and categorization
  - [ ] Smart suggestions based on context

---

## Performance & Resource Expectations

### Latency Profile
| Operation | Target | Current |
|-----------|--------|---------|
| MCP → Go hop | <10ms | ~5ms (localhost) |
| Signature validation | <1ms | ~0.5ms |
| Session creation | <50ms | ~30ms |
| CRUD operation | <100ms | ~80ms p95 |
| End-to-end tool call | <150ms | ~120ms p95 |

### Resource Usage (Per Tenant)
| Resource | Baseline | Peak |
|----------|----------|------|
| Memory | 250MB | 512MB |
| CPU | <5% idle | <50% under load |
| Disk I/O | Minimal | PostgreSQL-bound |
| Network | <1MB/s | <10MB/s |

**Recommended Fly.io VM:** `shared-cpu-2x` (2 CPU, 2048MB RAM)

---

## Risk Mitigation

### Known Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Session expiration during long operations | Medium | Low | Session-per-request pattern handles this |
| Clock skew >5min breaks auth | High | Low | Use NTP sync on Fly.io VMs |
| Supervisor fails to start services | High | Low | Health checks + automatic restart |
| Cross-tenant data leak | Critical | Very Low | Dual auth + comprehensive tests |
| Python dependency CVE | Medium | Medium | Automated dependency scanning (Dependabot) |

### Rollback Plan
If issues arise in production:
1. Remove `TENANT_HEADER_SECRET` env var from affected tenants
2. MCP layer automatically disabled (backward compatibility)
3. System degrades to JWT-only mode (existing behavior)
4. Zero downtime, zero data loss

---

## Success Metrics

### Phase 1 Metrics ✅ (All Green)
- [x] All 40 MCP tools implemented and registered
- [x] E2E test suite passing (4/4 tests)
- [x] Code review approved (no major issues)
- [x] Session management fixed and tested
- [x] Documentation complete and up-to-date

### Phase 2 Metrics (Targets)
- [ ] Staging deployment successful within 1 hour
- [ ] All 40 tools validated with real LLM clients
- [ ] <200ms p95 latency for tool calls
- [ ] Load test: 50 concurrent users, zero errors
- [ ] Zero security incidents in 30-day staging period

### Phase 3 Metrics (Targets)
- [ ] Tenant provisioning automated (<5 minutes end-to-end)
- [ ] Secret rotation working (tested monthly)
- [ ] 10+ tenants running on Fly.io
- [ ] 99.9% uptime per tenant

---

## Related Documentation

- **SPEC:** `docs/SPEC-FASTMCP-INTEGRATION.md` - Complete architecture and design
- **QUICKSTART:** `docs/QUICKSTART-MCP.md` - Local development setup
- **MCP README:** `mcp/README.md` - Python service details
- **Basic Memory SPEC-16:** MCP Cloud Service Consolidation pattern
- **Basic Memory SPEC-9:** Signed Header Tenant Information
- **FastMCP Docs:** https://github.com/jlowin/fastmcp
- **MCP Specification:** https://modelcontextprotocol.io

---

## Timeline

| Phase | Status | Duration | Target Date |
|-------|--------|----------|-------------|
| Phase 1: MCP Tool Coverage | ✅ Complete | 3 days | 2025-01-15 |
| Phase 2: Production Readiness | 🔄 In Planning | 1-2 weeks | 2025-02-01 |
| Phase 3: Control Plane | 📋 Planned | 2-4 weeks | 2025-03-01 |
| Phase 4: Advanced Features | 💡 Ideas | TBD | Q2 2025 |

---

**Last Updated:** 2025-01-15
**Next Review:** Before Phase 2 deployment
**Owner:** Engineering Team
