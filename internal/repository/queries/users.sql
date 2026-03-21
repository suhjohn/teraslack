-- name: CreateUser :one
INSERT INTO users (id, team_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, is_admin, profile)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, team_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, is_admin, is_owner,
          is_restricted, deleted, profile, created_at, updated_at;

-- name: GetUser :one
SELECT id, team_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, is_admin, is_owner,
       is_restricted, deleted, profile, created_at, updated_at
FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, team_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, is_admin, is_owner,
       is_restricted, deleted, profile, created_at, updated_at
FROM users WHERE email = $1;

-- name: UpdateUser :one
UPDATE users
SET real_name = $2, display_name = $3, email = $4, is_admin = $5,
    is_restricted = $6, deleted = $7, profile = $8
WHERE id = $1
RETURNING id, team_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, is_admin, is_owner,
          is_restricted, deleted, profile, created_at, updated_at;

-- name: ListUsers :many
SELECT id, team_id, name, real_name, display_name, email, principal_type, owner_id, is_bot, is_admin, is_owner,
       is_restricted, deleted, profile, created_at, updated_at
FROM users
WHERE team_id = $1 AND id >= $2
ORDER BY id ASC
LIMIT $3;
