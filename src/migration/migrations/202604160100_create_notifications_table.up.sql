CREATE TABLE IF NOT EXISTS notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_address TEXT NOT NULL,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  group_id NUMERIC(78, 0),
  action_url TEXT NOT NULL DEFAULT '',
  is_read BOOLEAN NOT NULL DEFAULT FALSE,
  external_ref TEXT NOT NULL DEFAULT '',
  read_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_created_at
  ON notifications (user_address, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_user_is_read
  ON notifications (user_address, is_read);

CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_external_ref_unique
  ON notifications (external_ref)
  WHERE external_ref <> '';

CREATE TABLE IF NOT EXISTS notification_cursors (
  cursor_key TEXT PRIMARY KEY,
  last_block NUMERIC(78, 0) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
