create table if not exists api_request_logs (
  id bigserial primary key,
  auth_kind text not null check (auth_kind in ('session', 'api_key')),
  user_id uuid not null references users(id) on delete cascade,
  api_key_id uuid references api_keys(id) on delete set null,
  scope_workspace_id uuid references workspaces(id) on delete set null,
  method text not null,
  path_template text not null,
  status_code integer not null,
  duration_ms integer not null,
  request_id text,
  created_at timestamptz not null
);

create index if not exists idx_api_request_logs_user_created on api_request_logs(user_id, created_at desc);
create index if not exists idx_api_request_logs_key_created on api_request_logs(api_key_id, created_at desc);
create index if not exists idx_api_request_logs_workspace_created on api_request_logs(scope_workspace_id, created_at desc);
create index if not exists idx_api_request_logs_path_created on api_request_logs(path_template, created_at desc);
