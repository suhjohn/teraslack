-- name: UpsertConversationReadV2 :exec
INSERT INTO conversation_reads_v2 (conversation_id, account_id, last_read_ts, last_read_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id, account_id) DO UPDATE SET
    last_read_ts = EXCLUDED.last_read_ts,
    last_read_at = EXCLUDED.last_read_at;

-- name: GetConversationReadV2 :one
SELECT conversation_id, account_id, last_read_ts, last_read_at
FROM conversation_reads_v2
WHERE conversation_id = $1 AND account_id = $2;
