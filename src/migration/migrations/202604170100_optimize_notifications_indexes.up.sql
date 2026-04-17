CREATE INDEX IF NOT EXISTS idx_notifications_user_created_at_id
  ON notifications (user_address, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_user_unread_created_at_id
  ON notifications (user_address, created_at DESC, id DESC)
  WHERE is_read = FALSE;
