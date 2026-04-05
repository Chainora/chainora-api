CREATE TABLE IF NOT EXISTS users (
  address TEXT PRIMARY KEY,
  username TEXT NOT NULL DEFAULT 'Chainora User',
  tcnr NUMERIC(38, 0) NOT NULL DEFAULT 0,
  kyc_status TEXT NOT NULL DEFAULT 'unavailable',
  public_key TEXT NOT NULL DEFAULT '',
  last_login TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users (username);
