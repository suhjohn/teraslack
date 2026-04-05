-- name: CreateUser :one
INSERT INTO users (id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, account_type, profile)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, account_type,
          deleted, profile, created_at, updated_at;

-- name: GetUser :one
SELECT id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot,
       account_type, deleted, profile, created_at, updated_at
FROM users WHERE id = $1;

-- name: GetUserByWorkspaceAndAccount :one
SELECT id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot,
       account_type, deleted, profile, created_at, updated_at
FROM users
WHERE workspace_id = $1 AND account_id = $2;

-- name: GetWorkspaceMembershipIDByWorkspaceAndAccount :one
SELECT id
FROM workspace_memberships
WHERE workspace_id = $1 AND account_id = $2 AND status = 'active';

-- name: GetWorkspaceMembershipByWorkspaceAndAccount :one
SELECT id, workspace_id, account_id, role, status, membership_kind, guest_scope,
       created_by_account_id, updated_by_account_id, created_at, updated_at
FROM workspace_memberships
WHERE workspace_id = $1 AND account_id = $2 AND status = 'active';

-- name: UpsertWorkspaceMembershipByAccount :exec
INSERT INTO workspace_memberships (
    id, workspace_id, account_id, role, status, membership_kind, guest_scope, created_at, updated_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(account_id), sqlc.arg(role), 'active', 'full', 'workspace_full', NOW(), NOW()
)
ON CONFLICT (workspace_id, account_id) DO UPDATE SET
    role = EXCLUDED.role,
    status = 'active',
    membership_kind = 'full',
    guest_scope = 'workspace_full',
    updated_at = NOW();

-- name: ListUsersByAccount :many
SELECT id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot,
       account_type, deleted, profile, created_at, updated_at
FROM users
WHERE account_id = $1
ORDER BY workspace_id ASC, id ASC;

-- name: ListWorkspaceMembershipsByAccount :many
SELECT id, workspace_id, account_id, role, status, membership_kind, guest_scope,
       created_by_account_id, updated_by_account_id, created_at, updated_at
FROM workspace_memberships
WHERE account_id = $1 AND status = 'active'
ORDER BY workspace_id ASC, id ASC;

-- name: UpdateUser :one
UPDATE users
SET real_name = $2, display_name = $3, email = $4, account_type = $5, deleted = $6, profile = $7
WHERE id = $1
RETURNING id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, account_type,
          deleted, profile, created_at, updated_at;

-- name: ListUsers :many
SELECT id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot,
       account_type, deleted, profile, created_at, updated_at
FROM users
WHERE workspace_id = $1 AND id >= $2
ORDER BY id ASC
LIMIT $3;
