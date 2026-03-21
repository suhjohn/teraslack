-- Usergroups table
CREATE TABLE IF NOT EXISTS usergroups (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    handle      TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_external BOOLEAN NOT NULL DEFAULT FALSE,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    user_count  INT NOT NULL DEFAULT 0,
    created_by  TEXT NOT NULL REFERENCES users(id),
    updated_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usergroups_team_id ON usergroups(team_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_usergroups_team_handle ON usergroups(team_id, handle);

-- Usergroup members join table
CREATE TABLE IF NOT EXISTS usergroup_members (
    usergroup_id TEXT NOT NULL REFERENCES usergroups(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (usergroup_id, user_id)
);

-- Pins table
CREATE TABLE IF NOT EXISTS pins (
    channel_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_ts TEXT NOT NULL,
    pinned_by  TEXT NOT NULL REFERENCES users(id),
    pinned_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, message_ts),
    FOREIGN KEY (channel_id, message_ts) REFERENCES messages(channel_id, ts) ON DELETE CASCADE
);

-- Bookmarks table
CREATE TABLE IF NOT EXISTS bookmarks (
    id         TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'link',
    link       TEXT NOT NULL,
    emoji      TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL REFERENCES users(id),
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bookmarks_channel_id ON bookmarks(channel_id);

-- Files table
CREATE TABLE IF NOT EXISTS files (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    title                TEXT NOT NULL DEFAULT '',
    mimetype             TEXT NOT NULL DEFAULT '',
    filetype             TEXT NOT NULL DEFAULT '',
    size                 BIGINT NOT NULL DEFAULT 0,
    user_id              TEXT NOT NULL REFERENCES users(id),
    s3_key               TEXT NOT NULL DEFAULT '',
    url_private          TEXT NOT NULL DEFAULT '',
    url_private_download TEXT NOT NULL DEFAULT '',
    permalink            TEXT NOT NULL DEFAULT '',
    is_external          BOOLEAN NOT NULL DEFAULT FALSE,
    external_url         TEXT NOT NULL DEFAULT '',
    upload_complete      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_files_user_id ON files(user_id);

-- File-channel association
CREATE TABLE IF NOT EXISTS file_channels (
    file_id    TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    shared_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (file_id, channel_id)
);

-- Event subscriptions table (webhooks)
CREATE TABLE IF NOT EXISTS event_subscriptions (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL,
    url         TEXT NOT NULL,
    event_types TEXT[] NOT NULL DEFAULT '{}',
    secret      TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_event_subscriptions_team_id ON event_subscriptions(team_id);

-- Events log table
CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    team_id    TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_team_type ON events(team_id, type);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at DESC);

-- Auth tokens table
CREATE TABLE IF NOT EXISTS tokens (
    id         TEXT PRIMARY KEY,
    team_id    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users(id),
    token      TEXT NOT NULL UNIQUE,
    scopes     TEXT[] NOT NULL DEFAULT '{}',
    is_bot     BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tokens_token ON tokens(token);
CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id);

-- Triggers for updated_at
CREATE TRIGGER trg_usergroups_updated_at
    BEFORE UPDATE ON usergroups
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_bookmarks_updated_at
    BEFORE UPDATE ON bookmarks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_files_updated_at
    BEFORE UPDATE ON files
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_event_subscriptions_updated_at
    BEFORE UPDATE ON event_subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
