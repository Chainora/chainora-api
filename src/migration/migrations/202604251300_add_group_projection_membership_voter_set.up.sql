CREATE TABLE IF NOT EXISTS group_projection_membership_voter_set (
  pool_id NUMERIC(78, 0) NOT NULL,
  vote_mode TEXT NOT NULL,
  proposal_id NUMERIC(78, 0) NOT NULL,
  voter_address TEXT NOT NULL,
  source_block_number NUMERIC(78, 0) NOT NULL DEFAULT 0,
  tx_hash TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (pool_id, vote_mode, proposal_id, voter_address)
);

CREATE INDEX IF NOT EXISTS idx_group_projection_membership_voter_set_lookup
  ON group_projection_membership_voter_set (pool_id, vote_mode, proposal_id, LOWER(voter_address));

CREATE INDEX IF NOT EXISTS idx_group_projection_membership_voter_set_pool
  ON group_projection_membership_voter_set (pool_id, vote_mode, proposal_id);
