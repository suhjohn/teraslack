-- name: AppendEvent :one
INSERT INTO event_log (aggregate_type, aggregate_id, event_type, event_data, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at;

-- name: GetEventsByAggregate :many
SELECT sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at
FROM event_log
WHERE aggregate_type = $1 AND aggregate_id = $2
ORDER BY sequence_id ASC;

-- name: GetEventsByAggregateSince :many
SELECT sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at
FROM event_log
WHERE aggregate_type = $1 AND aggregate_id = $2 AND sequence_id > $3
ORDER BY sequence_id ASC;

-- name: GetEventsByType :many
SELECT sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at
FROM event_log
WHERE event_type = $1
ORDER BY sequence_id DESC
LIMIT $2;

-- name: GetAllEventsSince :many
SELECT sequence_id, aggregate_type, aggregate_id, event_type, event_data, metadata, created_at
FROM event_log
WHERE sequence_id > $1
ORDER BY sequence_id ASC
LIMIT $2;
