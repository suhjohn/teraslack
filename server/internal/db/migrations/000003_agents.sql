create table if not exists agents (
  user_id uuid primary key references users(id) on delete cascade,
  owner_user_id uuid references users(id),
  owner_workspace_id uuid references workspaces(id),
  mode text not null check (mode in ('read_only', 'safe_write')),
  created_by_user_id uuid not null references users(id),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  check (
    (owner_user_id is not null and owner_workspace_id is null) or
    (owner_user_id is null and owner_workspace_id is not null)
  )
);

create index if not exists idx_agents_owner_user on agents(owner_user_id);
create index if not exists idx_agents_owner_workspace on agents(owner_workspace_id);

alter table workspace_invites
add column if not exists invited_user_id uuid references users(id);

create index if not exists idx_workspace_invites_invited_user on workspace_invites(invited_user_id);
