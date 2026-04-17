ALTER TABLE users
  ADD COLUMN IF NOT EXISTS primary_selection_sponsored_used BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_users_primary_selection_sponsored_used ON users (primary_selection_sponsored_used);
