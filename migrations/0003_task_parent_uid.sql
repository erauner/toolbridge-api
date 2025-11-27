-- Task parent_uid support for nested subtasks
-- Phase 2: SRE-24 - Nested subtasks for tasks
--
-- The parentUid field is stored in payload_json (JSONB) for sync compatibility.
-- This migration adds an optional expression index for future server-side filtering.

-- Expression index on parentUid extracted from JSONB payload
-- This enables efficient queries like: WHERE payload_json->>'parentUid' = 'some-uid'
CREATE INDEX CONCURRENTLY IF NOT EXISTS task_parent_uid_idx 
    ON task ((payload_json->>'parentUid'))
    WHERE payload_json->>'parentUid' IS NOT NULL;

-- Expression index for root tasks (no parent)
-- This enables efficient queries for top-level tasks only
CREATE INDEX CONCURRENTLY IF NOT EXISTS task_root_tasks_idx
    ON task ((payload_json->>'parentUid'))
    WHERE payload_json->>'parentUid' IS NULL;

COMMENT ON INDEX task_parent_uid_idx IS 'Expression index for filtering tasks by parentUid from JSONB payload';
COMMENT ON INDEX task_root_tasks_idx IS 'Expression index for filtering root tasks (no parent) from JSONB payload';
