-- name: InsertServiceEvent :one
INSERT INTO service_events (event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at;

-- name: GetServiceEventsByAggregate :many
SELECT id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at
FROM service_events
WHERE aggregate_type = $1 AND aggregate_id = $2
ORDER BY id ASC;

-- name: GetServiceEventsSince :many
SELECT id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at
FROM service_events
WHERE id > $1
ORDER BY id ASC
LIMIT $2;

-- name: GetServiceEventsByAggregateType :many
SELECT id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at
FROM service_events
WHERE aggregate_type = $1
ORDER BY id ASC;
