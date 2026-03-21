-- name: CreateToken :one
INSERT INTO tokens (id, team_id, user_id, token, token_hash, scopes, is_bot)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, team_id, user_id, token, token_hash, scopes, is_bot, expires_at, created_at;

-- name: GetByTokenHash :one
SELECT id, team_id, user_id, token, token_hash, scopes, is_bot, expires_at, created_at
FROM tokens WHERE token_hash = $1;

-- name: GetTokenByID :one
SELECT id, team_id, user_id, token, token_hash, scopes, is_bot, expires_at, created_at
FROM tokens WHERE id = $1;

-- name: RevokeTokenByHash :exec
DELETE FROM tokens WHERE token_hash = $1;
