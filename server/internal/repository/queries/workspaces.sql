-- name: CreateWorkspace :one
INSERT INTO workspaces (
    id, name, domain, email_domain, description,
    icon_image_original, icon_image_34, icon_image_44,
    discoverability, default_channels, preferences, profile_fields,
    billing_plan, billing_status, billing_email
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING id, name, domain, email_domain, description,
          icon_image_original, icon_image_34, icon_image_44,
          discoverability, default_channels, preferences, profile_fields,
          billing_plan, billing_status, billing_email, created_at, updated_at;

-- name: GetWorkspace :one
SELECT id, name, domain, email_domain, description,
       icon_image_original, icon_image_34, icon_image_44,
       discoverability, default_channels, preferences, profile_fields,
       billing_plan, billing_status, billing_email, created_at, updated_at
FROM workspaces
WHERE id = $1;

-- name: ListWorkspaces :many
SELECT id, name, domain, email_domain, description,
       icon_image_original, icon_image_34, icon_image_44,
       discoverability, default_channels, preferences, profile_fields,
       billing_plan, billing_status, billing_email, created_at, updated_at
FROM workspaces
ORDER BY created_at ASC, id ASC;

-- name: UpdateWorkspace :one
UPDATE workspaces
SET name = $2,
    domain = $3,
    email_domain = $4,
    description = $5,
    icon_image_original = $6,
    icon_image_34 = $7,
    icon_image_44 = $8,
    discoverability = $9,
    default_channels = $10,
    preferences = $11,
    profile_fields = $12,
    billing_plan = $13,
    billing_status = $14,
    billing_email = $15
WHERE id = $1
RETURNING id, name, domain, email_domain, description,
          icon_image_original, icon_image_34, icon_image_44,
          discoverability, default_channels, preferences, profile_fields,
          billing_plan, billing_status, billing_email, created_at, updated_at;

-- name: ListWorkspaceAdmins :many
SELECT id, workspace_id, name, real_name, display_name, email,
       principal_type, owner_id, is_bot, account_type,
       deleted, profile, created_at, updated_at
FROM users
WHERE workspace_id = $1 AND deleted = FALSE AND account_type IN ('primary_admin', 'admin')
ORDER BY name ASC, id ASC;

-- name: ListWorkspaceOwners :many
SELECT id, workspace_id, name, real_name, display_name, email,
       principal_type, owner_id, is_bot, account_type,
       deleted, profile, created_at, updated_at
FROM users
WHERE workspace_id = $1 AND deleted = FALSE AND account_type = 'primary_admin'
ORDER BY name ASC, id ASC;

-- name: ListWorkspaceBillableInfo :many
SELECT id, (NOT is_bot AND NOT deleted) AS billing_active
FROM users
WHERE workspace_id = $1
ORDER BY id ASC;

-- name: ListWorkspaceAccessLogs :many
SELECT se.actor_id,
       COALESCE(NULLIF(u.name, ''), se.actor_id) AS username,
       se.event_type,
       MIN(se.created_at) AS date_first,
       MAX(se.created_at) AS date_last
FROM internal_events se
LEFT JOIN users u ON u.id = se.actor_id
WHERE se.workspace_id = $1
  AND se.actor_id <> ''
  AND se.aggregate_type IN ('token', 'api_key')
GROUP BY se.actor_id, COALESCE(NULLIF(u.name, ''), se.actor_id), se.event_type
ORDER BY MAX(se.created_at) DESC
LIMIT $2;

-- name: ListWorkspaceIntegrationLogs :many
SELECT se.aggregate_id,
       CASE
           WHEN se.aggregate_type = 'event_subscription' THEN 'webhook'
           ELSE se.aggregate_type
       END AS app_type,
       CASE
           WHEN se.aggregate_type = 'event_subscription' THEN 'event_subscription'
           ELSE se.aggregate_type
       END AS app_name,
       se.actor_id,
       COALESCE(NULLIF(u.name, ''), se.actor_id) AS user_name,
       se.event_type,
       se.created_at
FROM internal_events se
LEFT JOIN users u ON u.id = se.actor_id
WHERE se.workspace_id = $1
  AND se.aggregate_type IN ('api_key', 'event_subscription')
ORDER BY se.created_at DESC
LIMIT $2;

-- name: ListWorkspaceExternalWorkspaces :many
SELECT id, external_workspace_id, external_workspace_name, connection_type,
       connected, created_at, disconnected_at
FROM workspace_external_workspaces
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC;

-- name: DisconnectWorkspaceExternalWorkspace :execrows
UPDATE workspace_external_workspaces
SET connected = FALSE, disconnected_at = NOW()
WHERE workspace_id = $1 AND external_workspace_id = $2 AND connected = TRUE;
