ALTER TABLE tokens DROP COLUMN IF EXISTS token_hash;
DROP INDEX IF EXISTS idx_tokens_token_hash;
ALTER TABLE event_subscriptions DROP COLUMN IF EXISTS encrypted_secret;
