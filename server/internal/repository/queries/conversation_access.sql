-- name: DeleteConversationManagersV2 :exec
DELETE FROM conversation_manager_assignments_v2
WHERE conversation_id = $1;

-- name: InsertConversationManagerV2 :exec
INSERT INTO conversation_manager_assignments_v2 (conversation_id, account_id, assigned_by_account_id)
VALUES (
    sqlc.arg(conversation_id),
    sqlc.arg(account_id),
    NULLIF(sqlc.arg(assigned_by_account_id), '')
)
ON CONFLICT DO NOTHING;

-- name: IsConversationManagerV2 :one
SELECT EXISTS(
    SELECT 1
    FROM conversation_manager_assignments_v2
    WHERE conversation_id = $1 AND account_id = $2
);

-- name: ListConversationManagersV2 :many
SELECT conversation_id, account_id, assigned_by_account_id, created_at
FROM conversation_manager_assignments_v2
WHERE conversation_id = $1
ORDER BY account_id ASC;

-- name: GetConversationPostingPolicy :one
SELECT conversation_id, policy_type, policy_json, updated_by, updated_at
FROM conversation_posting_policies
WHERE conversation_id = $1;

-- name: ReplaceConversationPostingPolicyAllowedAccounts :exec
WITH deleted AS (
    DELETE FROM conversation_posting_policy_allowed_accounts_v2
    WHERE conversation_id = sqlc.arg(conversation_id)
)
INSERT INTO conversation_posting_policy_allowed_accounts_v2 (conversation_id, account_id)
SELECT DISTINCT sqlc.arg(conversation_id), UNNEST(sqlc.arg(account_ids)::text[])
ON CONFLICT DO NOTHING;

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
