-- Comments table for delta sync
-- Comments can belong to either a note or a task (polymorphic parent relationship)

CREATE TABLE comment (
  uid            UUID NOT NULL,
  owner_id       UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  updated_at_ms  BIGINT NOT NULL,            -- Unix milliseconds for cursor-based pagination
  deleted_at_ms  BIGINT,                     -- NULL = alive, non-NULL = tombstone
  version        INT NOT NULL DEFAULT 1,     -- Server-controlled version for conflict detection
  payload_json   JSONB NOT NULL,             -- Original client JSON (preserved as-is)
  parent_type    TEXT NOT NULL CHECK (parent_type IN ('note', 'task')),  -- Parent entity type
  parent_uid     UUID NOT NULL,              -- Parent entity UID
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_id, uid)                -- Composite key for tenant isolation
);

-- Indexes for efficient delta sync queries
CREATE INDEX comment_owner_updated_idx ON comment (owner_id, updated_at_ms);
CREATE INDEX comment_owner_deleted_idx ON comment (owner_id, deleted_at_ms) WHERE deleted_at_ms IS NOT NULL;

-- Composite index for cursor-based pagination (updated_at_ms, uid)
CREATE INDEX comment_cursor_idx ON comment (updated_at_ms, uid);

-- Index for querying comments by parent (e.g., "get all comments for note X")
CREATE INDEX comment_parent_idx ON comment (owner_id, parent_type, parent_uid);

COMMENT ON TABLE comment IS 'Comments with delta sync support - uses LWW conflict resolution. Can belong to notes or tasks.';
COMMENT ON COLUMN comment.updated_at_ms IS 'Unix milliseconds timestamp for cursor pagination and LWW conflict resolution';
COMMENT ON COLUMN comment.deleted_at_ms IS 'Tombstone timestamp - NULL means active record';
COMMENT ON COLUMN comment.version IS 'Server-controlled version number - increments on each update';
COMMENT ON COLUMN comment.payload_json IS 'Full client JSON preserved as-is - allows flexible schema evolution';
COMMENT ON COLUMN comment.parent_type IS 'Type of parent entity: "note" or "task"';
COMMENT ON COLUMN comment.parent_uid IS 'UID of parent entity - parent existence validated at application level';
