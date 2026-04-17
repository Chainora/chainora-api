ALTER TABLE users
  ADD COLUMN IF NOT EXISTS username_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_users_username_count ON users (username_count);
