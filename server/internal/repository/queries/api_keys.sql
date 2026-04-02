-- name: CreateAPIKey :one
INSERT INTO api_keys (id, name, description, key_hash, key_prefix, key_hint, scope, workspace_id, owner_account_id, workspace_ids, created_by, on_behalf_of, type, environment, permissions, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, $11, $12, $13, $14, $15, $16)
RETURNING id, name, description, key_hash, key_prefix, key_hint, scope, COALESCE(workspace_id, ''), COALESCE(owner_account_id, ''), COALESCE(workspace_ids, '{}'::text[]), created_by, on_behalf_of,
          type, environment, permissions, expires_at, last_used_at, request_count,
          revoked, revoked_at, rotated_to_id, grace_period_ends_at, created_at, updated_at;

-- name: GetAPIKey :one
SELECT id, name, description, key_hash, key_prefix, key_hint, scope, COALESCE(workspace_id, ''), COALESCE(owner_account_id, ''), COALESCE(workspace_ids, '{}'::text[]), created_by, on_behalf_of,
       type, environment, permissions, expires_at, last_used_at, request_count,
       revoked, revoked_at, rotated_to_id, grace_period_ends_at, created_at, updated_at
FROM api_keys WHERE id = $1;

-- name: GetAPIKeyByHash :one
SELECT id, name, description, key_hash, key_prefix, key_hint, scope, COALESCE(workspace_id, ''), COALESCE(owner_account_id, ''), COALESCE(workspace_ids, '{}'::text[]), created_by, on_behalf_of,
       type, environment, permissions, expires_at, last_used_at, request_count,
       revoked, revoked_at, rotated_to_id, grace_period_ends_at, created_at, updated_at
FROM api_keys WHERE key_hash = $1;

-- name: ListAPIKeys :many
SELECT id, name, description, key_hash, key_prefix, key_hint, scope, COALESCE(workspace_id, ''), COALESCE(owner_account_id, ''), COALESCE(workspace_ids, '{}'::text[]), created_by, on_behalf_of,
       type, environment, permissions, expires_at, last_used_at, request_count,
       revoked, revoked_at, rotated_to_id, grace_period_ends_at, created_at, updated_at
FROM api_keys
WHERE
  (
    CASE
      WHEN $1::text = '' THEN TRUE
      WHEN scope = 'workspace_system' THEN workspace_id = $1
      ELSE cardinality(COALESCE(workspace_ids, '{}'::text[])) = 0 OR $1 = ANY(COALESCE(workspace_ids, '{}'::text[]))
    END
  )
  AND ($2::text = '' OR owner_account_id = $2)
  AND ($3::text = '' OR scope = $3)
  AND id >= $4
  AND revoked = FALSE
ORDER BY id ASC
LIMIT $5;

-- name: ListAPIKeysIncludeRevoked :many
SELECT id, name, description, key_hash, key_prefix, key_hint, scope, COALESCE(workspace_id, ''), COALESCE(owner_account_id, ''), COALESCE(workspace_ids, '{}'::text[]), created_by, on_behalf_of,
       type, environment, permissions, expires_at, last_used_at, request_count,
       revoked, revoked_at, rotated_to_id, grace_period_ends_at, created_at, updated_at
FROM api_keys
WHERE
  (
    CASE
      WHEN $1::text = '' THEN TRUE
      WHEN scope = 'workspace_system' THEN workspace_id = $1
      ELSE cardinality(COALESCE(workspace_ids, '{}'::text[])) = 0 OR $1 = ANY(COALESCE(workspace_ids, '{}'::text[]))
    END
  )
  AND ($2::text = '' OR owner_account_id = $2)
  AND ($3::text = '' OR scope = $3)
  AND id >= $4
ORDER BY id ASC
LIMIT $5;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET revoked = TRUE, revoked_at = NOW() WHERE id = $1;

-- name: UpdateAPIKey :one
UPDATE api_keys
SET name = $2, description = $3, permissions = $4, workspace_ids = $5
WHERE id = $1
RETURNING id, name, description, key_hash, key_prefix, key_hint, scope, COALESCE(workspace_id, ''), COALESCE(owner_account_id, ''), COALESCE(workspace_ids, '{}'::text[]), created_by, on_behalf_of,
          type, environment, permissions, expires_at, last_used_at, request_count,
          revoked, revoked_at, rotated_to_id, grace_period_ends_at, created_at, updated_at;

-- name: SetAPIKeyRotated :exec
UPDATE api_keys SET rotated_to_id = $2, grace_period_ends_at = $3, revoked = TRUE, revoked_at = NOW() WHERE id = $1;

-- name: UpdateAPIKeyUsage :exec
UPDATE api_keys SET last_used_at = NOW(), request_count = request_count + 1 WHERE id = $1;
