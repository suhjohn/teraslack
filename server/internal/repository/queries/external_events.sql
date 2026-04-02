-- name: CreateExternalEvent :one
INSERT INTO external_events (
    workspace_id, type, resource_type, resource_id, occurred_at, payload,
    source_internal_event_id, source_internal_event_ids, dedupe_key
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id, dedupe_key) DO UPDATE SET
    workspace_id = external_events.workspace_id
RETURNING id, workspace_id, type, resource_type, resource_id, occurred_at, payload,
          source_internal_event_id, source_internal_event_ids, dedupe_key, created_at;

-- name: RecordExternalEventProjectionFailure :exec
INSERT INTO external_event_projection_failures (internal_event_id, error)
VALUES ($1, $2)
ON CONFLICT (internal_event_id) DO UPDATE SET
    error = EXCLUDED.error,
    created_at = NOW();

-- name: GetExternalEventsSince :many
SELECT id, workspace_id, type, resource_type, resource_id, occurred_at, payload,
       source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
FROM external_events
WHERE id > $1
ORDER BY id ASC
LIMIT $2;

-- name: ListAllExternalEvents :many
SELECT id, workspace_id, type, resource_type, resource_id, occurred_at, payload,
       source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
FROM external_events
ORDER BY id ASC;

-- name: ListVisibleExternalEventsByWorkspaceAndResourceTypes :many
SELECT id, workspace_id, type, resource_type, resource_id, occurred_at, payload,
       source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
FROM external_events
WHERE workspace_id = $1
  AND id > $2
  AND resource_type = ANY($3::text[])
  AND (sqlc.narg(event_type)::text IS NULL OR type = sqlc.narg(event_type))
  AND (sqlc.narg(resource_id)::text IS NULL OR resource_id = sqlc.narg(resource_id))
ORDER BY id ASC
LIMIT $4;

-- name: ListVisibleConversationExternalEventsByExternalMember :many
SELECT DISTINCT ee.id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload,
       ee.source_internal_event_id, ee.source_internal_event_ids, ee.dedupe_key, ee.created_at
FROM external_events ee
JOIN conversation_event_feed cef ON cef.external_event_id = ee.id
JOIN external_members em ON em.conversation_id = cef.conversation_id
WHERE em.account_id = $1
  AND em.host_workspace_id = $2
  AND ee.workspace_id = $2
  AND ee.id > $3
  AND em.revoked_at IS NULL
  AND (em.expires_at IS NULL OR em.expires_at > NOW())
  AND (sqlc.narg(event_type)::text IS NULL OR ee.type = sqlc.narg(event_type))
  AND (sqlc.narg(conversation_id)::text IS NULL OR cef.conversation_id = sqlc.narg(conversation_id))
ORDER BY ee.id ASC
LIMIT $4;

-- name: ListVisibleFileExternalEventsByExternalMember :many
SELECT DISTINCT ee.id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload,
       ee.source_internal_event_id, ee.source_internal_event_ids, ee.dedupe_key, ee.created_at
FROM external_events ee
JOIN file_event_feed fef ON fef.external_event_id = ee.id
JOIN file_channels fc ON fc.file_id = fef.file_id
JOIN external_members em ON em.conversation_id = fc.channel_id
WHERE em.account_id = $1
  AND em.host_workspace_id = $2
  AND ee.workspace_id = $2
  AND ee.id > $3
  AND em.revoked_at IS NULL
  AND (em.expires_at IS NULL OR em.expires_at > NOW())
  AND (sqlc.narg(event_type)::text IS NULL OR ee.type = sqlc.narg(event_type))
  AND (sqlc.narg(file_id)::text IS NULL OR fef.file_id = sqlc.narg(file_id))
ORDER BY ee.id ASC
LIMIT $4;

-- name: ListVisibleExternalEventsByExternalMemberAndResourceTypes :many
SELECT DISTINCT ee.id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload,
       ee.source_internal_event_id, ee.source_internal_event_ids, ee.dedupe_key, ee.created_at
FROM external_events ee
LEFT JOIN conversation_event_feed cef
  ON cef.external_event_id = ee.id AND ee.resource_type = 'conversation'
LEFT JOIN external_members em_conv
  ON em_conv.account_id = $1
 AND em_conv.host_workspace_id = $2
 AND em_conv.conversation_id = cef.conversation_id
 AND em_conv.revoked_at IS NULL
 AND (em_conv.expires_at IS NULL OR em_conv.expires_at > NOW())
LEFT JOIN file_event_feed fef
  ON fef.external_event_id = ee.id AND ee.resource_type = 'file'
LEFT JOIN file_channels fc ON fc.file_id = fef.file_id
LEFT JOIN external_members em_file
  ON em_file.account_id = $1
 AND em_file.host_workspace_id = $2
 AND em_file.conversation_id = fc.channel_id
 AND em_file.revoked_at IS NULL
 AND (em_file.expires_at IS NULL OR em_file.expires_at > NOW())
WHERE ee.workspace_id = $2
  AND ee.id > $3
  AND ee.resource_type = ANY($4::text[])
  AND (sqlc.narg(event_type)::text IS NULL OR ee.type = sqlc.narg(event_type))
  AND (
    (ee.resource_type = 'conversation' AND em_conv.id IS NOT NULL) OR
    (ee.resource_type = 'file' AND em_file.id IS NOT NULL)
  )
ORDER BY ee.id ASC
LIMIT $5;

-- name: InsertWorkspaceEventFeed :exec
INSERT INTO workspace_event_feed (workspace_id, external_event_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: InsertConversationEventFeed :exec
INSERT INTO conversation_event_feed (conversation_id, external_event_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: InsertFileEventFeed :exec
INSERT INTO file_event_feed (file_id, external_event_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: InsertUserEventFeed :exec
INSERT INTO user_event_feed (user_id, external_event_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: TruncateExternalEventsAndFeeds :exec
TRUNCATE user_event_feed, file_event_feed,
         conversation_event_feed, workspace_event_feed, external_events
         RESTART IDENTITY CASCADE;

-- name: TruncateExternalEventFeeds :exec
TRUNCATE user_event_feed, file_event_feed,
         conversation_event_feed, workspace_event_feed RESTART IDENTITY;
