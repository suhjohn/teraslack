-- name: CreateEmailLoginChallenge :exec
insert into email_login_challenges (id, email, code_hash, expires_at, created_at)
values (
  sqlc.arg(id),
  sqlc.arg(email),
  sqlc.arg(code_hash),
  sqlc.arg(expires_at),
  sqlc.arg(created_at)
);

-- name: GetEmailLoginChallengeForVerification :one
select id, expires_at
from email_login_challenges
where email = sqlc.arg(email)
  and code_hash = sqlc.arg(code_hash)
  and consumed_at is null
order by created_at desc
limit 1
for update;

-- name: ConsumeEmailLoginChallenge :exec
update email_login_challenges
set consumed_at = sqlc.arg(consumed_at)
where id = sqlc.arg(id);

-- name: RevokeAuthSession :execrows
update auth_sessions
set revoked_at = sqlc.arg(revoked_at)
where id = sqlc.arg(id)
  and revoked_at is null;

-- name: ListWorkspaceMembershipSummariesByUser :many
select wm.workspace_id, wm.role, wm.status, w.name
from workspace_memberships wm
join workspaces w on w.id = wm.workspace_id
where wm.user_id = sqlc.arg(user_id)
  and wm.status = 'active'
order by w.name asc;

-- name: UpdateUserProfile :exec
update user_profiles
set handle = coalesce(sqlc.narg(handle), handle),
    display_name = coalesce(sqlc.narg(display_name), display_name),
    avatar_url = sqlc.narg(avatar_url),
    bio = sqlc.narg(bio),
    updated_at = sqlc.arg(updated_at)
where user_id = sqlc.arg(user_id);

-- name: ListAPIKeysByUser :many
select id, user_id, label, scope_type, scope_workspace_id, expires_at, last_used_at, revoked_at, created_at
from api_keys
where user_id = sqlc.arg(user_id)
order by created_at desc;

-- name: GetAPIKeyByID :one
select id, user_id, label, scope_type, scope_workspace_id, expires_at, last_used_at, revoked_at, created_at
from api_keys
where id = sqlc.arg(id);

-- name: CreateAPIKey :exec
insert into api_keys (
  id,
  user_id,
  label,
  secret_hash,
  scope_type,
  scope_workspace_id,
  expires_at,
  created_at
) values (
  sqlc.arg(id),
  sqlc.arg(user_id),
  sqlc.arg(label),
  sqlc.arg(secret_hash),
  sqlc.arg(scope_type),
  sqlc.narg(scope_workspace_id),
  sqlc.narg(expires_at),
  sqlc.arg(created_at)
);

-- name: CreateAgentAPIKey :exec
insert into agent_api_keys (
  id,
  agent_user_id,
  created_by_user_id,
  token_hash,
  encrypted_token,
  scope_type,
  scope_workspace_id,
  created_at
) values (
  sqlc.arg(id),
  sqlc.arg(agent_user_id),
  sqlc.arg(created_by_user_id),
  sqlc.arg(token_hash),
  sqlc.arg(encrypted_token),
  sqlc.arg(scope_type),
  sqlc.narg(scope_workspace_id),
  sqlc.arg(created_at)
);

-- name: GetActiveAgentAPIKeyForUpdate :one
select id, agent_user_id, encrypted_token, scope_type, scope_workspace_id, created_at
from agent_api_keys
where agent_user_id = sqlc.arg(agent_user_id)
  and revoked_at is null
for update;

-- name: RevokeActiveAgentAPIKey :execrows
update agent_api_keys
set revoked_at = sqlc.arg(revoked_at)
where agent_user_id = sqlc.arg(agent_user_id)
  and revoked_at is null;

-- name: RevokeAPIKeyByOwner :execrows
update api_keys
set revoked_at = sqlc.arg(revoked_at)
where id = sqlc.arg(id)
  and user_id = sqlc.arg(user_id)
  and revoked_at is null;

-- name: ListActiveWorkspacesByUser :many
select w.id, w.slug, w.name, w.created_by_user_id, w.created_at, w.updated_at
from workspaces w
join workspace_memberships wm on wm.workspace_id = w.id
where wm.user_id = sqlc.arg(user_id)
  and wm.status = 'active'
order by w.name asc;

