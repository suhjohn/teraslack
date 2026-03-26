-- name: InsertInternalEvent :one
INSERT INTO internal_events (event_type, aggregate_type, aggregate_id, workspace_id, actor_id, shard_key, shard_id, payload, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, event_type, aggregate_type, aggregate_id, workspace_id, actor_id, shard_key, shard_id, payload, metadata, created_at;

-- name: GetInternalEventsByAggregate :many
SELECT id, event_type, aggregate_type, aggregate_id, workspace_id, actor_id, shard_key, shard_id, payload, metadata, created_at
FROM internal_events
WHERE aggregate_type = $1 AND aggregate_id = $2
ORDER BY id ASC;

-- name: GetInternalEventsSince :many
SELECT id, event_type, aggregate_type, aggregate_id, workspace_id, actor_id, shard_key, shard_id, payload, metadata, created_at
FROM internal_events
WHERE id > $1
ORDER BY id ASC
LIMIT $2;

-- name: GetInternalEventsSinceByShard :many
SELECT id, event_type, aggregate_type, aggregate_id, workspace_id, actor_id, shard_key, shard_id, payload, metadata, created_at
FROM internal_events
WHERE shard_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3;

-- name: GetInternalEventsByAggregateType :many
SELECT id, event_type, aggregate_type, aggregate_id, workspace_id, actor_id, shard_key, shard_id, payload, metadata, created_at
FROM internal_events
WHERE aggregate_type = $1
ORDER BY id ASC;
