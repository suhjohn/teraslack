-- name: CreateConversation :one
INSERT INTO conversations (id, team_id, name, type, creator_id, topic_value, topic_creator, purpose_value, purpose_creator)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, team_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, created_at, updated_at;

-- name: GetConversation :one
SELECT id, team_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, created_at, updated_at
FROM conversations WHERE id = $1;

-- name: UpdateConversation :one
UPDATE conversations SET name = $2, is_archived = $3
WHERE id = $1
RETURNING id, team_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, created_at, updated_at;

-- name: SetConversationTopic :one
UPDATE conversations SET topic_value = $2, topic_creator = $3, topic_last_set = NOW()
WHERE id = $1
RETURNING id, team_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, created_at, updated_at;

-- name: SetConversationPurpose :one
UPDATE conversations SET purpose_value = $2, purpose_creator = $3, purpose_last_set = NOW()
WHERE id = $1
RETURNING id, team_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, created_at, updated_at;

-- name: ArchiveConversation :exec
UPDATE conversations SET is_archived = TRUE WHERE id = $1 AND is_archived = FALSE;

-- name: UnarchiveConversation :exec
UPDATE conversations SET is_archived = FALSE WHERE id = $1 AND is_archived = TRUE;

-- name: ListConversationsByTeam :many
SELECT id, team_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, created_at, updated_at
FROM conversations
WHERE team_id = $1 AND id >= $2
ORDER BY id ASC
LIMIT $3;

-- name: ListConversationsByTeamExcludeArchived :many
SELECT id, team_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, created_at, updated_at
FROM conversations
WHERE team_id = $1 AND is_archived = FALSE AND id >= $2
ORDER BY id ASC
LIMIT $3;

-- name: AddConversationMember :exec
INSERT INTO conversation_members (conversation_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveConversationMember :exec
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

-- name: UpdateConversationMemberCount :exec
UPDATE conversations SET num_members = (
    SELECT COUNT(*) FROM conversation_members WHERE conversation_id = $1
) WHERE id = $1;
