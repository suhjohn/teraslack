-- name: CreateAuthorizationAuditLog :one
INSERT INTO authorization_audit_log (
    id, workspace_id, actor_id, api_key_id, on_behalf_of, action, resource, resource_id, metadata
)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9)
RETURNING id, workspace_id, COALESCE(actor_id, ''), COALESCE(api_key_id, ''), COALESCE(on_behalf_of, ''), action, resource, resource_id, metadata, created_at;

-- name: ListAuthorizationAuditLogs :many
SELECT id, workspace_id, COALESCE(actor_id, ''), COALESCE(api_key_id, ''), COALESCE(on_behalf_of, ''), action, resource, resource_id, metadata, created_at
FROM authorization_audit_log
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;