-- name: CreateWorkspace :exec
insert into workspaces (id, slug, name, created_by_user_id, created_at, updated_at)
values (
  sqlc.arg(id),
  sqlc.arg(slug),
  sqlc.arg(name),
  sqlc.arg(created_by_user_id),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: CreateWorkspaceMembership :exec
insert into workspace_memberships (
  id,
  workspace_id,
  user_id,
  role,
  status,
  invited_by_user_id,
  joined_at,
  created_at,
  updated_at
) values (
  sqlc.arg(id),
  sqlc.arg(workspace_id),
  sqlc.arg(user_id),
  sqlc.arg(role),
  sqlc.arg(status),
  sqlc.narg(invited_by_user_id),
  sqlc.narg(joined_at),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: UpdateWorkspace :exec
update workspaces
set name = coalesce(sqlc.narg(name), name),
    slug = coalesce(sqlc.narg(slug), slug),
    updated_at = sqlc.arg(updated_at)
where id = sqlc.arg(id);

-- name: ListWorkspaceMembers :many
select wm.workspace_id, wm.user_id, wm.role, wm.status,
       u.id, u.principal_type, u.status, u.email, p.handle, p.display_name, p.avatar_url, p.bio
from workspace_memberships wm
join users u on u.id = wm.user_id
join user_profiles p on p.user_id = u.id
where wm.workspace_id = sqlc.arg(workspace_id)
order by p.display_name asc;

-- name: CreateWorkspaceInvite :exec
insert into workspace_invites (
  id,
  workspace_id,
  email,
  invited_user_id,
  invited_by_user_id,
  token_hash,
  expires_at,
  created_at
) values (
  sqlc.arg(id),
  sqlc.arg(workspace_id),
  sqlc.narg(email),
  sqlc.narg(invited_user_id),
  sqlc.arg(invited_by_user_id),
  sqlc.arg(token_hash),
  sqlc.arg(expires_at),
  sqlc.arg(created_at)
);

-- name: GetWorkspaceInviteByTokenHashForUpdate :one
select id, workspace_id, email, invited_user_id, accepted_at
from workspace_invites
where token_hash = sqlc.arg(token_hash)
  and expires_at > sqlc.arg(now_at)
for update;

-- name: GetWorkspaceMembershipForUpdate :one
select id, role, status
from workspace_memberships
where workspace_id = sqlc.arg(workspace_id)
  and user_id = sqlc.arg(user_id)
for update;

-- name: ActivateWorkspaceMembership :exec
update workspace_memberships
set status = 'active',
    joined_at = sqlc.arg(updated_at),
    updated_at = sqlc.arg(updated_at)
where id = sqlc.arg(id)
  and user_id = sqlc.arg(user_id);

-- name: AcceptWorkspaceInvite :exec
update workspace_invites
set accepted_at = sqlc.arg(accepted_at),
    accepted_by_user_id = sqlc.arg(accepted_by_user_id)
where id = sqlc.arg(id);

-- name: CountActiveWorkspaceOwners :one
select count(*)::int as count
from workspace_memberships
where workspace_id = sqlc.arg(workspace_id)
  and role = 'owner'
  and status = 'active';

-- name: UpdateWorkspaceMembership :exec
update workspace_memberships
set role = sqlc.arg(role),
    status = sqlc.arg(status),
    updated_at = sqlc.arg(updated_at),
    joined_at = case
      when sqlc.arg(status) = 'active' and joined_at is null then sqlc.arg(updated_at)
      else joined_at
    end
where id = sqlc.arg(id)
  and user_id = sqlc.arg(user_id);

-- name: CreateAuthSession :exec
insert into auth_sessions (id, user_id, token_hash, expires_at, last_seen_at, created_at)
values (
  sqlc.arg(id),
  sqlc.arg(user_id),
  sqlc.arg(token_hash),
  sqlc.arg(expires_at),
  sqlc.arg(last_seen_at),
  sqlc.arg(created_at)
);

-- name: GetUserByEmail :one
select u.id, u.principal_type, u.status, u.email, p.handle, p.display_name, p.avatar_url, p.bio
from users u
join user_profiles p on p.user_id = u.id
where u.email = sqlc.arg(email);

-- name: CreateUser :exec
insert into users (id, principal_type, email, status, created_at, updated_at)
values (
  sqlc.arg(id),
  sqlc.arg(principal_type),
  sqlc.narg(email),
  sqlc.arg(status),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: CreateUserProfile :exec
insert into user_profiles (user_id, handle, display_name, created_at, updated_at)
values (
  sqlc.arg(user_id),
  sqlc.arg(handle),
  sqlc.arg(display_name),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: CreateAgent :exec
insert into agents (
  user_id,
  owner_user_id,
  owner_workspace_id,
  mode,
  created_by_user_id,
  created_at,
  updated_at
) values (
  sqlc.arg(user_id),
  sqlc.narg(owner_user_id),
  sqlc.narg(owner_workspace_id),
  sqlc.arg(mode),
  sqlc.arg(created_by_user_id),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
);

-- name: UpdateAgent :exec
update agents
set mode = coalesce(sqlc.narg(mode), mode),
    updated_at = sqlc.arg(updated_at)
where user_id = sqlc.arg(user_id);

-- name: UpdateUserStatus :exec
update users
set status = sqlc.arg(status),
    updated_at = sqlc.arg(updated_at)
where id = sqlc.arg(id);

-- name: ListWorkspacePrivateConversationParticipantCountsForUser :many
select c.id, count(cp_all.user_id)::int as participant_count
from conversations c
join conversation_participants cp on cp.conversation_id = c.id and cp.user_id = sqlc.arg(user_id)
join conversation_participants cp_all on cp_all.conversation_id = c.id
where c.workspace_id = sqlc.arg(workspace_id)
  and c.access_policy = 'members'
group by c.id;
