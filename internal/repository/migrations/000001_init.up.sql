-- Users table
CREATE TABLE IF NOT EXISTS users (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    real_name   TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL DEFAULT '',
    is_bot      BOOLEAN NOT NULL DEFAULT FALSE,
    is_admin    BOOLEAN NOT NULL DEFAULT FALSE,
    is_owner    BOOLEAN NOT NULL DEFAULT FALSE,
    is_restricted BOOLEAN NOT NULL DEFAULT FALSE,
    deleted     BOOLEAN NOT NULL DEFAULT FALSE,
    profile     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_team_id ON users(team_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email != '';
CREATE INDEX IF NOT EXISTS idx_users_name ON users(name);

-- Conversations table
CREATE TABLE IF NOT EXISTS conversations (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL CHECK (type IN ('public_channel', 'private_channel', 'im', 'mpim')),
    creator_id  TEXT NOT NULL REFERENCES users(id),
    is_archived BOOLEAN NOT NULL DEFAULT FALSE,
    topic_value TEXT NOT NULL DEFAULT '',
    topic_creator TEXT NOT NULL DEFAULT '',
    topic_last_set TIMESTAMPTZ,
    purpose_value TEXT NOT NULL DEFAULT '',
    purpose_creator TEXT NOT NULL DEFAULT '',
    purpose_last_set TIMESTAMPTZ,
    num_members INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversations_team_id ON conversations(team_id);
CREATE INDEX IF NOT EXISTS idx_conversations_type ON conversations(type);
CREATE INDEX IF NOT EXISTS idx_conversations_name ON conversations(name);

-- Conversation members join table
CREATE TABLE IF NOT EXISTS conversation_members (
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_conversation_members_user_id ON conversation_members(user_id);

-- Messages table
CREATE TABLE IF NOT EXISTS messages (
    ts          TEXT NOT NULL,
    channel_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id),
    text        TEXT NOT NULL DEFAULT '',
    thread_ts   TEXT,
    type        TEXT NOT NULL DEFAULT 'message',
    subtype     TEXT,
    blocks      JSONB,
    metadata    JSONB,
    edited_by   TEXT,
    edited_at   TEXT,
    reply_count       INT NOT NULL DEFAULT 0,
    reply_users_count INT NOT NULL DEFAULT 0,
    latest_reply      TEXT,
    is_deleted  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, ts)
);

CREATE INDEX IF NOT EXISTS idx_messages_channel_ts ON messages(channel_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(channel_id, thread_ts) WHERE thread_ts IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);

-- Reactions table
CREATE TABLE IF NOT EXISTS reactions (
    id          BIGSERIAL PRIMARY KEY,
    channel_id  TEXT NOT NULL,
    message_ts  TEXT NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users(id),
    emoji       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (channel_id, message_ts) REFERENCES messages(channel_id, ts) ON DELETE CASCADE,
    UNIQUE (channel_id, message_ts, user_id, emoji)
);

CREATE INDEX IF NOT EXISTS idx_reactions_message ON reactions(channel_id, message_ts);

-- Function to update updated_at on row change
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_conversations_updated_at
    BEFORE UPDATE ON conversations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_messages_updated_at
    BEFORE UPDATE ON messages
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
