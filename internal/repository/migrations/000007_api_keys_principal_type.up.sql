-- Add principal_type and owner_id to users table.
-- principal_type: 'human' (default), 'agent', or 'system'
-- owner_id: for agents, references the human who owns this agent
ALTER TABLE users ADD COLUMN IF NOT EXISTS principal_type TEXT NOT NULL DEFAULT 'human';
ALTER TABLE users ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_users_principal_type ON users(principal_type);
CREATE INDEX IF NOT EXISTS idx_users_owner_id ON users(owner_id) WHERE owner_id != '';

-- API keys table — replaces tokens for new API key management.
-- Existing tokens table is kept for backward compatibility.
CREATE TABLE IF NOT EXISTS api_keys (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    key_hash             TEXT NOT NULL,
    key_prefix           TEXT NOT NULL,     -- e.g. 'sk_live_'
    key_hint             TEXT NOT NULL,     -- last 4 chars of the raw key
    team_id              TEXT NOT NULL,
    principal_id         TEXT NOT NULL REFERENCES users(id),
    created_by           TEXT NOT NULL REFERENCES users(id),
    on_behalf_of         TEXT NOT NULL DEFAULT '',  -- delegation chain
    type                 TEXT NOT NULL DEFAULT 'persistent' CHECK (type IN ('persistent', 'session', 'restricted')),
    environment          TEXT NOT NULL DEFAULT 'live' CHECK (environment IN ('live', 'test')),
    permissions          TEXT[] NOT NULL DEFAULT '{}',
    expires_at           TIMESTAMPTZ,
    last_used_at         TIMESTAMPTZ,
    request_count        BIGINT NOT NULL DEFAULT 0,
    revoked              BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at           TIMESTAMPTZ,
    rotated_to_id        TEXT NOT NULL DEFAULT '',
    grace_period_ends_at TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_team_id ON api_keys(team_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_principal_id ON api_keys(principal_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_created_by ON api_keys(created_by);

DROP TRIGGER IF EXISTS trg_api_keys_updated_at ON api_keys;
CREATE TRIGGER trg_api_keys_updated_at
    BEFORE UPDATE ON api_keys
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
