-- name: GetUser :one
select u.id, u.principal_type, u.status, u.email, p.handle, p.display_name, p.avatar_url, p.bio
from users u
join user_profiles p on p.user_id = u.id
where u.id = $1;

-- name: GetAgent :one
select user_id, owner_user_id, owner_workspace_id, mode, metadata, created_by_user_id, created_at, updated_at
from agents
where user_id = $1;

-- name: ListAgentsManagedByUser :many
select
  a.user_id,
  a.owner_user_id,
  a.owner_workspace_id,
  a.mode,
  a.metadata,
  a.created_by_user_id,
  a.created_at,
  a.updated_at,
  u.id,
  u.principal_type,
  u.status,
  u.email,
  p.handle,
  p.display_name,
  p.avatar_url,
  p.bio
from agents a
join users u on u.id = a.user_id
join user_profiles p on p.user_id = u.id
where a.owner_user_id = $1
   or exists (
    select 1
    from workspace_memberships wm
    where a.owner_workspace_id is not null
      and wm.workspace_id = a.owner_workspace_id
      and wm.user_id = $1
      and wm.status = 'active'
      and wm.role in ('owner', 'admin')
  )
order by p.display_name asc;

-- name: GetWorkspace :one
select id, slug, name, created_by_user_id, created_at, updated_at
from workspaces
where id = $1;

-- name: GetConversation :one
select
  c.id,
  c.workspace_id,
  c.access_policy,
  c.title,
  c.description,
  c.created_by_user_id,
  c.archived_at,
  c.last_message_at,
  c.created_at,
  c.updated_at,
  coalesce(pc.participant_count, 0)::int as participant_count
from conversations c
left join (
  select conversation_id, count(*)::int as participant_count
  from conversation_participants
  group by conversation_id
) pc on pc.conversation_id = c.id
where c.id = $1;

-- name: GetWorkspaceMembership :one
select role, status
from workspace_memberships
where workspace_id = $1 and user_id = $2;

-- name: IsConversationParticipant :one
select exists(
  select 1
  from conversation_participants
  where conversation_id = $1 and user_id = $2
);

-- name: IsDirectMessage :one
select exists(
  select 1
  from conversation_pairs
  where conversation_id = $1
);

-- name: GetMessage :one
select id, conversation_id, author_user_id, body_text, body_rich, metadata, edited_at, deleted_at, created_at
from messages
where id = $1;

-- name: CountActiveUsersByIDs :one
select count(*)::int as count
from users
where id = any($1::uuid[]) and status = 'active';

-- name: CountActiveWorkspaceMembersByIDs :one
select count(*)::int as count
from workspace_memberships
where workspace_id = $1
  and user_id = any($2::uuid[])
  and status = 'active';
