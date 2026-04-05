-- name: CreateConversation :one
INSERT INTO conversations (
    id, workspace_id, name, type, creator_id,
    owner_type, owner_account_id, owner_workspace_id,
    topic_value, topic_creator, purpose_value, purpose_creator,
    last_message_ts, last_activity_ts
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(workspace_id),
    sqlc.arg(name),
    sqlc.arg(type),
    sqlc.arg(creator_id),
    sqlc.arg(owner_type),
    sqlc.arg(owner_account_id),
    sqlc.arg(owner_workspace_id),
    sqlc.arg(topic_value),
    sqlc.arg(topic_creator),
    sqlc.arg(purpose_value),
    sqlc.arg(purpose_creator),
    sqlc.arg(last_message_ts),
    sqlc.arg(last_activity_ts)
)
RETURNING id, workspace_id, owner_type, owner_account_id, owner_workspace_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: GetConversation :one
SELECT id, workspace_id, owner_type, owner_account_id, owner_workspace_id, name, type, creator_id, is_archived,
       topic_value, topic_creator, topic_last_set,
       purpose_value, purpose_creator, purpose_last_set,
       num_members, last_message_ts, last_activity_ts, created_at, updated_at
FROM conversations WHERE id = $1;

-- name: UpdateConversation :one
UPDATE conversations SET name = $2, is_archived = $3
WHERE id = $1
RETURNING id, workspace_id, owner_type, owner_account_id, owner_workspace_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: SetConversationTopic :one
UPDATE conversations SET topic_value = $2, topic_creator = $3, topic_last_set = NOW()
WHERE id = $1
RETURNING id, workspace_id, owner_type, owner_account_id, owner_workspace_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: SetConversationPurpose :one
UPDATE conversations SET purpose_value = $2, purpose_creator = $3, purpose_last_set = NOW()
WHERE id = $1
RETURNING id, workspace_id, owner_type, owner_account_id, owner_workspace_id, name, type, creator_id, is_archived,
          topic_value, topic_creator, topic_last_set,
          purpose_value, purpose_creator, purpose_last_set,
          num_members, last_message_ts, last_activity_ts, created_at, updated_at;

-- name: ArchiveConversation :exec
UPDATE conversations SET is_archived = TRUE WHERE id = $1 AND is_archived = FALSE;

-- name: UnarchiveConversation :exec
UPDATE conversations SET is_archived = FALSE WHERE id = $1 AND is_archived = TRUE;

-- name: ListVisibleConversations :many
SELECT c.id, c.workspace_id, c.owner_type, c.owner_account_id, c.owner_workspace_id, c.name, c.type, c.creator_id, c.is_archived,
       c.topic_value, c.topic_creator, c.topic_last_set,
       c.purpose_value, c.purpose_creator, c.purpose_last_set,
       c.num_members, c.last_message_ts, c.last_activity_ts,
       COALESCE(crv2.last_read_ts, '') AS last_read_ts,
       CASE
           WHEN sqlc.arg(user_id) = '' AND sqlc.arg(account_id) = '' THEN NULL::boolean
           WHEN c.last_message_ts IS NULL THEN FALSE
           WHEN COALESCE(crv2.last_read_ts, '') = '' THEN TRUE
           ELSE COALESCE(crv2.last_read_ts, '') < c.last_message_ts
       END AS has_unread,
       c.created_at, c.updated_at
FROM conversations c
LEFT JOIN conversation_members_v2 cmv2
  ON cmv2.conversation_id = c.id AND cmv2.account_id = sqlc.arg(account_id)
LEFT JOIN workspace_memberships wm
  ON wm.workspace_id = c.owner_workspace_id
 AND wm.account_id = sqlc.arg(account_id)
 AND wm.status = 'active'
LEFT JOIN conversation_reads_v2 crv2
  ON crv2.conversation_id = c.id AND crv2.account_id = sqlc.arg(account_id)
WHERE (sqlc.arg(exclude_archived)::bool = FALSE OR c.is_archived = FALSE)
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR c.type = ANY(sqlc.arg(types)::text[]))
  AND (
    sqlc.arg(cursor_activity) = ''
    OR (COALESCE(c.last_activity_ts, ''), c.id) < (sqlc.arg(cursor_activity), sqlc.arg(cursor_id))
  )
  AND (
    (
      COALESCE(c.owner_type, 'workspace') = 'account'
      AND sqlc.arg(account_id) <> ''
      AND (
        c.owner_account_id = sqlc.arg(account_id)
        OR cmv2.account_id IS NOT NULL
      )
    )
    OR (
      COALESCE(c.owner_type, 'workspace') = 'workspace'
      AND c.owner_workspace_id = sqlc.arg(workspace_id)
      AND (
        (sqlc.arg(user_id) = '' AND sqlc.arg(account_id) = '')
        OR (
          sqlc.arg(account_id) <> ''
          AND wm.id IS NOT NULL
          AND (
            cmv2.account_id IS NOT NULL
            OR (
              c.type = 'public_channel'
              AND (
                COALESCE(wm.membership_kind, 'full') <> 'guest'
                OR COALESCE(wm.guest_scope, 'single_conversation') = 'workspace_full'
              )
            )
          )
        )
      )
    )
  )
ORDER BY COALESCE(c.last_activity_ts, '') DESC, c.id DESC
LIMIT sqlc.arg(limit_count);

