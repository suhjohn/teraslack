-- name: CreateMessage :one
INSERT INTO messages (ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, blocks, metadata)
VALUES (
    sqlc.arg(ts),
    sqlc.arg(channel_id),
    sqlc.arg(user_id),
    sqlc.arg(author_account_id),
    sqlc.arg(author_workspace_membership_id),
    sqlc.arg(text),
    sqlc.arg(thread_ts),
    sqlc.arg(type),
    sqlc.arg(blocks),
    sqlc.arg(metadata)
)
RETURNING ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
          blocks, metadata, edited_by, edited_at,
          reply_count, reply_users_count, latest_reply,
          is_deleted, created_at, updated_at;

-- name: CreateMessageByUser :one
WITH actor AS (
    SELECT u.id AS user_id, u.account_id, wm.id AS author_workspace_membership_id
    FROM users u
    JOIN conversations c
      ON c.id = sqlc.arg(channel_id)
    LEFT JOIN workspace_memberships wm
      ON wm.workspace_id = c.owner_workspace_id
     AND wm.account_id = u.account_id
    WHERE u.id = sqlc.arg(user_id)
)
INSERT INTO messages (ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, blocks, metadata)
SELECT sqlc.arg(ts), sqlc.arg(channel_id), actor.user_id, actor.account_id, actor.author_workspace_membership_id,
       sqlc.arg(text), sqlc.arg(thread_ts), sqlc.arg(type), sqlc.arg(blocks), sqlc.arg(metadata)
FROM actor
RETURNING ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
          blocks, metadata, edited_by, edited_at,
          reply_count, reply_users_count, latest_reply,
          is_deleted, created_at, updated_at;

-- name: GetMessageRow :one
SELECT ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
       blocks, metadata, edited_by, edited_at,
       reply_count, reply_users_count, latest_reply,
       is_deleted, created_at, updated_at
FROM messages WHERE channel_id = $1 AND ts = $2;

-- name: UpdateMessage :one
UPDATE messages
SET text = $3, blocks = $4, metadata = $5, edited_by = $6, edited_at = $7
WHERE channel_id = $1 AND ts = $2
RETURNING ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
          blocks, metadata, edited_by, edited_at,
          reply_count, reply_users_count, latest_reply,
          is_deleted, created_at, updated_at;

-- name: SoftDeleteMessage :exec
UPDATE messages SET is_deleted = TRUE, text = '' WHERE channel_id = $1 AND ts = $2 AND is_deleted = FALSE;

-- name: ListMessagesHistory :many
SELECT ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
       blocks, metadata, edited_by, edited_at,
       reply_count, reply_users_count, latest_reply,
       is_deleted, created_at, updated_at
FROM messages
WHERE channel_id = $1 AND is_deleted = FALSE AND thread_ts IS NULL AND ts <= $2
ORDER BY ts DESC
LIMIT $3;

-- name: ListMessagesHistoryNocursor :many
SELECT ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
       blocks, metadata, edited_by, edited_at,
       reply_count, reply_users_count, latest_reply,
       is_deleted, created_at, updated_at
FROM messages
WHERE channel_id = $1 AND is_deleted = FALSE AND thread_ts IS NULL
ORDER BY ts DESC
LIMIT $2;

-- name: ListReplies :many
SELECT ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
       blocks, metadata, edited_by, edited_at,
       reply_count, reply_users_count, latest_reply,
       is_deleted, created_at, updated_at
FROM messages
WHERE channel_id = $1 AND (thread_ts = $2 OR ts = $2) AND is_deleted = FALSE AND ts >= $3
ORDER BY ts ASC
LIMIT $4;

-- name: ListRepliesNoCursor :many
SELECT ts, channel_id, user_id, author_account_id, author_workspace_membership_id, text, thread_ts, type, subtype,
       blocks, metadata, edited_by, edited_at,
       reply_count, reply_users_count, latest_reply,
       is_deleted, created_at, updated_at
FROM messages
WHERE channel_id = $1 AND (thread_ts = $2 OR ts = $2) AND is_deleted = FALSE
ORDER BY ts ASC
LIMIT $3;

-- name: IncrementParentReplyCountAndLatestReply :exec
UPDATE messages
SET reply_count = reply_count + 1,
    latest_reply = $3
WHERE channel_id = $1 AND ts = $2;

-- name: IncrementParentReplyUsersCount :exec
UPDATE messages
SET reply_users_count = reply_users_count + 1
WHERE channel_id = $1 AND ts = $2;

-- name: AddThreadParticipant :execrows
INSERT INTO thread_participants (channel_id, thread_ts, user_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: AddReaction :execrows
INSERT INTO reactions (channel_id, message_ts, user_id, emoji)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id, message_ts, user_id, emoji) DO NOTHING;

-- name: RemoveReaction :execrows
DELETE FROM reactions WHERE channel_id = $1 AND message_ts = $2 AND user_id = $3 AND emoji = $4;

-- name: GetReactions :many
SELECT emoji, ARRAY_AGG(user_id ORDER BY created_at) AS users, COUNT(*) AS count
FROM reactions
WHERE channel_id = $1 AND message_ts = $2
GROUP BY emoji
ORDER BY MIN(created_at);
