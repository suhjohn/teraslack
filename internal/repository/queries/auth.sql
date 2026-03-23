-- name: CreateAuthSession :one
INSERT INTO auth_sessions (id, team_id, user_id, session_hash, provider, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, team_id, user_id, session_hash, provider, expires_at, revoked_at, created_at;

-- name: GetAuthSessionByHash :one
SELECT id, team_id, user_id, session_hash, provider, expires_at, revoked_at, created_at
FROM auth_sessions
WHERE session_hash = $1;

-- name: RevokeAuthSessionByHash :exec
UPDATE auth_sessions
SET revoked_at = NOW()
WHERE session_hash = $1 AND revoked_at IS NULL;

-- name: GetOAuthAccount :one
SELECT id, team_id, user_id, provider, provider_subject, email, created_at, updated_at
FROM oauth_accounts
WHERE team_id = $1 AND provider = $2 AND provider_subject = $3;

-- name: UpsertOAuthAccount :one
INSERT INTO oauth_accounts (id, team_id, user_id, provider, provider_subject, email)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (team_id, provider, provider_subject) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    email = EXCLUDED.email,
    updated_at = NOW()
RETURNING id, team_id, user_id, provider, provider_subject, email, created_at, updated_at;
