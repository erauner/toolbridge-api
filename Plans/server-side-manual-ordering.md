# Server-Side Manual Ordering Support

## Overview

This document outlines what backend changes would be required **if** we decide to add server-aware manual ordering (sortKey) support. Currently, manual ordering is purely a client concern - the backend stores `sortKey` opaquely in `payload_json` without interpretation.

**Status**: Not Required (reference only)
**Related**: Client-side implementation in ToolBridge app (SRE-60 through SRE-64)

---

## Current Architecture (No Changes Needed)

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CLIENT (ToolBridge)                         │
├─────────────────────────────────────────────────────────────────────┤
│  TaskItem.sortKey ──► Drift DB ──► TaskOrderBy.manual               │
│                                                                     │
│  Sorting happens HERE:                                              │
│  • LocalTaskRepository.list(sort: 'manual')                         │
│  • Drift DAO: ORDER BY sort_key ASC NULLS LAST, created_at ASC      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              │ sync (push/pull)
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       SERVER (toolbridge-api)                       │
├─────────────────────────────────────────────────────────────────────┤
│  payload_json JSONB ◄── stores sortKey opaquely                     │
│                                                                     │
│  Server is UNAWARE of sortKey:                                      │
│  • ExtractCommon() only reads: uid, updatedTs, sync.version, etc.   │
│  • All domain fields (title, sortKey, status) pass through as-is   │
│  • REST/Pull ordering: always by (updated_at_ms, uid) for sync      │
└─────────────────────────────────────────────────────────────────────┘
```

**Why this works**: The backend is schema-agnostic by design. New fields like `sortKey` or `isHeader` are stored in `payload_json` and round-tripped without any backend awareness.

---

## When Would Server Changes Be Needed?

| Use Case | Requires Backend? | Complexity |
|----------|-------------------|------------|
| Client sorts tasks by sortKey locally | ❌ No | N/A |
| `GET /v1/tasks?sort=manual` returns server-sorted list | ✅ Yes | Low |
| Server validates sortKey values (e.g., no duplicates) | ✅ Yes | Medium |
| Server rebalances sortKey on collision detection | ✅ Yes | High |
| Server computes "next action" from hierarchy | ✅ Yes | High |

---

## Option 1: Server-Sorted REST Listing (Low Complexity)

**Goal**: Allow `GET /v1/tasks?sort=manual&direction=asc` to return tasks ordered by `sortKey` instead of `updated_at_ms`.

### File Changes

```
internal/
├── httpapi/
│   └── rest_items.go           # Parse sort/direction query params
└── service/
    └── syncservice/
        └── tasks_service.go    # Add sortKey ordering to ListTasks
