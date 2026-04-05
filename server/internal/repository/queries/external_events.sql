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
JOIN conversations c ON c.id = cef.conversation_id
JOIN workspace_memberships wm
  ON wm.workspace_id = $2
 AND wm.account_id = $1
 AND wm.status = 'active'
LEFT JOIN conversation_members_v2 cmv2
  ON cmv2.conversation_id = c.id
 AND cmv2.account_id = $1
LEFT JOIN workspace_membership_conversation_access wmca
  ON wmca.workspace_membership_id = wm.id
 AND wmca.conversation_id = c.id
WHERE c.owner_workspace_id = $2
  AND ee.workspace_id = $2
  AND ee.id > $3
  AND (sqlc.narg(event_type)::text IS NULL OR ee.type = sqlc.narg(event_type))
  AND (sqlc.narg(conversation_id)::text IS NULL OR cef.conversation_id = sqlc.narg(conversation_id))
  AND (
    cmv2.account_id IS NOT NULL
    OR wmca.conversation_id IS NOT NULL
    OR (
      c.type = 'public_channel'
      AND (
        wm.membership_kind <> 'guest'
        OR wm.guest_scope = 'workspace_full'
      )
    )
  )
ORDER BY ee.id ASC
LIMIT $4;

-- name: ListVisibleFileExternalEventsByExternalMember :many
SELECT DISTINCT ee.id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload,
       ee.source_internal_event_id, ee.source_internal_event_ids, ee.dedupe_key, ee.created_at
FROM external_events ee
JOIN file_event_feed fef ON fef.external_event_id = ee.id
JOIN file_channels fc ON fc.file_id = fef.file_id
JOIN conversations c ON c.id = fc.channel_id
JOIN workspace_memberships wm
  ON wm.workspace_id = $2
 AND wm.account_id = $1
 AND wm.status = 'active'
LEFT JOIN conversation_members_v2 cmv2
  ON cmv2.conversation_id = c.id
 AND cmv2.account_id = $1
LEFT JOIN workspace_membership_conversation_access wmca
  ON wmca.workspace_membership_id = wm.id
 AND wmca.conversation_id = c.id
WHERE c.owner_workspace_id = $2
  AND ee.workspace_id = $2
  AND ee.id > $3
  AND (sqlc.narg(event_type)::text IS NULL OR ee.type = sqlc.narg(event_type))
  AND (sqlc.narg(file_id)::text IS NULL OR fef.file_id = sqlc.narg(file_id))
  AND (
    cmv2.account_id IS NOT NULL
    OR wmca.conversation_id IS NOT NULL
    OR (
      c.type = 'public_channel'
      AND (
        wm.membership_kind <> 'guest'
        OR wm.guest_scope = 'workspace_full'
      )
    )
  )
ORDER BY ee.id ASC
LIMIT $4;

-- name: ListVisibleExternalEventsByExternalMemberAndResourceTypes :many
SELECT DISTINCT ee.id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload,
       ee.source_internal_event_id, ee.source_internal_event_ids, ee.dedupe_key, ee.created_at
FROM external_events ee
LEFT JOIN conversation_event_feed cef
  ON cef.external_event_id = ee.id AND ee.resource_type = 'conversation'
LEFT JOIN conversations c_conv
  ON c_conv.id = cef.conversation_id
 AND c_conv.owner_workspace_id = $2
LEFT JOIN workspace_memberships wm_conv
  ON wm_conv.workspace_id = $2
 AND wm_conv.account_id = $1
 AND wm_conv.status = 'active'
LEFT JOIN conversation_members_v2 cm_conv
  ON cm_conv.conversation_id = c_conv.id
 AND cm_conv.account_id = $1
LEFT JOIN workspace_membership_conversation_access wmca_conv
  ON wmca_conv.workspace_membership_id = wm_conv.id
 AND wmca_conv.conversation_id = c_conv.id
LEFT JOIN file_event_feed fef
  ON fef.external_event_id = ee.id AND ee.resource_type = 'file'
LEFT JOIN file_channels fc ON fc.file_id = fef.file_id
LEFT JOIN conversations c_file
  ON c_file.id = fc.channel_id
 AND c_file.owner_workspace_id = $2
LEFT JOIN workspace_memberships wm_file
  ON wm_file.workspace_id = $2
 AND wm_file.account_id = $1
 AND wm_file.status = 'active'
LEFT JOIN conversation_members_v2 cm_file
  ON cm_file.conversation_id = c_file.id
 AND cm_file.account_id = $1
LEFT JOIN workspace_membership_conversation_access wmca_file
  ON wmca_file.workspace_membership_id = wm_file.id
 AND wmca_file.conversation_id = c_file.id
WHERE ee.workspace_id = $2
  AND ee.id > $3
  AND ee.resource_type = ANY($4::text[])
  AND (sqlc.narg(event_type)::text IS NULL OR ee.type = sqlc.narg(event_type))
  AND (
    (
      ee.resource_type = 'conversation'
      AND (
        cm_conv.account_id IS NOT NULL
        OR wmca_conv.conversation_id IS NOT NULL
        OR (
          c_conv.type = 'public_channel'
          AND (
            wm_conv.membership_kind <> 'guest'
            OR wm_conv.guest_scope = 'workspace_full'
          )
        )
      )
    ) OR
    (
      ee.resource_type = 'file'
      AND (
        cm_file.account_id IS NOT NULL
        OR wmca_file.conversation_id IS NOT NULL
        OR (
          c_file.type = 'public_channel'
          AND (
            wm_file.membership_kind <> 'guest'
            OR wm_file.guest_scope = 'workspace_full'
          )
        )
      )
    )
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
