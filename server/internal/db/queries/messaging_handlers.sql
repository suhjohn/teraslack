-- name: CreateConversation :exec
insert into conversations (
  id,
  workspace_id,
  access_policy,
  title,
  description,
  created_by_user_id,
  created_at,
  updated_at
) values (
  sqlc.arg(id),
  sqlc.narg(workspace_id),
  sqlc.arg(access_policy),
  sqlc.narg(title),
  sqlc.narg(description),
  sqlc.arg(created_by_user_id),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: GetConversationPair :one
select conversation_id
from conversation_pairs
where first_user_id = sqlc.arg(first_user_id)
  and second_user_id = sqlc.arg(second_user_id);

-- name: CreateConversationParticipant :exec
insert into conversation_participants (conversation_id, user_id, added_by_user_id, joined_at)
values (
  sqlc.arg(conversation_id),
  sqlc.arg(user_id),
  sqlc.narg(added_by_user_id),
  sqlc.arg(joined_at)
);

-- name: CreateConversationParticipantIfMissing :execrows
insert into conversation_participants (conversation_id, user_id, added_by_user_id, joined_at)
values (
  sqlc.arg(conversation_id),
  sqlc.arg(user_id),
  sqlc.narg(added_by_user_id),
  sqlc.arg(joined_at)
)
on conflict do nothing;

-- name: CreateConversationPair :exec
insert into conversation_pairs (conversation_id, first_user_id, second_user_id)
values (
  sqlc.arg(conversation_id),
  sqlc.arg(first_user_id),
  sqlc.arg(second_user_id)
);

-- name: UpdateConversationDetails :exec
update conversations
set title = coalesce(sqlc.narg(title), title),
    description = coalesce(sqlc.narg(description), description),
    archived_at = sqlc.narg(archived_at),
    updated_at = sqlc.arg(updated_at)
where id = sqlc.arg(id);

-- name: ListConversationParticipants :many
select u.id, u.principal_type, u.status, u.email, p.handle, p.display_name, p.avatar_url, p.bio
from conversation_participants cp
join users u on u.id = cp.user_id
join user_profiles p on p.user_id = u.id
where cp.conversation_id = sqlc.arg(conversation_id)
order by p.display_name asc;

-- name: CountConversationParticipants :one
select count(*)::int as count
from conversation_participants
where conversation_id = sqlc.arg(conversation_id);

-- name: DeleteConversationParticipant :execrows
delete from conversation_participants
where conversation_id = sqlc.arg(conversation_id)
  and user_id = sqlc.arg(user_id);

-- name: CreateConversationInvite :exec
insert into conversation_invites (
  id,
  conversation_id,
  created_by_user_id,
  token_hash,
  encrypted_token,
  expires_at,
  mode,
  allowed_user_ids,
  allowed_emails,
  created_at
) values (
  sqlc.arg(id),
  sqlc.arg(conversation_id),
  sqlc.arg(created_by_user_id),
  sqlc.arg(token_hash),
  sqlc.narg(encrypted_token),
  sqlc.narg(expires_at),
  sqlc.arg(mode),
  sqlc.arg(allowed_user_ids),
  sqlc.arg(allowed_emails),
  sqlc.arg(created_at)
);

-- name: GetActiveConversationInvite :one
select id, conversation_id, encrypted_token, created_at
from conversation_invites
where conversation_id = sqlc.arg(conversation_id)
  and revoked_at is null;

-- name: GetActiveConversationInviteForUpdate :one
select id, conversation_id, encrypted_token, created_at
from conversation_invites
where conversation_id = sqlc.arg(conversation_id)
  and revoked_at is null
for update;

-- name: RevokeActiveConversationInvite :execrows
update conversation_invites
set revoked_at = sqlc.arg(revoked_at)
where conversation_id = sqlc.arg(conversation_id)
  and revoked_at is null;

-- name: GetConversationInviteByTokenHashForUpdate :one
select id, conversation_id, mode, allowed_user_ids, allowed_emails, expires_at, revoked_at
from conversation_invites
where token_hash = sqlc.arg(token_hash)
for update;

-- name: CreateMessage :exec
insert into messages (id, conversation_id, author_user_id, body_text, body_rich, metadata, created_at)
values (
  sqlc.arg(id),
  sqlc.arg(conversation_id),
  sqlc.arg(author_user_id),
  sqlc.arg(body_text),
  sqlc.arg(body_rich),
  sqlc.arg(metadata),
  sqlc.arg(created_at)
);

-- name: TouchConversationLastMessage :exec
update conversations
set last_message_at = sqlc.arg(updated_at),
    updated_at = sqlc.arg(updated_at)
where id = sqlc.arg(id);

-- name: UpdateMessageContent :exec
update messages
set body_text = sqlc.arg(body_text),
    body_rich = sqlc.arg(body_rich),
    metadata = sqlc.arg(metadata),
    edited_at = sqlc.arg(edited_at)
where id = sqlc.arg(id);

-- name: SoftDeleteMessage :exec
update messages
set deleted_at = sqlc.arg(deleted_at)
where id = sqlc.arg(id);

-- name: MessageExistsInConversation :one
select exists(
  select 1
  from messages
  where id = sqlc.arg(message_id)
    and conversation_id = sqlc.arg(conversation_id)
);

-- name: UpsertConversationRead :exec
insert into conversation_reads (conversation_id, user_id, last_read_message_id, last_read_at, updated_at)
values (
  sqlc.arg(conversation_id),
  sqlc.arg(user_id),
  sqlc.arg(last_read_message_id),
  sqlc.arg(last_read_at),
  sqlc.arg(updated_at)
)
on conflict (conversation_id, user_id)
do update set
  last_read_message_id = excluded.last_read_message_id,
  last_read_at = excluded.last_read_at,
  updated_at = excluded.updated_at;

-- name: ListEventSubscriptionsByOwner :many
select id, workspace_id, url, enabled, event_type, resource_type, resource_id, created_at, updated_at
from event_subscriptions
where owner_user_id = sqlc.arg(owner_user_id)
  and (sqlc.narg(workspace_id)::uuid is null or workspace_id = sqlc.narg(workspace_id))
order by created_at desc;

-- name: CreateEventSubscription :exec
insert into event_subscriptions (
  id,
  owner_user_id,
  workspace_id,
  url,
  enabled,
  encrypted_secret,
  event_type,
  resource_type,
  resource_id,
  created_at,
  updated_at
) values (
  sqlc.arg(id),
  sqlc.arg(owner_user_id),
  sqlc.narg(workspace_id),
  sqlc.arg(url),
  true,
  sqlc.arg(encrypted_secret),
  sqlc.narg(event_type),
  sqlc.narg(resource_type),
  sqlc.narg(resource_id),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: GetEventSubscriptionWorkspaceForOwnerForUpdate :one
select workspace_id
from event_subscriptions
where id = sqlc.arg(id)
  and owner_user_id = sqlc.arg(owner_user_id)
for update;

-- name: UpdateEventSubscriptionEnabledByOwner :execrows
update event_subscriptions
set enabled = sqlc.arg(enabled),
    updated_at = sqlc.arg(updated_at)
where id = sqlc.arg(id)
  and owner_user_id = sqlc.arg(owner_user_id);

-- name: GetEventSubscriptionByIDAndOwner :one
select id, workspace_id, url, enabled, event_type, resource_type, resource_id, created_at, updated_at
from event_subscriptions
where id = sqlc.arg(id)
  and owner_user_id = sqlc.arg(owner_user_id)
  and (sqlc.narg(workspace_id)::uuid is null or workspace_id = sqlc.narg(workspace_id));

-- name: DeleteEventSubscriptionByOwner :execrows
delete from event_subscriptions
where id = sqlc.arg(id)
  and owner_user_id = sqlc.arg(owner_user_id);
