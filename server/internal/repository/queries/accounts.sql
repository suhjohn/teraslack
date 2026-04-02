-- name: CreateAccount :one
INSERT INTO accounts (
    id, principal_type, email, is_bot, deleted
)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, principal_type, email, is_bot, deleted, created_at, updated_at;

-- name: GetAccount :one
SELECT id, principal_type, email, is_bot, deleted, created_at, updated_at
FROM accounts
WHERE id = $1;

-- name: GetAccountByEmail :one
SELECT id, principal_type, email, is_bot, deleted, created_at, updated_at
FROM accounts
WHERE LOWER(email) = LOWER($1)
ORDER BY created_at ASC, id ASC
LIMIT 1;
