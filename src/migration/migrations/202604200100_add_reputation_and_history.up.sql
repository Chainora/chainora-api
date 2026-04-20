ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS min_reputation NUMERIC(78, 0) NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_groups_min_reputation ON groups (min_reputation);

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS reputation_score BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_users_reputation_score ON users (reputation_score);

CREATE TABLE IF NOT EXISTS group_period_member_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pool_id NUMERIC(78, 0) NOT NULL,
  cycle_id NUMERIC(78, 0) NOT NULL,
  period_id NUMERIC(78, 0) NOT NULL,
  member_address TEXT NOT NULL,
  contributed BOOLEAN NOT NULL DEFAULT FALSE,
  bid_amount NUMERIC(78, 0) NOT NULL DEFAULT 0,
  claimed BOOLEAN NOT NULL DEFAULT FALSE,
  claim_amount NUMERIC(78, 0) NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (pool_id, cycle_id, period_id, member_address)
);

CREATE INDEX IF NOT EXISTS idx_group_period_member_history_pool_cycle_period
  ON group_period_member_history (pool_id, cycle_id, period_id);

CREATE INDEX IF NOT EXISTS idx_group_period_member_history_member
  ON group_period_member_history (member_address);

CREATE TABLE IF NOT EXISTS user_reputation_ledger (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_address TEXT NOT NULL,
  pool_id NUMERIC(78, 0) NOT NULL,
  cycle_id NUMERIC(78, 0) NOT NULL,
  reason TEXT NOT NULL,
  points BIGINT NOT NULL,
  triggered_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_address, pool_id, cycle_id, reason)
);

CREATE INDEX IF NOT EXISTS idx_user_reputation_ledger_user_address
  ON user_reputation_ledger (user_address);

CREATE INDEX IF NOT EXISTS idx_user_reputation_ledger_pool_cycle
  ON user_reputation_ledger (pool_id, cycle_id);
