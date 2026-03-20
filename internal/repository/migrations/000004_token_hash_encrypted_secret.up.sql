-- Add token_hash column for one-way token storage.
-- The raw token is only returned on creation; after that, lookups use the hash.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS token_hash TEXT NOT NULL DEFAULT '';

-- Create index on token_hash for lookups.
CREATE INDEX IF NOT EXISTS idx_tokens_token_hash ON tokens(token_hash);

-- Add encrypted_secret column to event_subscriptions.
-- Webhook secrets will be stored encrypted; the plaintext secret column is kept
-- for backward compatibility during migration but should be cleared after migration.
ALTER TABLE event_subscriptions ADD COLUMN IF NOT EXISTS encrypted_secret TEXT NOT NULL DEFAULT '';
