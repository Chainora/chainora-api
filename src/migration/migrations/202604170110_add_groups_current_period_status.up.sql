ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS current_period_status SMALLINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_groups_current_period_status
  ON groups (current_period_status);
