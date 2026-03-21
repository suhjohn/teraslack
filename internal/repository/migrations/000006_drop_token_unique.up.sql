-- Drop the UNIQUE constraint on tokens.token column.
-- Lookups now use token_hash (added in migration 000004).
-- The raw token is redacted in event_data, so during projection rebuilds
-- multiple tokens would have token="" which violates the old UNIQUE constraint.
ALTER TABLE tokens DROP CONSTRAINT IF EXISTS tokens_token_key;

-- Also drop any unique index that may exist on the token column.
DROP INDEX IF EXISTS tokens_token_key;
