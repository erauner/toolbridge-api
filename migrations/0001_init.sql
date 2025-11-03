-- Initial schema for ToolBridge sync API
-- Creates app_user table for tenant isolation and note table for delta sync

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- App users (mapped from JWT sub claims)
CREATE TABLE app_user (
  id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  sub        TEXT UNIQUE NOT NULL,          -- JWT subject (user identifier)
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Notes table
-- Uses typed sync columns + payload_json for fast delta sync with flexible schema
CREATE TABLE note (
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
CREATE INDEX note_owner_updated_idx ON note (owner_id, updated_at_ms);
CREATE INDEX note_owner_deleted_idx ON note (owner_id, deleted_at_ms) WHERE deleted_at_ms IS NOT NULL;
-- note_owner_uid_idx is now redundant (covered by PRIMARY KEY (owner_id, uid))

-- Composite index for cursor-based pagination (updated_at_ms, uid)
CREATE INDEX note_cursor_idx ON note (updated_at_ms, uid);

COMMENT ON TABLE note IS 'Notes with delta sync support - uses LWW conflict resolution';
COMMENT ON COLUMN note.updated_at_ms IS 'Unix milliseconds timestamp for cursor pagination and LWW conflict resolution';
COMMENT ON COLUMN note.deleted_at_ms IS 'Tombstone timestamp - NULL means active record';
COMMENT ON COLUMN note.version IS 'Server-controlled version number - increments on each update';
COMMENT ON COLUMN note.payload_json IS 'Full client JSON preserved as-is - allows flexible schema evolution';
