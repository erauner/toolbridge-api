# Neon Migration: Tenant Identity Contract

## Overview

This document defines the **tenant identity contract** that must be preserved when migrating from row-level multi-tenancy to Neon's DB-per-tenant architecture.

**Status**: Planning (not yet implemented)
**Related**: `Plans/redis-distributed-state.md` (session storage prerequisite)

---

## Core Principle

> **The tenant identifier is a logical, IdP-level organization ID - NOT a database artifact.**

This separation of concerns is critical:

- **Tenant Identity (Layer 1)**: Who is this request for? ← Current implementation
- **Data Storage (Layer 2)**: Where do we store their data? ← Future Neon migration

Layer 1 (what we have now) must remain stable regardless of Layer 2 implementation changes.

---

## Current Implementation

### Tenant Identifier Source

**IdP**: WorkOS AuthKit (supports other OIDC providers: Okta, Auth0, Keycloak, etc.)
**Claim Key**: `organization_id` (configurable via `TENANT_CLAIM` env var)
**Example Value**: `org_2a1b3c4d5e6f7g8h` (WorkOS organization ID)

### Tenant Resolution Flow

```
1. OIDC Sign-In
   └─> ID token contains: { "organization_id": "org_abc123", ... }

2. Flutter Client (TenantResolver)
   └─> Extracts organization_id from ID token claims
   └─> Persists to TenantUserProfile (SharedPreferences)

3. API Request
   └─> Authorization: Bearer <JWT with organization_id claim>
   └─> X-TB-Tenant-ID: org_abc123
   └─> X-TB-Timestamp: 1234567890
   └─> X-TB-Signature: <HMAC-SHA256 of tenant_id:timestamp>

4. Go API Middleware (Precedence Order)
   a) HMAC tenant headers validated → TenantID(ctx) = "org_abc123"
   b) If no HMAC headers, extract from JWT claim → TenantID(ctx) = "org_abc123"
   c) If neither, TenantID(ctx) = "" (no tenant context)

5. Data Layer (Today)
   └─> SELECT * FROM notes WHERE tenant_id = $1  -- Uses TenantID(ctx)
```

### Critical Code Locations

**Go Backend:**
- `internal/auth/tenant_headers.go:143-148` - `TenantID(ctx)` accessor (DO NOT CHANGE signature)
- `internal/auth/tenant_headers.go:128` - Stores tenant in context
- `internal/auth/jwt.go:155-165` - Extracts tenant from JWT claims (fallback)
- `cmd/server/main.go:95` - Wires `TENANT_CLAIM` env var into JWTCfg

**Flutter Client:**
- `lib/services/tenant_resolver.dart` - Extracts `organization_id` from OIDC
- `lib/state/auth/tenant_user_profile.dart` - Persists tenant-user mapping
- `lib/utils/tenant_header_signer.dart` - Signs HMAC tenant headers

**Configuration:**
- `k8s/configmap.yaml` - `TENANT_CLAIM: "organization_id"`
- `chart/values.yaml` - `api.jwt.tenantClaim: "organization_id"`

---

## Future: Neon DB-Per-Tenant Migration

### What Changes

**Add (not replace) these components:**

1. **Tenant Registry** (metadata database):
   ```sql
   CREATE TABLE tenant_registry (
     organization_id TEXT PRIMARY KEY,  -- SAME as TenantID(ctx)!
     neon_project_id TEXT NOT NULL,
     neon_branch_id TEXT NOT NULL,
     connection_string TEXT NOT NULL,  -- Encrypted
     created_at TIMESTAMPTZ DEFAULT NOW(),
     status TEXT DEFAULT 'active',
     CONSTRAINT valid_status CHECK (status IN ('active', 'suspended', 'migrating'))
   );
   ```

2. **Pool Manager** (new package):
   ```go
   package tenantdb

   type PoolManager struct {
     registry   *RegistryDB          // Queries tenant_registry table
     pools      map[string]*pgxpool.Pool  // Keyed by organization_id
     poolsMux   sync.RWMutex
   }

   // GetTenantPool returns (or lazy-creates) the pgxpool.Pool for a tenant's Neon branch
   func (pm *PoolManager) GetTenantPool(ctx context.Context, organizationID string) (*pgxpool.Pool, error) {
     // 1. Lock and check cache
     // 2. If miss, query tenant_registry for connection_string
     // 3. Create new pgxpool.Pool with Neon connection string
     // 4. Cache and return
   }
   ```

3. **Data Layer Swap**:
   ```go
   // OLD (row-level multi-tenancy):
   tenantID := auth.TenantID(ctx)
   srv.DB.Exec(ctx, "SELECT * FROM notes WHERE tenant_id = $1", tenantID)

   // NEW (Neon DB-per-tenant):
   tenantID := auth.TenantID(ctx)  // SAME accessor!
   pool, err := srv.PoolMgr.GetTenantPool(ctx, tenantID)
   if err != nil { return err }
   pool.Exec(ctx, "SELECT * FROM notes")  // No tenant_id column needed
   ```

