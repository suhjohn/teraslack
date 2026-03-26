-- name: UpsertConversationRead :exec
INSERT INTO conversation_reads (workspace_id, conversation_id, user_id, last_read_ts, last_read_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (conversation_id, user_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    last_read_ts = EXCLUDED.last_read_ts,
    last_read_at = EXCLUDED.last_read_at;

-- name: GetConversationRead :one
SELECT workspace_id, conversation_id, user_id, last_read_ts, last_read_at
FROM conversation_reads
WHERE conversation_id = $1 AND user_id = $2;
