-- name: CreateExternalMember :one
INSERT INTO external_members (
    id, conversation_id, host_workspace_id, external_workspace_id, account_id,
    access_mode, allowed_capabilities, invited_by, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, conversation_id, host_workspace_id, external_workspace_id, account_id,
          access_mode, allowed_capabilities, invited_by, created_at, expires_at, revoked_at;

-- name: GetExternalMember :one
SELECT id, conversation_id, host_workspace_id, external_workspace_id, account_id,
       access_mode, allowed_capabilities, invited_by, created_at, expires_at, revoked_at
FROM external_members
WHERE id = $1;

-- name: GetActiveExternalMemberByConversationAndAccount :one
SELECT id, conversation_id, host_workspace_id, external_workspace_id, account_id,
       access_mode, allowed_capabilities, invited_by, created_at, expires_at, revoked_at
FROM external_members
WHERE conversation_id = $1
  AND account_id = $2
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListExternalMembersByConversation :many
SELECT id, conversation_id, host_workspace_id, external_workspace_id, account_id,
       access_mode, allowed_capabilities, invited_by, created_at, expires_at, revoked_at
FROM external_members
WHERE conversation_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListActiveExternalMembersByAccountAndWorkspace :many
SELECT id, conversation_id, host_workspace_id, external_workspace_id, account_id,
       access_mode, allowed_capabilities, invited_by, created_at, expires_at, revoked_at
FROM external_members
WHERE account_id = $1
  AND host_workspace_id = $2
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY conversation_id ASC, created_at DESC, id DESC;

-- name: UpdateExternalMember :exec
UPDATE external_members
SET access_mode = $2,
    allowed_capabilities = $3,
    expires_at = $4,
    revoked_at = $5
WHERE id = $1;

-- name: RevokeExternalMember :exec
UPDATE external_members
SET revoked_at = $2
WHERE id = $1;

-- name: RevokeExternalMembersByExternalWorkspace :exec
UPDATE external_members
SET revoked_at = $3
WHERE host_workspace_id = $1
  AND external_workspace_id = $2
  AND revoked_at IS NULL;
