-- name: CreateOAuthState :exec
insert into oauth_states (id, provider, state_hash, redirect_uri, expires_at, created_at)
values (
  sqlc.arg(id),
  sqlc.arg(provider),
  sqlc.arg(state_hash),
  sqlc.arg(redirect_uri),
  sqlc.arg(expires_at),
  sqlc.arg(created_at)
);

-- name: GetOAuthStateByHash :one
select id, redirect_uri
from oauth_states
where provider = sqlc.arg(provider)
  and state_hash = sqlc.arg(state_hash)
  and expires_at > sqlc.arg(now_at);

-- name: DeleteOAuthState :exec
delete from oauth_states
where id = sqlc.arg(id);

-- name: GetUserByOAuthAccount :one
select u.id, u.principal_type, u.status, u.email, p.handle, p.display_name, p.avatar_url, p.bio
from oauth_accounts oa
join users u on u.id = oa.user_id
join user_profiles p on p.user_id = u.id
where oa.provider = sqlc.arg(provider)
  and oa.provider_user_id = sqlc.arg(provider_user_id);

-- name: UpsertOAuthAccount :exec
insert into oauth_accounts (
  id,
  provider,
  provider_user_id,
  user_id,
  email,
  created_at,
  updated_at
) values (
  sqlc.arg(id),
  sqlc.arg(provider),
  sqlc.arg(provider_user_id),
  sqlc.arg(user_id),
  sqlc.narg(email),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
)
on conflict (provider, provider_user_id)
do update set
  user_id = excluded.user_id,
  email = excluded.email,
  updated_at = excluded.updated_at;
