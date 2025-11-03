-- Chats table for delta sync
-- Identical structure to notes table - chats are conversation containers

CREATE TABLE chat (
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
CREATE INDEX chat_owner_updated_idx ON chat (owner_id, updated_at_ms);
CREATE INDEX chat_owner_deleted_idx ON chat (owner_id, deleted_at_ms) WHERE deleted_at_ms IS NOT NULL;

-- Composite index for cursor-based pagination (updated_at_ms, uid)
CREATE INDEX chat_cursor_idx ON chat (updated_at_ms, uid);

COMMENT ON TABLE chat IS 'Chats with delta sync support - uses LWW conflict resolution';
COMMENT ON COLUMN chat.updated_at_ms IS 'Unix milliseconds timestamp for cursor pagination and LWW conflict resolution';
COMMENT ON COLUMN chat.deleted_at_ms IS 'Tombstone timestamp - NULL means active record';
COMMENT ON COLUMN chat.version IS 'Server-controlled version number - increments on each update';
COMMENT ON COLUMN chat.payload_json IS 'Full client JSON preserved as-is - allows flexible schema evolution';
