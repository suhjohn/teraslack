-- Projector queries: used by the event-sourcing projector to rebuild projection
-- tables from the event log. These are upsert/delete operations that replay
-- events into the read-side tables.

-- name: ProjectorUpsertUser :exec
INSERT INTO users (id, team_id, name, real_name, display_name, email, is_bot, account_type, deleted, profile, principal_type, owner_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (id) DO UPDATE SET
    team_id = EXCLUDED.team_id, name = EXCLUDED.name, real_name = EXCLUDED.real_name,
    display_name = EXCLUDED.display_name, email = EXCLUDED.email, is_bot = EXCLUDED.is_bot,
    account_type = EXCLUDED.account_type, deleted = EXCLUDED.deleted, profile = EXCLUDED.profile,
    principal_type = EXCLUDED.principal_type, owner_id = EXCLUDED.owner_id,
    updated_at = EXCLUDED.updated_at;

-- name: ProjectorMarkUserDeleted :exec
UPDATE users SET deleted = TRUE, updated_at = $2 WHERE id = $1;

-- name: ProjectorUpsertConversation :exec
INSERT INTO conversations (id, team_id, name, type, creator_id, is_archived,
    topic_value, topic_creator, topic_last_set,
    purpose_value, purpose_creator, purpose_last_set,
    num_members, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (id) DO UPDATE SET
    team_id = EXCLUDED.team_id, name = EXCLUDED.name, type = EXCLUDED.type,
    creator_id = EXCLUDED.creator_id, is_archived = EXCLUDED.is_archived,
    topic_value = EXCLUDED.topic_value, topic_creator = EXCLUDED.topic_creator,
    topic_last_set = EXCLUDED.topic_last_set,
    purpose_value = EXCLUDED.purpose_value, purpose_creator = EXCLUDED.purpose_creator,
    purpose_last_set = EXCLUDED.purpose_last_set,
    num_members = EXCLUDED.num_members, updated_at = EXCLUDED.updated_at;

-- name: ProjectorUpsertMember :exec
INSERT INTO conversation_members (conversation_id, user_id, joined_at)
VALUES ($1, $2, $3)
ON CONFLICT (conversation_id, user_id) DO NOTHING;

-- name: ProjectorDeleteMember :exec
DELETE FROM conversation_members WHERE conversation_id = $1 AND user_id = $2;

-- name: ProjectorUpsertConversationManager :exec
INSERT INTO conversation_manager_assignments (conversation_id, user_id, assigned_by, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id, user_id) DO NOTHING;

-- name: ProjectorDeleteConversationManager :exec
DELETE FROM conversation_manager_assignments WHERE conversation_id = $1 AND user_id = $2;

-- name: ProjectorUpsertConversationPostingPolicy :exec
INSERT INTO conversation_posting_policies (conversation_id, policy_type, policy_json, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (conversation_id) DO UPDATE SET
    policy_type = EXCLUDED.policy_type,
    policy_json = EXCLUDED.policy_json,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at;

-- name: ProjectorUpsertMessage :exec
INSERT INTO messages (ts, channel_id, user_id, text, thread_ts, type, subtype,
    blocks, metadata, edited_by, edited_at, reply_count, reply_users_count,
    latest_reply, is_deleted, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
ON CONFLICT (channel_id, ts) DO UPDATE SET
    user_id = EXCLUDED.user_id, text = EXCLUDED.text, thread_ts = EXCLUDED.thread_ts,
    type = EXCLUDED.type, subtype = EXCLUDED.subtype, blocks = EXCLUDED.blocks,
    metadata = EXCLUDED.metadata, edited_by = EXCLUDED.edited_by,
    edited_at = EXCLUDED.edited_at, reply_count = EXCLUDED.reply_count,
    reply_users_count = EXCLUDED.reply_users_count, latest_reply = EXCLUDED.latest_reply,
    is_deleted = EXCLUDED.is_deleted, updated_at = EXCLUDED.updated_at;

-- name: ProjectorMarkMessageDeleted :exec
UPDATE messages SET is_deleted = TRUE, updated_at = $3 WHERE channel_id = $1 AND ts = $2;

-- name: ProjectorUpsertReaction :exec
INSERT INTO reactions (channel_id, message_ts, user_id, emoji, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (channel_id, message_ts, user_id, emoji) DO NOTHING;

-- name: ProjectorDeleteReaction :exec
DELETE FROM reactions WHERE channel_id = $1 AND message_ts = $2 AND user_id = $3 AND emoji = $4;

-- name: ProjectorUpsertUsergroup :exec
INSERT INTO usergroups (id, team_id, name, handle, description, is_external, enabled, user_count, created_by, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    team_id = EXCLUDED.team_id, name = EXCLUDED.name, handle = EXCLUDED.handle,
    description = EXCLUDED.description, is_external = EXCLUDED.is_external,
    enabled = EXCLUDED.enabled, user_count = EXCLUDED.user_count,
    updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at;

-- name: ProjectorDeleteUsergroupMembers :exec
DELETE FROM usergroup_members WHERE usergroup_id = $1;

-- name: ProjectorUpsertUsergroupMember :exec
INSERT INTO usergroup_members (usergroup_id, user_id, added_at)
VALUES ($1, $2, $3)
ON CONFLICT (usergroup_id, user_id) DO NOTHING;

-- name: ProjectorUpsertPin :exec
INSERT INTO pins (channel_id, message_ts, pinned_by, pinned_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id, message_ts) DO NOTHING;

-- name: ProjectorDeletePin :exec
DELETE FROM pins WHERE channel_id = $1 AND message_ts = $2;

-- name: ProjectorUpsertBookmark :exec
INSERT INTO bookmarks (id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
    channel_id = EXCLUDED.channel_id, title = EXCLUDED.title, type = EXCLUDED.type,
    link = EXCLUDED.link, emoji = EXCLUDED.emoji,
    updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at;

-- name: ProjectorDeleteBookmark :exec
DELETE FROM bookmarks WHERE id = $1;

-- name: ProjectorUpsertFile :exec
INSERT INTO files (id, team_id, name, title, mimetype, filetype, size, user_id, s3_key,
    url_private, url_private_download, permalink, is_external, external_url, upload_complete, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '', $9, $10, $11, $12, $13, TRUE, $14, $15)
ON CONFLICT (id) DO UPDATE SET
    team_id = EXCLUDED.team_id, name = EXCLUDED.name, title = EXCLUDED.title, mimetype = EXCLUDED.mimetype,
    filetype = EXCLUDED.filetype, size = EXCLUDED.size, user_id = EXCLUDED.user_id,
    url_private = EXCLUDED.url_private, url_private_download = EXCLUDED.url_private_download,
    permalink = EXCLUDED.permalink, is_external = EXCLUDED.is_external,
    external_url = EXCLUDED.external_url, updated_at = EXCLUDED.updated_at;

-- name: ProjectorDeleteFile :exec
DELETE FROM files WHERE id = $1;

-- name: ProjectorUpsertFileChannel :exec
INSERT INTO file_channels (file_id, channel_id, shared_at)
VALUES ($1, $2, $3)
ON CONFLICT (file_id, channel_id) DO NOTHING;

-- name: ProjectorUpsertSubscription :exec
INSERT INTO event_subscriptions (id, team_id, url, event_type, resource_type, resource_id, encrypted_secret, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
    team_id = EXCLUDED.team_id, url = EXCLUDED.url, event_type = EXCLUDED.event_type,
    resource_type = EXCLUDED.resource_type, resource_id = EXCLUDED.resource_id,
    encrypted_secret = EXCLUDED.encrypted_secret,
    enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at;

-- name: ProjectorDeleteSubscription :exec
DELETE FROM event_subscriptions WHERE id = $1;

-- name: ProjectorUpsertAPIKey :exec
INSERT INTO api_keys (id, name, description, key_hash, key_prefix, key_hint,
    team_id, principal_id, created_by, on_behalf_of, type, environment,
    permissions, expires_at, last_used_at, request_count, revoked, revoked_at,
    rotated_to_id, grace_period_ends_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name, description = EXCLUDED.description,
    key_hash = EXCLUDED.key_hash, key_prefix = EXCLUDED.key_prefix, key_hint = EXCLUDED.key_hint,
    team_id = EXCLUDED.team_id, principal_id = EXCLUDED.principal_id,
    created_by = EXCLUDED.created_by, on_behalf_of = EXCLUDED.on_behalf_of,
    type = EXCLUDED.type, environment = EXCLUDED.environment,
    permissions = EXCLUDED.permissions, expires_at = EXCLUDED.expires_at,
    last_used_at = EXCLUDED.last_used_at, request_count = EXCLUDED.request_count,
    revoked = EXCLUDED.revoked, revoked_at = EXCLUDED.revoked_at,
    rotated_to_id = EXCLUDED.rotated_to_id, grace_period_ends_at = EXCLUDED.grace_period_ends_at,
    updated_at = EXCLUDED.updated_at;

-- name: ProjectorMarkAPIKeyRotated :exec
UPDATE api_keys SET rotated_to_id = $2, grace_period_ends_at = $3, revoked = TRUE, revoked_at = $4, updated_at = $4 WHERE id = $1;

-- name: ProjectorMarkAPIKeyRevoked :exec
UPDATE api_keys SET revoked = TRUE, revoked_at = $2, updated_at = $3 WHERE id = $1;

-- name: ProjectorGetInternalEventsByAggregateType :many
SELECT id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at
FROM internal_events
WHERE aggregate_type = $1
ORDER BY id ASC;

-- name: ProjectorGetInternalEventsSince :many
SELECT id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at
FROM internal_events
WHERE id > $1
ORDER BY id ASC;
