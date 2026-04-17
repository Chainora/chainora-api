ALTER TABLE users
  ALTER COLUMN username SET DEFAULT '';

UPDATE users
SET username = ''
WHERE LOWER(TRIM(COALESCE(username, ''))) = 'chainora user';
