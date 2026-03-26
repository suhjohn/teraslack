-- name: CreateExternalPrincipalAccess :exec
INSERT INTO external_principal_access (
    id, host_workspace_id, principal_id, principal_type, home_workspace_id, access_mode,
    allowed_capabilities, granted_by, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetExternalPrincipalAccess :one
SELECT id, host_workspace_id, principal_id, principal_type, home_workspace_id, access_mode,
       allowed_capabilities, granted_by, created_at, expires_at, revoked_at
FROM external_principal_access
WHERE id = $1;

-- name: GetActiveExternalPrincipalAccessByPrincipal :one
SELECT id
FROM external_principal_access
WHERE host_workspace_id = $1
  AND principal_id = $2
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC
LIMIT 1;

-- name: ListExternalPrincipalAccessIDs :many
SELECT id
FROM external_principal_access
WHERE host_workspace_id = $1
ORDER BY created_at DESC, id DESC;

-- name: UpdateExternalPrincipalAccess :exec
UPDATE external_principal_access
SET access_mode = $2,
    allowed_capabilities = $3,
    expires_at = $4,
    revoked_at = $5
WHERE id = $1;

-- name: RevokeExternalPrincipalAccess :exec
UPDATE external_principal_access
SET revoked_at = $2
WHERE id = $1;

-- name: HasExternalPrincipalConversationAccess :one
SELECT EXISTS(
    SELECT 1
    FROM external_principal_conversation_assignments
    WHERE access_id = $1 AND conversation_id = $2
);

-- name: ListExternalPrincipalConversationAssignments :many
SELECT conversation_id
FROM external_principal_conversation_assignments
WHERE access_id = $1
ORDER BY conversation_id ASC;

-- name: DeleteExternalPrincipalConversationAssignments :exec
DELETE FROM external_principal_conversation_assignments
WHERE access_id = $1;

-- name: InsertExternalPrincipalConversationAssignment :exec
INSERT INTO external_principal_conversation_assignments (access_id, conversation_id, granted_by)
VALUES ($1, $2, $3);

-- name: GetExternalAccessStateByPrincipal :one
SELECT id, allowed_capabilities
FROM external_principal_access
WHERE host_workspace_id = $1
  AND principal_id = $2
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC
LIMIT 1;
