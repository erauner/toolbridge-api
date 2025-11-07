-- Owner state table for epoch-based reset coordination
CREATE TABLE IF NOT EXISTS owner_state (
  owner_id TEXT PRIMARY KEY,
  epoch INT NOT NULL DEFAULT 1,
  last_wipe_at TIMESTAMPTZ,
  last_wipe_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for fast lookup by owner
CREATE INDEX IF NOT EXISTS idx_owner_state_owner_id ON owner_state(owner_id);

-- Trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_owner_state_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER owner_state_updated_at
  BEFORE UPDATE ON owner_state
  FOR EACH ROW
  EXECUTE FUNCTION update_owner_state_updated_at();
