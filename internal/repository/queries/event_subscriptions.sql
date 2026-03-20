-- name: CreateEventSubscription :one
INSERT INTO event_subscriptions (id, team_id, url, event_types, secret)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, team_id, url, event_types, secret, enabled, created_at, updated_at;

-- name: GetEventSubscription :one
SELECT id, team_id, url, event_types, secret, enabled, created_at, updated_at
FROM event_subscriptions WHERE id = $1;

-- name: UpdateEventSubscription :one
UPDATE event_subscriptions SET url = $2, event_types = $3, enabled = $4
WHERE id = $1
RETURNING id, team_id, url, event_types, secret, enabled, created_at, updated_at;

-- name: DeleteEventSubscription :exec
DELETE FROM event_subscriptions WHERE id = $1;

-- name: ListEventSubscriptions :many
SELECT id, team_id, url, event_types, secret, enabled, created_at, updated_at
FROM event_subscriptions WHERE team_id = $1
ORDER BY created_at ASC;

-- name: ListEventSubscriptionsByTeamAndEvent :many
SELECT id, team_id, url, event_types, secret, enabled, created_at, updated_at
FROM event_subscriptions
WHERE team_id = $1 AND enabled = TRUE AND $2 = ANY(event_types)
ORDER BY created_at ASC;

-- name: CreateEventRecord :exec
INSERT INTO events (id, type, team_id, payload)
VALUES ($1, $2, $3, $4);
