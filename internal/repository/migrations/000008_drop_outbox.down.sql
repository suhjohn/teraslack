-- Recreate the outbox table (reversed from 000005)
CREATE TABLE outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_id        BIGINT NOT NULL REFERENCES service_events(id) ON DELETE CASCADE,
    subscription_id TEXT NOT NULL REFERENCES event_subscriptions(id) ON DELETE CASCADE,
    url             TEXT NOT NULL,
    payload         JSONB NOT NULL,
    secret          TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'delivered', 'failed')),
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error      TEXT NOT NULL DEFAULT '',
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_outbox_pending
    ON outbox (next_attempt_at) WHERE status IN ('pending', 'processing');
