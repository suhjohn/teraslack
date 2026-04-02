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

-- name: ListUsersByAccount :many
SELECT id, account_id, workspace_id, name, real_name, display_name, email, principal_type, owner_id, is_bot,
       account_type, deleted, profile, created_at, updated_at
FROM users
WHERE account_id = $1
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
