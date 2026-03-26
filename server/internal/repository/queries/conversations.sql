-- name: CreateConversation :one
INSERT INTO conversations (
    id, workspace_id, name, type, creator_id,
    topic_value, topic_creator, purpose_value, purpose_creator,
    last_message_ts, last_activity_ts
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, workspace_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: GetConversation :one
SELECT id, workspace_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, last_message_ts, last_activity_ts, created_at, updated_at
FROM conversations WHERE id = $1;

-- name: UpdateConversation :one
UPDATE conversations SET name = $2, is_archived = $3
WHERE id = $1
RETURNING id, workspace_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: SetConversationTopic :one
UPDATE conversations SET topic_value = $2, topic_creator = $3, topic_last_set = NOW()
WHERE id = $1
RETURNING id, workspace_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: SetConversationPurpose :one
UPDATE conversations SET purpose_value = $2, purpose_creator = $3, purpose_last_set = NOW()
WHERE id = $1
RETURNING id, workspace_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: ArchiveConversation :exec
UPDATE conversations SET is_archived = TRUE WHERE id = $1 AND is_archived = FALSE;

-- name: UnarchiveConversation :exec
UPDATE conversations SET is_archived = FALSE WHERE id = $1 AND is_archived = TRUE;

-- name: ListVisibleConversations :many
SELECT c.id, c.workspace_id, c.name, c.type, c.creator_id, c.is_archived,
       c.topic_value, c.topic_creator, c.topic_last_set,
       c.purpose_value, c.purpose_creator, c.purpose_last_set,
       c.num_members, c.last_message_ts, c.last_activity_ts,
       cr.last_read_ts,
       CASE
           WHEN sqlc.arg(user_id) = '' THEN NULL::boolean
           WHEN c.last_message_ts IS NULL THEN FALSE
           WHEN cr.last_read_ts IS NULL THEN TRUE
           ELSE cr.last_read_ts < c.last_message_ts
       END AS has_unread,
       c.created_at, c.updated_at
FROM conversations c
LEFT JOIN conversation_members cm
  ON cm.conversation_id = c.id AND cm.user_id = sqlc.arg(user_id)
LEFT JOIN conversation_reads cr
  ON cr.conversation_id = c.id AND cr.user_id = sqlc.arg(user_id)
WHERE c.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.arg(exclude_archived)::bool = FALSE OR c.is_archived = FALSE)
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR c.type = ANY(sqlc.arg(types)::text[]))
  AND (
    sqlc.arg(cursor_activity) = ''
    OR (COALESCE(c.last_activity_ts, ''), c.id) < (sqlc.arg(cursor_activity), sqlc.arg(cursor_id))
  )
  AND (
    sqlc.arg(user_id) = ''
    OR
    c.type = 'public_channel'
    OR cm.user_id IS NOT NULL
  )
ORDER BY COALESCE(c.last_activity_ts, '') DESC, c.id DESC
LIMIT sqlc.arg(limit_count);

-- name: AddConversationMember :execrows
INSERT INTO conversation_members (conversation_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveConversationMember :execrows
DELETE FROM conversation_members WHERE conversation_id = $1 AND user_id = $2;

-- name: ListConversationMembers :many
SELECT conversation_id, user_id, joined_at
FROM conversation_members
WHERE conversation_id = $1 AND user_id >= $2
ORDER BY user_id ASC
LIMIT $3;

-- name: IsConversationMember :one
SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2);

-- name: CountConversationMembers :one
SELECT COUNT(*) FROM conversation_members WHERE conversation_id = $1;

-- name: LockConversationForUpdate :one
SELECT id FROM conversations WHERE id = $1 FOR UPDATE;

-- name: IncrementConversationMemberCount :exec
UPDATE conversations
SET num_members = num_members + 1
WHERE id = $1;

-- name: DecrementConversationMemberCount :exec
UPDATE conversations
SET num_members = num_members - 1
WHERE id = $1;

-- name: DeleteCanonicalDMByConversation :execrows
DELETE FROM canonical_dms
WHERE conversation_id = $1;

-- name: UpdateConversationLastMessageAndActivity :exec
UPDATE conversations
SET last_message_ts = CASE
        WHEN last_message_ts IS NULL OR last_message_ts < sqlc.arg(ts) THEN sqlc.arg(ts)
        ELSE last_message_ts
    END,
    last_activity_ts = CASE
        WHEN last_activity_ts IS NULL OR last_activity_ts < sqlc.arg(ts) THEN sqlc.arg(ts)
        ELSE last_activity_ts
    END
WHERE id = sqlc.arg(id);

-- name: UpdateConversationLastActivity :exec
UPDATE conversations
SET last_activity_ts = CASE
        WHEN last_activity_ts IS NULL OR last_activity_ts < sqlc.arg(ts) THEN sqlc.arg(ts)
        ELSE last_activity_ts
    END
WHERE id = sqlc.arg(id);

-- name: GetCanonicalDMConversation :one
SELECT c.id, c.workspace_id, c.name, c.type, c.creator_id, c.is_archived,
       c.topic_value, c.topic_creator, c.topic_last_set,
       c.purpose_value, c.purpose_creator, c.purpose_last_set,
       c.num_members, c.last_message_ts, c.last_activity_ts, c.created_at, c.updated_at
FROM canonical_dms d
JOIN conversations c ON c.id = d.conversation_id
WHERE d.workspace_id = sqlc.arg(workspace_id)
  AND d.user_low_id = sqlc.arg(user_low_id)
  AND d.user_high_id = sqlc.arg(user_high_id);

-- name: CreateCanonicalDM :execrows
INSERT INTO canonical_dms (workspace_id, user_low_id, user_high_id, conversation_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;
