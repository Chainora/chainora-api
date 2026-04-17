ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS public_recruitment BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_groups_public_recruitment ON groups (public_recruitment);
