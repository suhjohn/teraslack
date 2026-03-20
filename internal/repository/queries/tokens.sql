-- name: CreateToken :one
INSERT INTO tokens (id, team_id, user_id, token, scopes, is_bot)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, team_id, user_id, token, scopes, is_bot, expires_at, created_at;

-- name: GetByToken :one
SELECT id, team_id, user_id, token, scopes, is_bot, expires_at, created_at
FROM tokens WHERE token = $1;

-- name: RevokeToken :exec
DELETE FROM tokens WHERE token = $1;
