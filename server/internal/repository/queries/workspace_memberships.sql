-- name: CreateWorkspaceMembership :one
INSERT INTO workspace_memberships (
    id, account_id, workspace_id, user_id, account_type
)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, account_id, workspace_id, user_id, account_type, created_at, updated_at;

-- name: GetWorkspaceMembershipByWorkspaceAndAccount :one
SELECT id, account_id, workspace_id, user_id, account_type, created_at, updated_at
FROM workspace_memberships
WHERE workspace_id = $1 AND account_id = $2;

-- name: GetWorkspaceMembershipByLegacyUserID :one
SELECT id, account_id, workspace_id, user_id, account_type, created_at, updated_at
FROM workspace_memberships
WHERE user_id = $1;

-- name: ListWorkspaceMembershipsByAccount :many
SELECT id, account_id, workspace_id, user_id, account_type, created_at, updated_at
FROM workspace_memberships
WHERE account_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListWorkspaceMembershipsByWorkspace :many
SELECT id, account_id, workspace_id, user_id, account_type, created_at, updated_at
FROM workspace_memberships
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC;

-- name: AttachWorkspaceMembershipUser :one
UPDATE workspace_memberships
SET user_id = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING id, account_id, workspace_id, user_id, account_type, created_at, updated_at;

-- name: UpdateWorkspaceMembershipAccountTypeByLegacyUserID :one
UPDATE workspace_memberships
SET account_type = $2,
    updated_at = NOW()
WHERE user_id = $1
RETURNING id, account_id, workspace_id, user_id, account_type, created_at, updated_at;