### What Stays the Same

✅ **Tenant Identity Resolution** - Still use `organization_id` from OIDC
✅ **TenantID(ctx) Accessor** - API signature unchanged
✅ **HMAC Tenant Headers** - Still validated and trusted
✅ **JWT Claims Fallback** - Still extract tenant from claims
✅ **Precedence Rules** - HMAC > JWT > none
✅ **Flutter TenantResolver** - Still resolves same `organization_id`
✅ **Configuration** - `TENANT_CLAIM` still points to same claim key

---

## Migration Strategy

### Phase 1: Preparation (Can do now)
- ✅ Implement OIDC-derived tenant resolution (DONE - current PR)
- ⏳ Migrate to Redis for session/rate-limit storage (See `Plans/redis-distributed-state.md`)
- ⏳ Create `tenant_registry` table in metadata DB
- ⏳ Backfill registry with current tenants (from existing `tenant_id` column usage)

### Phase 2: Pilot (Single Tenant)
- Create Neon project + branch for one tenant
- Add registry entry: `organization_id → neon_branch_id + connection_string`
- Implement `PoolManager` with fallback logic:
  ```go
  if pool := poolMgr.GetTenantPool(ctx, tenantID); pool != nil {
    // Use Neon branch
  } else {
    // Fall back to shared DB with tenant_id column
  }
  ```
- Migrate pilot tenant's data to Neon branch
- Monitor for 1-2 weeks

### Phase 3: Gradual Rollout
- Migrate tenants in batches (sorted by data size, smallest first)
- Update registry as each tenant migrates
- Keep shared DB running for unmigrated tenants
- RLS policies remain as defense-in-depth even in Neon

### Phase 4: Complete Migration
- All tenants on Neon branches
- Shared DB becomes metadata-only (tenant_registry, migrations, etc.)
- Remove `tenant_id` columns from tables (optional - can keep for extra safety)

---

## Contract Requirements

### For All DB/API Code Changes

When modifying tenant-related code between now and Neon migration:

1. **DO NOT change `TenantID(ctx)` to return database-specific identifiers**
   - ❌ Bad: `TenantID(ctx)` returns `neon_branch_id`
   - ✅ Good: `TenantID(ctx)` returns `organization_id` (from OIDC)
   - Registry should map: `organization_id → neon_branch_id`

2. **DO NOT couple tenant identity to database schema**
   - ❌ Bad: `tenantID := "schema_" + orgID`
   - ✅ Good: `tenantID := orgID` (registry handles schema mapping)

3. **DO NOT assume single global `srv.DB` will always exist**
   - ❌ Bad: `srv.DB.Exec(ctx, query)`  ← Hardcoded global pool
   - ✅ Good: `pool := srv.getPool(ctx); pool.Exec(ctx, query)`  ← Abstraction

4. **DO preserve `TenantID(ctx)` as the single source of truth**
   - ✅ Always use `auth.TenantID(ctx)` to get tenant identifier
   - ✅ Never parse tenant from JWT multiple times in same request
   - ✅ Never store tenant in other context keys

5. **DO keep HMAC tenant header validation strict**
   - The headers remain useful for trusted server-to-server (MCP, webhooks, etc.)
   - Even with Neon, we still need to know WHICH tenant's database to query

---

## Testing Neon Compatibility

Before merging DB-related changes, verify:

```go
// Can this code work if TenantID(ctx) routes to different pools?
tenantID := auth.TenantID(ctx)

// Bad: Assumes shared DB with tenant_id column
db.Exec(ctx, "UPDATE config SET value = $1 WHERE tenant_id = $2", val, tenantID)
// Problem: Neon mode has no tenant_id column, and "config" might be per-tenant DB

// Good: Uses accessor that will route to correct pool
pool := srv.getPoolForTenant(ctx, tenantID)  // Future: returns Neon pool
pool.Exec(ctx, "UPDATE config SET value = $1", val)
```

---

## References

- **Current PR**: `feat/oidc-tenant-claims` (Go backend + Flutter client)
- **OIDC Config**: `chart/values.yaml` - `api.jwt.tenantClaim`
- **Tenant Headers Spec**: `docs/SPEC-FASTMCP-INTEGRATION.md`
- **Neon Docs**: https://neon.tech/docs/guides/branching
- **Multi-tenant Patterns**: https://neon.tech/docs/guides/multi-tenant

---

## Questions?

If you're unsure whether a change affects tenant identity:

1. Does it change what `TenantID(ctx)` returns?
2. Does it add a new way to identify tenants (beyond OIDC `organization_id`)?
3. Does it assume a specific database topology (shared vs. isolated)?

If YES to any: Review against this contract before merging.
