-- name: InsertOutboxEntry :exec
INSERT INTO outbox (event_id, subscription_id, url, payload, secret, status, max_attempts, next_attempt_at)
VALUES ($1, $2, $3, $4, $5, 'pending', $6, NOW());

-- name: ClaimOutboxBatch :many
SELECT id, event_id, subscription_id, url, payload, secret, status, attempts, max_attempts, next_attempt_at, last_error, delivered_at, created_at
FROM outbox
WHERE status = 'pending' AND next_attempt_at <= NOW()
ORDER BY next_attempt_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxDelivered :exec
UPDATE outbox
SET status = 'delivered', delivered_at = NOW(), attempts = attempts + 1
WHERE id = $1;

-- name: MarkOutboxFailed :exec
UPDATE outbox
SET status = 'failed', last_error = $2, attempts = attempts + 1
WHERE id = $1;

-- name: ScheduleOutboxRetry :exec
UPDATE outbox
SET next_attempt_at = $2, last_error = $3, attempts = attempts + 1
WHERE id = $1;

-- name: CleanupDeliveredOutbox :execresult
DELETE FROM outbox
WHERE status = 'delivered' AND created_at < $1;

-- name: GetMatchingSubscriptions :many
SELECT id, team_id, url, event_types, secret, encrypted_secret, enabled, created_at, updated_at
FROM event_subscriptions
WHERE team_id = $1 AND enabled = TRUE AND $2::TEXT = ANY(event_types);
