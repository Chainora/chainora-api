WITH ranked_notifications AS (
  SELECT
    ctid,
    ROW_NUMBER() OVER (
      PARTITION BY external_ref
      ORDER BY created_at DESC, id DESC
    ) AS rank_order
  FROM notifications
  WHERE external_ref <> ''
)
DELETE FROM notifications target
USING ranked_notifications ranked
WHERE target.ctid = ranked.ctid
  AND ranked.rank_order > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_external_ref_unique
  ON notifications (external_ref)
  WHERE external_ref <> '';
