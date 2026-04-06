-- name: GetSessionAuthByTokenHash :one
select s.id, s.user_id
from auth_sessions s
join users u on u.id = s.user_id
where s.token_hash = $1
  and s.revoked_at is null
  and s.expires_at > $2
  and u.status = 'active';

-- name: TouchAuthSessionLastSeen :exec
update auth_sessions
set last_seen_at = $2
where id = $1;

-- name: GetAPIKeyAuthBySecretHash :one
select k.id, k.user_id, k.scope_type, k.scope_workspace_id
from api_keys k
join users u on u.id = k.user_id
where k.secret_hash = $1
  and k.revoked_at is null
  and (k.expires_at is null or k.expires_at > $2)
  and u.status = 'active';

-- name: TouchAPIKeyLastUsed :exec
update api_keys
set last_used_at = $2
where id = $1;
