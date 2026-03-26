-- name: GetProjectorCheckpoint :one
SELECT last_event_id
FROM projector_checkpoints
WHERE name = $1;

-- name: SetProjectorCheckpoint :exec
INSERT INTO projector_checkpoints (name, last_event_id)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = NOW();
