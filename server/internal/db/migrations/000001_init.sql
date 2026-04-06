create extension if not exists pgcrypto;

create table if not exists users (
  id uuid primary key,
  principal_type text not null check (principal_type in ('human', 'agent')),
  email text unique,
  status text not null check (status in ('active', 'suspended', 'deleted')),
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table if not exists user_profiles (
  user_id uuid primary key references users(id) on delete cascade,
  handle text not null unique,
  display_name text not null,
  avatar_url text,
  bio text,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table if not exists auth_sessions (
  id uuid primary key,
  user_id uuid not null references users(id) on delete cascade,
  token_hash text not null unique,
  expires_at timestamptz not null,
  last_seen_at timestamptz not null,
  revoked_at timestamptz,
  created_at timestamptz not null
);

create table if not exists email_login_challenges (
  id uuid primary key,
  email text not null,
  code_hash text not null,
  expires_at timestamptz not null,
  consumed_at timestamptz,
  created_at timestamptz not null
);

create table if not exists oauth_accounts (
  id uuid primary key,
  provider text not null check (provider in ('google', 'github')),
  provider_user_id text not null,
  user_id uuid not null references users(id) on delete cascade,
  email text,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (provider, provider_user_id)
);

create table if not exists oauth_states (
  id uuid primary key,
  provider text not null check (provider in ('google', 'github')),
  state_hash text not null unique,
  redirect_uri text,
  expires_at timestamptz not null,
  created_at timestamptz not null
);

create table if not exists workspaces (
  id uuid primary key,
  slug text not null unique,
  name text not null,
  created_by_user_id uuid not null references users(id),
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table if not exists api_keys (
  id uuid primary key,
  user_id uuid not null references users(id) on delete cascade,
  label text not null,
  secret_hash text not null unique,
  scope_type text not null check (scope_type in ('user', 'workspace')),
  scope_workspace_id uuid references workspaces(id),
  expires_at timestamptz,
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null,
  check (
    (scope_type = 'user' and scope_workspace_id is null) or
    (scope_type = 'workspace' and scope_workspace_id is not null)
  )
);

create table if not exists workspace_memberships (
  id uuid primary key,
  workspace_id uuid not null references workspaces(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  role text not null check (role in ('owner', 'admin', 'member')),
  status text not null check (status in ('invited', 'active', 'suspended', 'removed')),
  invited_by_user_id uuid references users(id),
  joined_at timestamptz,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (workspace_id, user_id)
);

create table if not exists workspace_invites (
  id uuid primary key,
  workspace_id uuid not null references workspaces(id) on delete cascade,
  email text,
  invited_by_user_id uuid not null references users(id),
  token_hash text not null unique,
  expires_at timestamptz not null,
  accepted_at timestamptz,
  accepted_by_user_id uuid references users(id),
  created_at timestamptz not null
);

create table if not exists conversations (
  id uuid primary key,
  workspace_id uuid references workspaces(id) on delete cascade,
  access_policy text not null check (access_policy in ('members', 'workspace', 'authenticated')),
  title text,
  description text,
  created_by_user_id uuid not null references users(id),
  archived_at timestamptz,
  last_message_at timestamptz,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  check (
    (workspace_id is null and access_policy in ('members', 'authenticated')) or
    (workspace_id is not null and access_policy in ('members', 'workspace'))
  ),
  check (
    access_policy = 'members' or title is not null
  )
);

create table if not exists conversation_pairs (
  conversation_id uuid primary key references conversations(id) on delete cascade,
  first_user_id uuid not null references users(id),
  second_user_id uuid not null references users(id),
  unique (first_user_id, second_user_id),
  check (first_user_id <> second_user_id),
  check (first_user_id < second_user_id)
);

create table if not exists conversation_participants (
  conversation_id uuid not null references conversations(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  added_by_user_id uuid references users(id),
  joined_at timestamptz not null,
  primary key (conversation_id, user_id)
);

create table if not exists conversation_invites (
  id uuid primary key,
  conversation_id uuid not null references conversations(id) on delete cascade,
  created_by_user_id uuid not null references users(id),
  token_hash text not null unique,
  expires_at timestamptz,
  mode text not null check (mode in ('link', 'restricted')),
  allowed_user_ids jsonb,
  allowed_emails jsonb,
  revoked_at timestamptz,
  created_at timestamptz not null
);

create table if not exists messages (
  id uuid primary key,
  conversation_id uuid not null references conversations(id) on delete cascade,
  author_user_id uuid not null references users(id),
  body_text text not null,
  body_rich jsonb,
  metadata jsonb,
  edited_at timestamptz,
  deleted_at timestamptz,
  created_at timestamptz not null,
  check (body_text <> '' or body_rich is not null)
);

create table if not exists conversation_reads (
  conversation_id uuid not null references conversations(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  last_read_message_id uuid references messages(id),
  last_read_at timestamptz,
  updated_at timestamptz not null,
  primary key (conversation_id, user_id)
);

create table if not exists internal_events (
  id bigserial primary key,
  event_type text not null,
  aggregate_type text not null,
  aggregate_id uuid not null,
  workspace_id uuid references workspaces(id),
  actor_user_id uuid references users(id),
  shard_id integer not null default 0,
  payload jsonb not null,
  created_at timestamptz not null
);

create table if not exists projector_checkpoints (
  name text primary key,
  last_event_id bigint not null,
  updated_at timestamptz not null
);

create table if not exists projector_leases (
  name text not null,
  shard_id integer not null,
  owner text not null,
  lease_until timestamptz not null,
  updated_at timestamptz not null,
  primary key (name, shard_id)
);

create table if not exists external_events (
  id bigserial primary key,
  workspace_id uuid references workspaces(id),
  type text not null,
  resource_type text not null,
  resource_id uuid not null,
  occurred_at timestamptz not null,
  payload jsonb not null,
  source_internal_event_id bigint references internal_events(id) on delete set null,
  dedupe_key text not null unique,
  created_at timestamptz not null
);

create table if not exists workspace_event_feed (
  workspace_id uuid not null references workspaces(id) on delete cascade,
  external_event_id bigint not null references external_events(id) on delete cascade,
  unique (workspace_id, external_event_id)
);

create table if not exists conversation_event_feed (
  conversation_id uuid not null references conversations(id) on delete cascade,
  external_event_id bigint not null references external_events(id) on delete cascade,
  unique (conversation_id, external_event_id)
);

create table if not exists user_event_feed (
  user_id uuid not null references users(id) on delete cascade,
  external_event_id bigint not null references external_events(id) on delete cascade,
  unique (user_id, external_event_id)
);

create table if not exists event_subscriptions (
  id uuid primary key,
  owner_user_id uuid not null references users(id) on delete cascade,
  workspace_id uuid references workspaces(id) on delete cascade,
  url text not null,
  enabled boolean not null default true,
  encrypted_secret text not null,
  event_type text,
  resource_type text,
  resource_id uuid,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  check (resource_id is null or resource_type is not null)
);

create table if not exists external_event_projection_failures (
  id bigserial primary key,
  internal_event_id bigint not null references internal_events(id) on delete cascade,
  error text not null,
  created_at timestamptz not null
);

create table if not exists webhook_deliveries (
  id bigserial primary key,
  subscription_id uuid not null references event_subscriptions(id) on delete cascade,
  external_event_id bigint not null references external_events(id) on delete cascade,
  status text not null check (status in ('pending', 'processing', 'delivered', 'failed')),
  attempt_count integer not null default 0,
  next_attempt_at timestamptz not null,
  last_error text,
  delivered_at timestamptz,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (subscription_id, external_event_id)
);

create table if not exists search_documents (
  entity_type text not null check (entity_type in ('user', 'workspace', 'conversation')),
  entity_id uuid not null,
  workspace_id uuid references workspaces(id) on delete cascade,
  title text not null,
  subtitle text,
  content text not null,
  updated_at timestamptz not null,
  primary key (entity_type, entity_id)
);

create index if not exists idx_workspace_memberships_workspace_user on workspace_memberships(workspace_id, user_id);
create index if not exists idx_workspace_memberships_user_status on workspace_memberships(user_id, status);
create index if not exists idx_conversations_workspace_access_updated on conversations(workspace_id, access_policy, updated_at desc);
create index if not exists idx_conversations_access_updated on conversations(access_policy, updated_at desc);
create index if not exists idx_conversation_participants_user_conversation on conversation_participants(user_id, conversation_id);
create index if not exists idx_conversation_invites_token_hash on conversation_invites(token_hash);
create index if not exists idx_workspace_invites_token_hash on workspace_invites(token_hash);
create index if not exists idx_messages_conversation_created_at on messages(conversation_id, created_at desc);
create index if not exists idx_internal_events_id on internal_events(id);
create index if not exists idx_internal_events_aggregate on internal_events(aggregate_type, aggregate_id, id);
create index if not exists idx_external_events_id on external_events(id);
create index if not exists idx_workspace_event_feed_workspace_event on workspace_event_feed(workspace_id, external_event_id);
create index if not exists idx_conversation_event_feed_conversation_event on conversation_event_feed(conversation_id, external_event_id);
create index if not exists idx_user_event_feed_user_event on user_event_feed(user_id, external_event_id);
create index if not exists idx_event_subscriptions_owner on event_subscriptions(owner_user_id);
create index if not exists idx_projector_leases_name_owner on projector_leases(name, owner, lease_until);
create index if not exists idx_search_documents_workspace on search_documents(workspace_id, entity_type, updated_at desc);
create index if not exists idx_search_documents_title on search_documents(entity_type, title);

create or replace function teraslack_check_active_workspace_owner() returns trigger as $$
declare
  affected_workspace_id uuid;
begin
  if tg_op = 'DELETE' then
    affected_workspace_id := old.workspace_id;
  else
    affected_workspace_id := new.workspace_id;
  end if;
  if affected_workspace_id is null then
    return null;
  end if;
  if exists (
    select 1
    from workspaces w
    where w.id = affected_workspace_id
  ) and not exists (
    select 1
    from workspace_memberships wm
    where wm.workspace_id = affected_workspace_id
      and wm.role = 'owner'
      and wm.status = 'active'
  ) then
    raise exception 'workspace must retain at least one active owner';
  end if;
  return null;
end;
$$ language plpgsql;

drop trigger if exists teraslack_check_active_workspace_owner on workspace_memberships;
create constraint trigger teraslack_check_active_workspace_owner
after insert or update or delete on workspace_memberships
deferrable initially deferred
for each row execute function teraslack_check_active_workspace_owner();

create or replace function teraslack_check_members_conversation_has_participants() returns trigger as $$
declare
  affected_conversation_id uuid;
begin
  if tg_table_name = 'conversations' then
    if tg_op = 'DELETE' then
      affected_conversation_id := old.id;
    else
      affected_conversation_id := new.id;
    end if;
  else
    if tg_op = 'DELETE' then
      affected_conversation_id := old.conversation_id;
    else
      affected_conversation_id := new.conversation_id;
    end if;
  end if;
  if affected_conversation_id is null then
    return null;
  end if;
  if exists (
    select 1
    from conversations c
    where c.id = affected_conversation_id
      and c.access_policy = 'members'
  ) and not exists (
    select 1
    from conversation_participants cp
    where cp.conversation_id = affected_conversation_id
  ) then
    raise exception 'member-only conversations must retain at least one participant';
  end if;
  return null;
end;
$$ language plpgsql;

drop trigger if exists teraslack_check_members_conversation_has_participants_on_conversations on conversations;
create constraint trigger teraslack_check_members_conversation_has_participants_on_conversations
after insert or update on conversations
deferrable initially deferred
for each row execute function teraslack_check_members_conversation_has_participants();

drop trigger if exists teraslack_check_members_conversation_has_participants_on_participants on conversation_participants;
create constraint trigger teraslack_check_members_conversation_has_participants_on_participants
after insert or update or delete on conversation_participants
deferrable initially deferred
for each row execute function teraslack_check_members_conversation_has_participants();

create or replace function teraslack_check_workspace_private_participants() returns trigger as $$
declare
  affected_conversation_id uuid;
  affected_workspace_id uuid;
  affected_user_id uuid;
begin
  if tg_table_name = 'conversation_participants' then
    if tg_op = 'DELETE' then
      affected_conversation_id := old.conversation_id;
      affected_user_id := old.user_id;
    else
      affected_conversation_id := new.conversation_id;
      affected_user_id := new.user_id;
    end if;
    if affected_conversation_id is null or affected_user_id is null then
      return null;
    end if;
    if exists (
      select 1
      from conversations c
      left join workspace_memberships wm
        on wm.workspace_id = c.workspace_id
       and wm.user_id = affected_user_id
       and wm.status = 'active'
      where c.id = affected_conversation_id
        and c.workspace_id is not null
        and c.access_policy = 'members'
        and wm.id is null
    ) then
      raise exception 'workspace-private conversation participants must be active workspace members';
    end if;
    return null;
  end if;

  if tg_op = 'DELETE' then
    affected_workspace_id := old.workspace_id;
    affected_user_id := old.user_id;
  else
    affected_workspace_id := new.workspace_id;
    affected_user_id := new.user_id;
  end if;
  if affected_workspace_id is null or affected_user_id is null then
    return null;
  end if;
  if exists (
    select 1
    from conversation_participants cp
    join conversations c on c.id = cp.conversation_id
    left join workspace_memberships wm
      on wm.workspace_id = c.workspace_id
     and wm.user_id = cp.user_id
     and wm.status = 'active'
    where c.workspace_id = affected_workspace_id
      and c.access_policy = 'members'
      and cp.user_id = affected_user_id
      and wm.id is null
  ) then
    raise exception 'workspace-private conversation participants must be active workspace members';
  end if;
  return null;
end;
$$ language plpgsql;

drop trigger if exists teraslack_check_workspace_private_participants_on_participants on conversation_participants;
create constraint trigger teraslack_check_workspace_private_participants_on_participants
after insert or update on conversation_participants
deferrable initially deferred
for each row execute function teraslack_check_workspace_private_participants();

drop trigger if exists teraslack_check_workspace_private_participants_on_memberships on workspace_memberships;
create constraint trigger teraslack_check_workspace_private_participants_on_memberships
after insert or update or delete on workspace_memberships
deferrable initially deferred
for each row execute function teraslack_check_workspace_private_participants();

create or replace function teraslack_validate_conversation_invite() returns trigger as $$
begin
  if not exists (
    select 1
    from conversations c
    where c.id = new.conversation_id
      and c.access_policy = 'members'
  ) then
    raise exception 'conversation invites require a member-only conversation';
  end if;
  if exists (
    select 1
    from conversation_pairs cp
    where cp.conversation_id = new.conversation_id
  ) then
    raise exception 'conversation invites are invalid for canonical direct messages';
  end if;
  return new;
end;
$$ language plpgsql;

drop trigger if exists teraslack_validate_conversation_invite on conversation_invites;
create trigger teraslack_validate_conversation_invite
before insert or update on conversation_invites
for each row execute function teraslack_validate_conversation_invite();

create or replace function teraslack_check_conversation_pair_integrity() returns trigger as $$
declare
  affected_conversation_id uuid;
begin
  if tg_table_name = 'conversations' then
    if tg_op = 'DELETE' then
      affected_conversation_id := old.id;
    else
      affected_conversation_id := new.id;
    end if;
  else
    if tg_op = 'DELETE' then
      affected_conversation_id := old.conversation_id;
    else
      affected_conversation_id := new.conversation_id;
    end if;
  end if;
  if affected_conversation_id is null then
    return null;
  end if;
  if exists (
    select 1
    from conversation_pairs cp
    join conversations c on c.id = cp.conversation_id
    where cp.conversation_id = affected_conversation_id
      and (
        c.workspace_id is not null
        or c.access_policy <> 'members'
        or (select count(*) from conversation_participants p where p.conversation_id = cp.conversation_id) <> 2
      )
  ) then
    raise exception 'conversation pair rows require exactly two participants in a global member-only conversation';
  end if;
  return null;
end;
$$ language plpgsql;

drop trigger if exists teraslack_check_conversation_pair_integrity_on_pairs on conversation_pairs;
create constraint trigger teraslack_check_conversation_pair_integrity_on_pairs
after insert or update on conversation_pairs
deferrable initially deferred
for each row execute function teraslack_check_conversation_pair_integrity();

drop trigger if exists teraslack_check_conversation_pair_integrity_on_participants on conversation_participants;
create constraint trigger teraslack_check_conversation_pair_integrity_on_participants
after insert or update or delete on conversation_participants
deferrable initially deferred
for each row execute function teraslack_check_conversation_pair_integrity();

drop trigger if exists teraslack_check_conversation_pair_integrity_on_conversations on conversations;
create constraint trigger teraslack_check_conversation_pair_integrity_on_conversations
after update on conversations
deferrable initially deferred
for each row execute function teraslack_check_conversation_pair_integrity();
