-- name: CreateAuthSession :one
INSERT INTO auth_sessions (id, workspace_id, account_id, user_id, session_hash, provider, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, workspace_id, account_id, user_id, session_hash, provider, expires_at, revoked_at, created_at;

-- name: GetAuthSessionByHash :one
SELECT id, workspace_id, account_id, user_id, session_hash, provider, expires_at, revoked_at, created_at
FROM auth_sessions
WHERE session_hash = $1;

-- name: RevokeAuthSessionByHash :exec
UPDATE auth_sessions
SET revoked_at = NOW()
WHERE session_hash = $1 AND revoked_at IS NULL;

-- name: DeletePendingEmailVerificationChallenges :exec
DELETE FROM email_verification_challenges
WHERE LOWER(email) = LOWER($1) AND consumed_at IS NULL;

-- name: CreateEmailVerificationChallenge :one
INSERT INTO email_verification_challenges (id, email, code_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, email, code_hash, expires_at, consumed_at, created_at;

-- name: GetEmailVerificationChallenge :one
SELECT id, email, code_hash, expires_at, consumed_at, created_at
FROM email_verification_challenges
WHERE LOWER(email) = LOWER($1) AND code_hash = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: ConsumeEmailVerificationChallenge :execrows
UPDATE email_verification_challenges
SET consumed_at = $2
WHERE id = $1 AND consumed_at IS NULL;

-- name: GetOAuthAccount :one
SELECT id, workspace_id, account_id, user_id, provider, provider_subject, email, created_at, updated_at
FROM oauth_accounts
WHERE workspace_id = $1 AND provider = $2 AND provider_subject = $3;

-- name: UpsertOAuthAccount :one
INSERT INTO oauth_accounts (id, workspace_id, account_id, user_id, provider, provider_subject, email)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, provider, provider_subject) DO UPDATE SET
    account_id = EXCLUDED.account_id,
    user_id = EXCLUDED.user_id,
    email = EXCLUDED.email,
    updated_at = NOW()
RETURNING id, workspace_id, account_id, user_id, provider, provider_subject, email, created_at, updated_at;

-- name: ListOAuthAccountsBySubject :many
SELECT id, workspace_id, account_id, user_id, provider, provider_subject, email, created_at, updated_at
FROM oauth_accounts
WHERE provider = $1 AND provider_subject = $2
ORDER BY created_at ASC, id ASC;
