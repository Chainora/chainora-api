ALTER TABLE users
  ADD COLUMN IF NOT EXISTS gas_sponsored BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS is_hardware_verified BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_users_gas_sponsored ON users (gas_sponsored);
CREATE INDEX IF NOT EXISTS idx_users_is_hardware_verified ON users (is_hardware_verified);