-- Event log table (event sourcing - append-only source of truth)
CREATE TABLE IF NOT EXISTS event_log (
    sequence_id    BIGSERIAL PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id   TEXT NOT NULL,
    event_type     TEXT NOT NULL,
    event_data     JSONB NOT NULL,
    metadata       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_event_log_aggregate
    ON event_log (aggregate_type, aggregate_id, sequence_id);

CREATE INDEX IF NOT EXISTS idx_event_log_type
    ON event_log (event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_event_log_created_at
    ON event_log (created_at DESC);