```

### 1. Parse Query Parameters

**File**: `internal/httpapi/rest_items.go`

```go
// ADD: Helper to parse sort parameter
// ┌──────────────────────────────────────────────────────────────────┐
// │ CHANGE: Add new query param parsing for sort field               │
// │ Impact: Low - additive change, backward compatible               │
// └──────────────────────────────────────────────────────────────────┘
func parseSortParam(r *http.Request) (field string, direction string) {
    field = r.URL.Query().Get("sort")
    direction = r.URL.Query().Get("direction")

    // Validate allowed sort fields
    // ┌──────────────────────────────────────────────────────────────┐
    // │ NOTE: Whitelist approach prevents SQL injection via field    │
    // │ names. Only allow known-safe sort fields.                    │
    // └──────────────────────────────────────────────────────────────┘
    switch field {
    case "manual", "sortKey":
        field = "sortKey"
    case "updatedAt", "updated_at", "":
        field = "updatedAt" // default
    case "createdAt", "created_at":
        field = "createdAt"
    default:
        field = "updatedAt"
    }

    if direction != "desc" {
        direction = "asc"
    }

    return field, direction
}
```

**In `ListTasks` handler**:

```go
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
    // ... existing code ...

    // ┌──────────────────────────────────────────────────────────────┐
    // │ CHANGE: Parse sort parameters from query string              │
    // │ Backward compatible: defaults to updatedAt if not specified  │
    // └──────────────────────────────────────────────────────────────┘
    sortField, sortDirection := parseSortParam(r)

    resp, err := s.TaskSvc.ListTasks(
        ctx,
        userID,
        cursor,
        limit,
        includeDeleted,
        sortField,      // NEW
        sortDirection,  // NEW
    )
    // ...
}
```

### 2. Update Service Layer

**File**: `internal/service/syncservice/tasks_service.go`

```go
// ┌──────────────────────────────────────────────────────────────────┐
// │ CHANGE: Extend ListTasks signature with sort parameters          │
// │ Impact: Breaking change to internal API (not external REST)      │
// │ Migration: Update all call sites (just ListTasks handler)        │
// └──────────────────────────────────────────────────────────────────┘
func (s *TaskService) ListTasks(
    ctx context.Context,
    userID string,
    cursor syncx.Cursor,
    limit int,
    includeDeleted bool,
    sortField string,      // NEW: "sortKey", "updatedAt", "createdAt"
    sortDirection string,  // NEW: "asc" or "desc"
) (*RESTListResponse, error) {
    logger := log.With().Logger()

    // ┌──────────────────────────────────────────────────────────────┐
    // │ CRITICAL: Build ORDER BY clause based on sort field          │
    // │                                                              │
    // │ For sortKey ordering:                                        │
    // │ - Extract from JSONB: payload_json->>'sortKey'               │
    // │ - Cast to numeric for proper ordering                        │
    // │ - Handle NULL values (tasks without sortKey)                 │
    // │ - Fallback to created_at for stable ordering                 │
    // │                                                              │
    // │ IMPORTANT: Cursor pagination still uses (updated_at_ms, uid) │
    // │ to maintain sync invariants. Sort only affects presentation. │
    // └──────────────────────────────────────────────────────────────┘

    var orderClause string
    switch sortField {
    case "sortKey":
        // ┌──────────────────────────────────────────────────────────┐
        // │ JSONB extraction for sortKey ordering                    │
        // │                                                          │
        // │ (payload_json->>'sortKey') IS NULL  → NULLs last         │
        // │ (payload_json->>'sortKey')::float   → numeric sort       │
        // │ created_at                          → tiebreaker         │
        // └──────────────────────────────────────────────────────────┘
        if sortDirection == "desc" {
            orderClause = `
                (payload_json->>'sortKey') IS NULL,
                (payload_json->>'sortKey')::float DESC,
                created_at DESC
            `
        } else {
            orderClause = `
                (payload_json->>'sortKey') IS NULL,
                (payload_json->>'sortKey')::float ASC,
                created_at ASC
            `
        }
    case "createdAt":
        if sortDirection == "desc" {
            orderClause = `created_at DESC, uid DESC`
        } else {
            orderClause = `created_at ASC, uid ASC`
        }
    default: // "updatedAt"
        // ┌──────────────────────────────────────────────────────────┐
        // │ Default: sync-compatible ordering                        │
        // │ This matches the cursor pagination invariant             │
        // └──────────────────────────────────────────────────────────┘
        orderClause = `updated_at_ms, uid`
    }

    // Build query
    query := fmt.Sprintf(`
        SELECT payload_json, deleted_at_ms, updated_at_ms, uid, version
        FROM task
        WHERE owner_id = $1
          AND (updated_at_ms, uid) > ($2, $3::uuid)
          %s
        ORDER BY %s
        LIMIT $4
    `, deletedClause, orderClause)

    // ... rest of implementation unchanged ...
}
```

### 3. Optional: Add JSONB Index

**File**: `migrations/0003_task_sortkey_index.sql` (new file)

```sql
-- ┌──────────────────────────────────────────────────────────────────┐
-- │ OPTIONAL: Expression index on sortKey for faster ordering        │
-- │                                                                  │
-- │ Only add this if:                                                │
-- │ 1. You have significant data volume (>10k tasks per user)        │
-- │ 2. sort=manual queries are frequent                              │
-- │ 3. Query plans show sequential scans on task table               │
-- │                                                                  │
-- │ Trade-off: Index adds write overhead on every task update        │
-- └──────────────────────────────────────────────────────────────────┘

-- Index for sortKey ordering (extract from JSONB, cast to float)
CREATE INDEX CONCURRENTLY IF NOT EXISTS task_sortkey_idx
    ON task (owner_id, ((payload_json->>'sortKey')::float) NULLS LAST);

-- ┌──────────────────────────────────────────────────────────────────┐
-- │ NOTE: CONCURRENTLY allows index creation without blocking writes │
-- │ Run during low-traffic period if table is large                  │
-- └──────────────────────────────────────────────────────────────────┘

COMMENT ON INDEX task_sortkey_idx IS
    'Expression index for server-side manual ordering by sortKey';
```

---

## Option 2: Server-Side Validation (Medium Complexity)

**Goal**: Validate `sortKey` values on push/mutation to ensure data integrity.

### Potential Validations

```go
// ┌──────────────────────────────────────────────────────────────────┐
// │ OPTIONAL: Add to PushTaskItem or ApplyTaskMutation               │
// │                                                                  │
// │ Considerations:                                                  │
// │ - These validations add latency to every write                   │
// │ - Client already manages sortKey correctly                       │
// │ - Only implement if you see data integrity issues                │
// └──────────────────────────────────────────────────────────────────┘

