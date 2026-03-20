-- name: InsertOutboxEntry :exec
INSERT INTO outbox (event_id, subscription_id, url, payload, secret, status, max_attempts, next_attempt_at)
VALUES ($1, $2, $3, $4, $5, 'pending', $6, NOW());

-- name: ClaimOutboxBatch :many
UPDATE outbox SET status = 'processing', attempts = attempts + 1
WHERE id IN (
  SELECT id FROM outbox
  WHERE status IN ('pending', 'processing') AND next_attempt_at <= NOW()
  ORDER BY next_attempt_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING id, event_id, subscription_id, url, payload, secret, status, attempts, max_attempts, next_attempt_at, last_error, delivered_at, created_at;

-- name: MarkOutboxDelivered :exec
UPDATE outbox
SET status = 'delivered', delivered_at = NOW()
WHERE id = $1;

-- name: MarkOutboxFailed :exec
UPDATE outbox
SET status = 'failed', last_error = $2
WHERE id = $1;

-- name: ScheduleOutboxRetry :exec
UPDATE outbox
SET status = 'pending', next_attempt_at = $2, last_error = $3
WHERE id = $1;

-- name: CleanupDeliveredOutbox :execresult
DELETE FROM outbox
WHERE status = 'delivered' AND created_at < $1;

-- name: GetMatchingSubscriptions :many
SELECT id, team_id, url, event_types, secret, encrypted_secret, enabled, created_at, updated_at
FROM event_subscriptions
WHERE team_id = $1 AND enabled = TRUE AND $2::TEXT = ANY(event_types);
