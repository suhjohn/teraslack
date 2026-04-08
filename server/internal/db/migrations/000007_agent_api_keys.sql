create table if not exists agent_api_keys (
  id uuid primary key,
  agent_user_id uuid not null references agents(user_id) on delete cascade,
  created_by_user_id uuid not null references users(id),
  token_hash text not null unique,
  encrypted_token text not null,
  scope_type text not null check (scope_type in ('user', 'workspace')),
  scope_workspace_id uuid references workspaces(id),
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null,
  check (
    (scope_type = 'user' and scope_workspace_id is null) or
    (scope_type = 'workspace' and scope_workspace_id is not null)
  )
);

create unique index if not exists agent_api_keys_one_active_per_agent
  on agent_api_keys (agent_user_id)
  where revoked_at is null;