func validateSortKey(payload map[string]any) error {
    sortKey, hasSortKey := payload["sortKey"]
    if !hasSortKey {
        return nil // sortKey is optional
    }

    // ┌──────────────────────────────────────────────────────────────┐
    // │ Validation 1: Type check                                     │
    // │ sortKey must be numeric (float64 from JSON)                  │
    // └──────────────────────────────────────────────────────────────┘
    sk, ok := sortKey.(float64)
    if !ok {
        return fmt.Errorf("sortKey must be numeric, got %T", sortKey)
    }

    // ┌──────────────────────────────────────────────────────────────┐
    // │ Validation 2: Range check (optional)                         │
    // │ Prevent extreme values that could cause precision issues     │
    // └──────────────────────────────────────────────────────────────┘
    if sk < -1e15 || sk > 1e15 {
        return fmt.Errorf("sortKey out of safe range: %f", sk)
    }

    // ┌──────────────────────────────────────────────────────────────┐
    // │ Validation 3: NaN/Inf check                                  │
    // │ JSON technically allows these but they break ordering        │
    // └──────────────────────────────────────────────────────────────┘
    if math.IsNaN(sk) || math.IsInf(sk, 0) {
        return fmt.Errorf("sortKey cannot be NaN or Infinity")
    }

    return nil
}
```

---

## Option 3: Server-Side Hierarchy/Next-Action (High Complexity)

**Goal**: Server computes hierarchical relationships or "next action" logic.

### Why This Is Complex

```
┌──────────────────────────────────────────────────────────────────────┐
│ SERVER-SIDE HIERARCHY CONSIDERATIONS                                 │
│                                                                      │
│ 1. Requires understanding parentUid relationships                    │
│    - Need to traverse parent→child chains                            │
│    - Must handle orphaned tasks (deleted parents)                    │
│                                                                      │
│ 2. "Next action" requires understanding:                             │
│    - isHeader field (headers are not actionable)                     │
│    - status/done state                                               │
│    - sortKey ordering within siblings                                │
│                                                                      │
│ 3. Performance implications:                                         │
│    - Can't use simple SQL for recursive hierarchy                    │
│    - May need CTEs or application-level tree building                │
│    - Caching becomes important                                       │
│                                                                      │
│ 4. Sync complexity:                                                  │
│    - Computed "next action" could become stale                       │
│    - Would need to invalidate on any subtask change                  │
│                                                                      │
│ RECOMMENDATION: Keep hierarchy logic client-side (TaskHierarchy)     │
│ unless you have a specific server-side use case (e.g., API for       │
│ third-party integrations that need next-action data).                │
└──────────────────────────────────────────────────────────────────────┘
```

### If You Really Need It

**New file**: `internal/service/syncservice/task_hierarchy.go`

```go
// ┌──────────────────────────────────────────────────────────────────┐
// │ TaskHierarchy: Server-side tree utilities                        │
// │                                                                  │
// │ CAUTION: This adds significant complexity. Only implement if:    │
// │ 1. External API consumers need hierarchy data                    │
// │ 2. Server-side notifications need "next action" info             │
// │ 3. Reporting/analytics require hierarchy aggregation             │
// └──────────────────────────────────────────────────────────────────┘

type TaskNode struct {
    UID       string
    ParentUID *string
    SortKey   *float64
    IsHeader  bool
    IsDone    bool
    Children  []*TaskNode
}

// BuildHierarchy constructs a tree from flat task list
// ┌──────────────────────────────────────────────────────────────────┐
// │ WARNING: O(n) memory, O(n log n) for sorting                     │
// │ For large task sets (>1000), consider pagination or caching      │
// └──────────────────────────────────────────────────────────────────┘
func BuildHierarchy(tasks []map[string]any) []*TaskNode {
    // ... implementation similar to client-side TaskHierarchy ...
}

// FirstActionableDescendant finds the first non-done, non-header task
// ┌──────────────────────────────────────────────────────────────────┐
// │ Traversal order: depth-first, sorted by sortKey at each level   │
// │ Returns nil if no actionable descendant exists                   │
// └──────────────────────────────────────────────────────────────────┘
func (n *TaskNode) FirstActionableDescendant() *TaskNode {
    for _, child := range n.Children {
        if !child.IsHeader && !child.IsDone {
            return child
        }
        if desc := child.FirstActionableDescendant(); desc != nil {
            return desc
        }
    }
    return nil
}
```

---

## Decision Matrix

| Approach | Effort | Risk | Recommendation |
|----------|--------|------|----------------|
| **Do nothing** (current) | None | None | ✅ **Recommended** |
| Option 1: REST sort param | 2-4 hours | Low | Consider if API consumers need it |
| Option 2: Validation | 1-2 hours | Low | Only if data integrity issues occur |
| Option 3: Server hierarchy | 1-2 days | Medium | Avoid unless external API requires it |

---

## Summary

**Current state**: Backend is schema-agnostic. `sortKey` and `isHeader` are stored in `payload_json` and round-tripped without server awareness. All ordering happens client-side in Drift.

**Recommendation**: Keep it this way. The architecture intentionally separates concerns:
- **Client**: Schema owner, ordering logic, hierarchy utilities
- **Server**: Sync protocol, LWW conflict resolution, opaque payload storage

Only implement server-side ordering if you have a concrete use case (e.g., REST API consumers who can't sort client-side, or third-party integrations needing computed hierarchy data).
