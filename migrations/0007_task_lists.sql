-- Task Lists and Categories for Phase 3 project containers
-- SRE-28: Task Lists as project containers for grouping tasks
--
-- Follows same pattern as Notes/Tasks with composite PK for tenant isolation
-- and payload_json for flexible schemas supporting sync metadata.

-- ============================================================================
-- Task List Categories (hierarchical organization)
-- ============================================================================
CREATE TABLE task_list_category (
  uid            UUID NOT NULL,
  owner_id       UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  updated_at_ms  BIGINT NOT NULL,            -- Unix milliseconds for cursor-based pagination
  deleted_at_ms  BIGINT,                     -- NULL = alive, non-NULL = tombstone
  version        INT NOT NULL DEFAULT 1,     -- Server-controlled version for conflict detection
  payload_json   JSONB NOT NULL,             -- Original client JSON (preserved as-is)
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_id, uid)                -- Composite key for tenant isolation
);

-- Indexes for efficient delta sync queries
CREATE INDEX task_list_category_owner_updated_idx ON task_list_category (owner_id, updated_at_ms);
CREATE INDEX task_list_category_owner_deleted_idx ON task_list_category (owner_id, deleted_at_ms) WHERE deleted_at_ms IS NOT NULL;
CREATE INDEX task_list_category_cursor_idx ON task_list_category (updated_at_ms, uid);

-- Expression index for hierarchical queries (parentUid from JSONB)
CREATE INDEX task_list_category_parent_idx ON task_list_category ((payload_json->>'parentUid'))
    WHERE payload_json->>'parentUid' IS NOT NULL;

COMMENT ON TABLE task_list_category IS 'Task list categories for hierarchical organization - uses LWW conflict resolution';
COMMENT ON COLUMN task_list_category.payload_json IS 'Client JSON with fields: uid, name, parentUid, order, createdAt, updatedAt, sync';

-- ============================================================================
-- Task Lists (project containers)
-- ============================================================================
CREATE TABLE task_list (
  uid            UUID NOT NULL,
  owner_id       UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  updated_at_ms  BIGINT NOT NULL,            -- Unix milliseconds for cursor-based pagination
  deleted_at_ms  BIGINT,                     -- NULL = alive, non-NULL = tombstone
  version        INT NOT NULL DEFAULT 1,     -- Server-controlled version for conflict detection
  payload_json   JSONB NOT NULL,             -- Original client JSON (preserved as-is)
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_id, uid)                -- Composite key for tenant isolation
);

-- Indexes for efficient delta sync queries
CREATE INDEX task_list_owner_updated_idx ON task_list (owner_id, updated_at_ms);
CREATE INDEX task_list_owner_deleted_idx ON task_list (owner_id, deleted_at_ms) WHERE deleted_at_ms IS NOT NULL;
CREATE INDEX task_list_cursor_idx ON task_list (updated_at_ms, uid);

-- Expression index for filtering by category
CREATE INDEX task_list_category_uid_idx ON task_list ((payload_json->>'categoryUid'))
    WHERE payload_json->>'categoryUid' IS NOT NULL;

COMMENT ON TABLE task_list IS 'Task lists (project containers) with delta sync support - uses LWW conflict resolution';
COMMENT ON COLUMN task_list.payload_json IS 'Client JSON with fields: uid, title, description, categoryUid, color, icon, createdAt, updatedAt, sync';

-- ============================================================================
-- Task → TaskList linkage (expression index on existing task table)
-- ============================================================================

-- Expression index on taskListUid extracted from task JSONB payload
-- This enables efficient queries like: WHERE payload_json->>'taskListUid' = 'some-uid'
CREATE INDEX CONCURRENTLY IF NOT EXISTS task_task_list_uid_idx 
    ON task ((payload_json->>'taskListUid'))
    WHERE payload_json->>'taskListUid' IS NOT NULL;

-- Expression index for standalone tasks (no task list)
CREATE INDEX CONCURRENTLY IF NOT EXISTS task_standalone_idx
    ON task ((payload_json->>'taskListUid'))
    WHERE payload_json->>'taskListUid' IS NULL;

COMMENT ON INDEX task_task_list_uid_idx IS 'Expression index for filtering tasks by taskListUid from JSONB payload';
COMMENT ON INDEX task_standalone_idx IS 'Expression index for filtering standalone tasks (no task list) from JSONB payload';
