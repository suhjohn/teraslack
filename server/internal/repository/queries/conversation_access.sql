-- name: ListConversationManagers :many
SELECT conversation_id, user_id, assigned_by, created_at
FROM conversation_manager_assignments
WHERE conversation_id = $1
ORDER BY user_id ASC;

-- name: DeleteConversationManagers :exec
DELETE FROM conversation_manager_assignments
WHERE conversation_id = $1;

-- name: InsertConversationManager :exec
INSERT INTO conversation_manager_assignments (conversation_id, user_id, assigned_by)
VALUES ($1, $2, $3);

-- name: IsConversationManager :one
SELECT EXISTS(
    SELECT 1
    FROM conversation_manager_assignments
    WHERE conversation_id = $1 AND user_id = $2
);

-- name: GetConversationPostingPolicy :one
SELECT conversation_id, policy_type, policy_json, updated_by, updated_at
FROM conversation_posting_policies
WHERE conversation_id = $1;

-- name: UpsertConversationPostingPolicy :one
INSERT INTO conversation_posting_policies (conversation_id, policy_type, policy_json, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id)
DO UPDATE SET
    policy_type = EXCLUDED.policy_type,
    policy_json = EXCLUDED.policy_json,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING updated_at;