-- name: AddConversationMember :execrows
INSERT INTO conversation_members (conversation_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: AddConversationMemberByAccount :execrows
INSERT INTO conversation_members (conversation_id, user_id)
SELECT c.id, u.id
FROM conversations c
JOIN users u
  ON u.workspace_id = c.owner_workspace_id
 AND u.account_id = $2
WHERE c.id = $1
ON CONFLICT DO NOTHING;

-- name: AddConversationMemberV2ByUser :execrows
INSERT INTO conversation_members_v2 (conversation_id, account_id, membership_role, added_by_account_id)
SELECT sqlc.arg(conversation_id), u.account_id, 'member', added_by.account_id
FROM users u
LEFT JOIN users added_by ON added_by.id = sqlc.arg(added_by_user_id)
WHERE u.id = sqlc.arg(user_id)
  AND u.account_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- name: AddConversationMemberV2 :execrows
INSERT INTO conversation_members_v2 (conversation_id, account_id, membership_role, added_by_account_id)
VALUES ($1, $2, 'member', NULLIF($3, ''))
ON CONFLICT DO NOTHING;

-- name: RemoveConversationMember :execrows
DELETE FROM conversation_members WHERE conversation_id = $1 AND user_id = $2;

-- name: RemoveConversationMemberByAccount :execrows
DELETE FROM conversation_members cm
USING conversations c, users u
WHERE cm.conversation_id = $1
  AND c.id = cm.conversation_id
  AND u.workspace_id = c.owner_workspace_id
  AND u.account_id = $2
  AND cm.user_id = u.id;

-- name: RemoveConversationMemberV2ByUser :execrows
DELETE FROM conversation_members_v2 cmv2
USING users u
WHERE cmv2.conversation_id = sqlc.arg(conversation_id)
  AND u.id = sqlc.arg(user_id)
  AND cmv2.account_id = u.account_id;

-- name: RemoveConversationMemberV2 :execrows
DELETE FROM conversation_members_v2
WHERE conversation_id = $1 AND account_id = $2;

-- name: IsConversationAccountMember :one
SELECT EXISTS(SELECT 1 FROM conversation_members_v2 WHERE conversation_id = $1 AND account_id = $2);

-- name: ListConversationMembersV2 :many
SELECT conversation_id, account_id, membership_role, added_by_account_id, created_at
FROM conversation_members_v2
WHERE conversation_id = $1 AND account_id >= $2
ORDER BY account_id ASC
LIMIT $3;

-- name: LockConversationForUpdate :one
SELECT id FROM conversations WHERE id = $1 FOR UPDATE;

-- name: IncrementConversationMemberCount :exec
UPDATE conversations
SET num_members = num_members + 1
WHERE id = $1;

-- name: DecrementConversationMemberCount :exec
UPDATE conversations
SET num_members = num_members - 1
WHERE id = $1;

-- name: DeleteCanonicalDMByConversation :execrows
DELETE FROM canonical_dms
WHERE conversation_id = $1;

-- name: UpdateConversationLastMessageAndActivity :exec
UPDATE conversations
SET last_message_ts = CASE
        WHEN last_message_ts IS NULL OR last_message_ts < sqlc.arg(ts) THEN sqlc.arg(ts)
        ELSE last_message_ts
    END,
    last_activity_ts = CASE
        WHEN last_activity_ts IS NULL OR last_activity_ts < sqlc.arg(ts) THEN sqlc.arg(ts)
        ELSE last_activity_ts
    END
WHERE id = sqlc.arg(id);

-- name: UpdateConversationLastActivity :exec
UPDATE conversations
SET last_activity_ts = CASE
        WHEN last_activity_ts IS NULL OR last_activity_ts < sqlc.arg(ts) THEN sqlc.arg(ts)
        ELSE last_activity_ts
    END
WHERE id = sqlc.arg(id);

-- name: GetCanonicalDMConversation :one
SELECT c.id, c.workspace_id, c.owner_type, c.owner_account_id, c.owner_workspace_id, c.name, c.type, c.creator_id, c.is_archived,
       c.topic_value, c.topic_creator, c.topic_last_set,
       c.purpose_value, c.purpose_creator, c.purpose_last_set,
       c.num_members, c.last_message_ts, c.last_activity_ts, c.created_at, c.updated_at
FROM canonical_dms d
JOIN conversations c ON c.id = d.conversation_id
WHERE d.workspace_id = sqlc.arg(workspace_id)
  AND d.user_low_id = sqlc.arg(user_low_id)
  AND d.user_high_id = sqlc.arg(user_high_id);

-- name: CreateCanonicalDM :execrows
INSERT INTO canonical_dms (workspace_id, user_low_id, user_high_id, conversation_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: UpsertGuestWorkspaceMembership :exec
INSERT INTO workspace_memberships (
    id, workspace_id, account_id, role, status, membership_kind, guest_scope, created_at, updated_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(account_id), 'member', 'active', 'guest', 'single_conversation', NOW(), NOW()
)
ON CONFLICT (workspace_id, account_id) DO NOTHING;

-- name: UpsertWorkspaceProfileFromAccount :exec
INSERT INTO workspace_profiles (
    workspace_id, account_id, name, real_name, display_name, profile, created_at, updated_at
)
SELECT
    sqlc.arg(workspace_id),
    a.id,
    COALESCE(NULLIF(split_part(a.email, '@', 1), ''), a.id),
    ''::text,
    ''::text,
    '{}'::jsonb,
    NOW(),
    NOW()
FROM accounts a
WHERE a.id = sqlc.arg(account_id)
ON CONFLICT (workspace_id, account_id) DO NOTHING;

-- name: UpsertWorkspaceMembershipConversationAccess :exec
INSERT INTO workspace_membership_conversation_access (workspace_membership_id, conversation_id, created_at)
VALUES (sqlc.arg(workspace_membership_id), sqlc.arg(conversation_id), NOW())
ON CONFLICT (workspace_membership_id, conversation_id) DO NOTHING;
