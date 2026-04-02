-- name: CreateWorkspaceInvite :one
INSERT INTO workspace_invites (id, workspace_id, email, invited_by, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, workspace_id, email, invited_by, accepted_by_account_id, accepted_by_membership_id, expires_at, accepted_at, created_at, updated_at;

-- name: GetWorkspaceInviteByTokenHash :one
SELECT id, workspace_id, email, invited_by, accepted_by_account_id, accepted_by_membership_id, expires_at, accepted_at, created_at, updated_at
FROM workspace_invites
WHERE token_hash = $1;

-- name: MarkWorkspaceInviteAccepted :execrows
UPDATE workspace_invites
SET accepted_by_account_id = $2,
    accepted_by_membership_id = $3,
    accepted_at = $4
WHERE id = $1;
