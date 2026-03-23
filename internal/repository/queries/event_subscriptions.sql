-- name: CreateEventSubscription :one
INSERT INTO event_subscriptions (id, team_id, url, event_type, resource_type, resource_id, encrypted_secret)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, team_id, url, event_type, resource_type, resource_id, encrypted_secret, enabled, created_at, updated_at;

-- name: GetEventSubscription :one
SELECT id, team_id, url, event_type, resource_type, resource_id, encrypted_secret, enabled, created_at, updated_at
FROM event_subscriptions WHERE id = $1;

-- name: UpdateEventSubscription :one
UPDATE event_subscriptions
SET url = $2, event_type = $3, resource_type = $4, resource_id = $5, enabled = $6
WHERE id = $1
RETURNING id, team_id, url, event_type, resource_type, resource_id, encrypted_secret, enabled, created_at, updated_at;

-- name: DeleteEventSubscription :exec
DELETE FROM event_subscriptions WHERE id = $1;

-- name: ListEventSubscriptions :many
SELECT id, team_id, url, event_type, resource_type, resource_id, encrypted_secret, enabled, created_at, updated_at
FROM event_subscriptions WHERE team_id = $1
ORDER BY created_at ASC;

-- name: ListEventSubscriptionsByTeamAndEvent :many
SELECT id, team_id, url, event_type, resource_type, resource_id, encrypted_secret, enabled, created_at, updated_at
FROM event_subscriptions
WHERE team_id = $1
  AND enabled = TRUE
  AND (event_type = '' OR event_type = $2)
  AND (sqlc.narg(resource_type)::TEXT IS NULL OR resource_type = '' OR resource_type = sqlc.narg(resource_type))
  AND (sqlc.narg(resource_id)::TEXT IS NULL OR resource_id = '' OR resource_id = sqlc.narg(resource_id))
ORDER BY created_at ASC;
