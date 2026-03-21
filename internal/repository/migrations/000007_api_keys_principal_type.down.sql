DROP TRIGGER IF EXISTS trg_api_keys_updated_at ON api_keys;
DROP TABLE IF EXISTS api_keys;
ALTER TABLE users DROP COLUMN IF EXISTS owner_id;
ALTER TABLE users DROP COLUMN IF EXISTS principal_type;
