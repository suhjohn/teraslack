-- name: AddPin :one
INSERT INTO pins (channel_id, message_ts, pinned_by)
VALUES ($1, $2, $3)
ON CONFLICT (channel_id, message_ts) DO NOTHING
RETURNING channel_id, message_ts, pinned_by, pinned_at;

-- name: RemovePin :exec
DELETE FROM pins WHERE channel_id = $1 AND message_ts = $2;

-- name: ListPins :many
SELECT channel_id, message_ts, pinned_by, pinned_at
FROM pins WHERE channel_id = $1
ORDER BY pinned_at DESC;
