-- name: CreateAccount :one
INSERT INTO accounts (
    id, principal_type, name, real_name, display_name, email, is_bot, deleted, profile
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, principal_type, name, real_name, display_name, email, is_bot,
          deleted, profile, created_at, updated_at;

-- name: GetAccount :one
SELECT id, principal_type, name, real_name, display_name, email, is_bot,
       deleted, profile, created_at, updated_at
FROM accounts
WHERE id = $1;

-- name: GetAccountByEmail :one
SELECT id, principal_type, name, real_name, display_name, email, is_bot,
       deleted, profile, created_at, updated_at
FROM accounts
WHERE LOWER(email) = LOWER($1)
ORDER BY created_at ASC, id ASC
LIMIT 1;

