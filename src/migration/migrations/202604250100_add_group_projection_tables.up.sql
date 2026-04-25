CREATE TABLE IF NOT EXISTS group_projection_meta (
  pool_id NUMERIC(78, 0) PRIMARY KEY,
  pool_address TEXT NOT NULL UNIQUE,
  last_indexed_block NUMERIC(78, 0) NOT NULL DEFAULT 0,
  last_indexed_at TIMESTAMPTZ,
  last_indexed_tx_hash TEXT NOT NULL DEFAULT '',
  projection_version INTEGER NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_group_projection_meta_pool_address
  ON group_projection_meta (LOWER(pool_address));

CREATE TABLE IF NOT EXISTS group_projection_members (
  pool_id NUMERIC(78, 0) NOT NULL,
  pool_address TEXT NOT NULL,
  member_address TEXT NOT NULL,
  is_active_member BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (pool_id, member_address)
);

CREATE INDEX IF NOT EXISTS idx_group_projection_members_member
  ON group_projection_members (LOWER(member_address), is_active_member);

CREATE INDEX IF NOT EXISTS idx_group_projection_members_pool
  ON group_projection_members (pool_id, is_active_member);

CREATE TABLE IF NOT EXISTS group_projection_membership_proposals (
  pool_id NUMERIC(78, 0) NOT NULL,
  pool_address TEXT NOT NULL,
  vote_mode TEXT NOT NULL,
  proposal_id NUMERIC(78, 0) NOT NULL,
  candidate_address TEXT NOT NULL,
  yes_votes BIGINT NOT NULL DEFAULT 0,
  no_votes BIGINT NOT NULL DEFAULT 0,
  open BOOLEAN NOT NULL DEFAULT FALSE,
  tx_hash TEXT NOT NULL DEFAULT '',
  block_number NUMERIC(78, 0) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (pool_id, vote_mode, proposal_id)
);

CREATE INDEX IF NOT EXISTS idx_group_projection_membership_proposals_pool
  ON group_projection_membership_proposals (pool_id, vote_mode, open);

CREATE TABLE IF NOT EXISTS group_projection_membership_votes (
  pool_id NUMERIC(78, 0) NOT NULL,
  vote_mode TEXT NOT NULL,
  proposal_id NUMERIC(78, 0) NOT NULL,
  voter_address TEXT NOT NULL,
  support BOOLEAN NOT NULL,
  tx_hash TEXT NOT NULL DEFAULT '',
  block_number NUMERIC(78, 0) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (pool_id, vote_mode, proposal_id, voter_address)
);

CREATE INDEX IF NOT EXISTS idx_group_projection_membership_votes_lookup
  ON group_projection_membership_votes (pool_id, vote_mode, proposal_id, LOWER(voter_address));

CREATE TABLE IF NOT EXISTS group_projection_member_bids (
  pool_id NUMERIC(78, 0) NOT NULL,
  cycle_id NUMERIC(78, 0) NOT NULL,
  period_id NUMERIC(78, 0) NOT NULL,
  bidder_address TEXT NOT NULL,
  discount_amount NUMERIC(78, 0) NOT NULL DEFAULT 0,
  tx_hash TEXT NOT NULL DEFAULT '',
  block_number NUMERIC(78, 0) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (pool_id, cycle_id, period_id, bidder_address)
);

CREATE INDEX IF NOT EXISTS idx_group_projection_member_bids_pool_cycle_period
  ON group_projection_member_bids (pool_id, cycle_id, period_id);

CREATE TABLE IF NOT EXISTS group_projection_extension_votes (
  pool_id NUMERIC(78, 0) NOT NULL,
  cycle_id NUMERIC(78, 0) NOT NULL,
  period_id NUMERIC(78, 0) NOT NULL,
  voter_address TEXT NOT NULL,
  support BOOLEAN NOT NULL,
  tx_hash TEXT NOT NULL DEFAULT '',
  block_number NUMERIC(78, 0) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (pool_id, cycle_id, period_id, voter_address)
);

CREATE INDEX IF NOT EXISTS idx_group_projection_extension_votes_pool_cycle_period
  ON group_projection_extension_votes (pool_id, cycle_id, period_id);
