CREATE TABLE IF NOT EXISTS groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pool_id NUMERIC(78, 0) NOT NULL,
  pool_address TEXT NOT NULL,
  creator_address TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  contribution_amount NUMERIC(78, 0) NOT NULL,
  target_members INTEGER NOT NULL,
  period_duration INTEGER NOT NULL,
  contribution_window INTEGER NOT NULL,
  auction_window INTEGER NOT NULL,
  status SMALLINT NOT NULL DEFAULT 0,
  current_cycle NUMERIC(78, 0) NOT NULL DEFAULT 0,
  current_period NUMERIC(78, 0) NOT NULL DEFAULT 0,
  active_member_count INTEGER NOT NULL DEFAULT 0,
  cycle_completed BOOLEAN NOT NULL DEFAULT FALSE,
  tx_hash TEXT NOT NULL DEFAULT '',
  last_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_pool_address_unique ON groups (pool_address);
CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_pool_id_unique ON groups (pool_id);
CREATE INDEX IF NOT EXISTS idx_groups_creator_address ON groups (creator_address);
CREATE INDEX IF NOT EXISTS idx_groups_status ON groups (status);
CREATE INDEX IF NOT EXISTS idx_groups_last_synced_at ON groups (last_synced_at);
