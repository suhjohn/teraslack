-- Upgrade older dev databases that still use bigint event IDs and last_event_id
-- so they match the current UUID + sequence_id schema without dropping data.

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'projector_checkpoints'
      and column_name = 'last_event_id'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'projector_checkpoints'
      and column_name = 'last_sequence_id'
  ) then
    alter table projector_checkpoints rename column last_event_id to last_sequence_id;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'internal_events'
      and column_name = 'id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'internal_events'
      and column_name = 'legacy_id'
  ) then
    alter table internal_events rename column id to legacy_id;
  end if;
end $$;

alter table internal_events add column if not exists id uuid;
update internal_events
set id = gen_random_uuid()
where id is null;
alter table internal_events alter column id set default gen_random_uuid();
alter table internal_events alter column id set not null;

alter table internal_events add column if not exists sequence_id bigint;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'internal_events'
      and column_name = 'legacy_id'
  ) then
    execute 'update internal_events set sequence_id = legacy_id where sequence_id is null';
  end if;
end $$;
create sequence if not exists internal_events_sequence_id_seq;
alter sequence internal_events_sequence_id_seq owned by internal_events.sequence_id;
select setval(
  'internal_events_sequence_id_seq',
  coalesce((select max(sequence_id) from internal_events) + 1, 1),
  false
);
alter table internal_events alter column sequence_id set default nextval('internal_events_sequence_id_seq');
alter table internal_events alter column sequence_id set not null;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'internal_events'
      and column_name = 'legacy_id'
  ) and not exists (
    select 1
    from pg_constraint
    where conname = 'internal_events_id_uuid_key'
      and conrelid = 'internal_events'::regclass
  ) then
    alter table internal_events add constraint internal_events_id_uuid_key unique (id);
  end if;

  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'internal_events'
      and column_name = 'legacy_id'
  ) and not exists (
    select 1
    from pg_constraint
    where conname = 'internal_events_sequence_id_key'
      and conrelid = 'internal_events'::regclass
  ) then
    alter table internal_events add constraint internal_events_sequence_id_key unique (sequence_id);
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'legacy_id'
  ) then
    alter table external_events rename column id to legacy_id;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'source_internal_event_id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'source_internal_event_legacy_id'
  ) then
    alter table external_events rename column source_internal_event_id to source_internal_event_legacy_id;
  end if;
end $$;

alter table external_events add column if not exists id uuid;
update external_events
set id = gen_random_uuid()
where id is null;
alter table external_events alter column id set default gen_random_uuid();
alter table external_events alter column id set not null;

alter table external_events add column if not exists sequence_id bigint;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'legacy_id'
  ) then
    execute 'update external_events set sequence_id = legacy_id where sequence_id is null';
  end if;
end $$;
create sequence if not exists external_events_sequence_id_seq;
alter sequence external_events_sequence_id_seq owned by external_events.sequence_id;
select setval(
  'external_events_sequence_id_seq',
  coalesce((select max(sequence_id) from external_events) + 1, 1),
  false
);
alter table external_events alter column sequence_id set default nextval('external_events_sequence_id_seq');
alter table external_events alter column sequence_id set not null;

alter table external_events add column if not exists source_internal_event_id uuid;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'source_internal_event_legacy_id'
  ) then
    execute $sql$
      update external_events ee
      set source_internal_event_id = ie.id
      from internal_events ie
      where ee.source_internal_event_legacy_id = ie.legacy_id
        and ee.source_internal_event_id is null
    $sql$;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'legacy_id'
  ) and not exists (
    select 1
    from pg_constraint
    where conname = 'external_events_id_uuid_key'
      and conrelid = 'external_events'::regclass
  ) then
    alter table external_events add constraint external_events_id_uuid_key unique (id);
  end if;

  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'legacy_id'
  ) and not exists (
    select 1
    from pg_constraint
    where conname = 'external_events_sequence_id_key'
      and conrelid = 'external_events'::regclass
  ) then
    alter table external_events add constraint external_events_sequence_id_key unique (sequence_id);
  end if;

  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_events'
      and column_name = 'source_internal_event_legacy_id'
  ) and not exists (
    select 1
    from pg_constraint
    where conname = 'external_events_source_internal_event_id_uuid_fkey'
      and conrelid = 'external_events'::regclass
  ) then
    alter table external_events
      add constraint external_events_source_internal_event_id_uuid_fkey
      foreign key (source_internal_event_id) references internal_events(id) on delete set null;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'workspace_event_feed'
      and column_name = 'external_event_id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'workspace_event_feed'
      and column_name = 'external_event_legacy_id'
  ) then
    alter table workspace_event_feed rename column external_event_id to external_event_legacy_id;
  end if;
end $$;

alter table workspace_event_feed add column if not exists external_event_id uuid;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'workspace_event_feed'
      and column_name = 'external_event_legacy_id'
  ) then
    execute $sql$
      update workspace_event_feed wef
      set external_event_id = ee.id
      from external_events ee
      where wef.external_event_legacy_id = ee.legacy_id
        and wef.external_event_id is null
    $sql$;
    alter table workspace_event_feed alter column external_event_legacy_id drop not null;
    alter table workspace_event_feed alter column external_event_id set not null;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'workspace_event_feed_workspace_id_external_event_uuid_key'
        and conrelid = 'workspace_event_feed'::regclass
    ) then
      alter table workspace_event_feed
        add constraint workspace_event_feed_workspace_id_external_event_uuid_key
        unique (workspace_id, external_event_id);
    end if;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'workspace_event_feed_external_event_id_uuid_fkey'
        and conrelid = 'workspace_event_feed'::regclass
    ) then
      alter table workspace_event_feed
        add constraint workspace_event_feed_external_event_id_uuid_fkey
        foreign key (external_event_id) references external_events(id) on delete cascade;
    end if;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'conversation_event_feed'
      and column_name = 'external_event_id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'conversation_event_feed'
      and column_name = 'external_event_legacy_id'
  ) then
    alter table conversation_event_feed rename column external_event_id to external_event_legacy_id;
  end if;
end $$;

alter table conversation_event_feed add column if not exists external_event_id uuid;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'conversation_event_feed'
      and column_name = 'external_event_legacy_id'
  ) then
    execute $sql$
      update conversation_event_feed cef
      set external_event_id = ee.id
      from external_events ee
      where cef.external_event_legacy_id = ee.legacy_id
        and cef.external_event_id is null
    $sql$;
    alter table conversation_event_feed alter column external_event_legacy_id drop not null;
    alter table conversation_event_feed alter column external_event_id set not null;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'conversation_event_feed_conversation_id_external_event_uuid_key'
        and conrelid = 'conversation_event_feed'::regclass
    ) then
      alter table conversation_event_feed
        add constraint conversation_event_feed_conversation_id_external_event_uuid_key
        unique (conversation_id, external_event_id);
    end if;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'conversation_event_feed_external_event_id_uuid_fkey'
        and conrelid = 'conversation_event_feed'::regclass
    ) then
      alter table conversation_event_feed
        add constraint conversation_event_feed_external_event_id_uuid_fkey
        foreign key (external_event_id) references external_events(id) on delete cascade;
    end if;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'user_event_feed'
      and column_name = 'external_event_id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'user_event_feed'
      and column_name = 'external_event_legacy_id'
  ) then
    alter table user_event_feed rename column external_event_id to external_event_legacy_id;
  end if;
end $$;

alter table user_event_feed add column if not exists external_event_id uuid;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'user_event_feed'
      and column_name = 'external_event_legacy_id'
  ) then
    execute $sql$
      update user_event_feed uef
      set external_event_id = ee.id
      from external_events ee
      where uef.external_event_legacy_id = ee.legacy_id
        and uef.external_event_id is null
    $sql$;
    alter table user_event_feed alter column external_event_legacy_id drop not null;
    alter table user_event_feed alter column external_event_id set not null;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'user_event_feed_user_id_external_event_uuid_key'
        and conrelid = 'user_event_feed'::regclass
    ) then
      alter table user_event_feed
        add constraint user_event_feed_user_id_external_event_uuid_key
        unique (user_id, external_event_id);
    end if;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'user_event_feed_external_event_id_uuid_fkey'
        and conrelid = 'user_event_feed'::regclass
    ) then
      alter table user_event_feed
        add constraint user_event_feed_external_event_id_uuid_fkey
        foreign key (external_event_id) references external_events(id) on delete cascade;
    end if;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_event_projection_failures'
      and column_name = 'id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_event_projection_failures'
      and column_name = 'legacy_id'
  ) then
    alter table external_event_projection_failures rename column id to legacy_id;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_event_projection_failures'
      and column_name = 'internal_event_id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_event_projection_failures'
      and column_name = 'internal_event_legacy_id'
  ) then
    alter table external_event_projection_failures rename column internal_event_id to internal_event_legacy_id;
  end if;
end $$;

alter table external_event_projection_failures add column if not exists id uuid;
update external_event_projection_failures
set id = gen_random_uuid()
where id is null;
alter table external_event_projection_failures alter column id set default gen_random_uuid();
alter table external_event_projection_failures alter column id set not null;

alter table external_event_projection_failures add column if not exists internal_event_id uuid;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'external_event_projection_failures'
      and column_name = 'internal_event_legacy_id'
  ) then
    execute $sql$
      update external_event_projection_failures eepf
      set internal_event_id = ie.id
      from internal_events ie
      where eepf.internal_event_legacy_id = ie.legacy_id
        and eepf.internal_event_id is null
    $sql$;
    alter table external_event_projection_failures alter column internal_event_legacy_id drop not null;
    alter table external_event_projection_failures alter column internal_event_id set not null;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'external_event_projection_failures_id_uuid_key'
        and conrelid = 'external_event_projection_failures'::regclass
    ) then
      alter table external_event_projection_failures
        add constraint external_event_projection_failures_id_uuid_key
        unique (id);
    end if;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'external_event_projection_failures_internal_event_id_uuid_fkey'
        and conrelid = 'external_event_projection_failures'::regclass
    ) then
      alter table external_event_projection_failures
        add constraint external_event_projection_failures_internal_event_id_uuid_fkey
        foreign key (internal_event_id) references internal_events(id) on delete cascade;
    end if;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'webhook_deliveries'
      and column_name = 'id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'webhook_deliveries'
      and column_name = 'legacy_id'
  ) then
    alter table webhook_deliveries rename column id to legacy_id;
  end if;
end $$;

do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'webhook_deliveries'
      and column_name = 'external_event_id'
      and data_type = 'bigint'
  ) and not exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'webhook_deliveries'
      and column_name = 'external_event_legacy_id'
  ) then
    alter table webhook_deliveries rename column external_event_id to external_event_legacy_id;
  end if;
end $$;

alter table webhook_deliveries add column if not exists id uuid;
update webhook_deliveries
set id = gen_random_uuid()
where id is null;
alter table webhook_deliveries alter column id set default gen_random_uuid();
alter table webhook_deliveries alter column id set not null;

alter table webhook_deliveries add column if not exists external_event_id uuid;
do $$
begin
  if exists (
    select 1
    from information_schema.columns
    where table_schema = 'public'
      and table_name = 'webhook_deliveries'
      and column_name = 'external_event_legacy_id'
  ) then
    execute $sql$
      update webhook_deliveries wd
      set external_event_id = ee.id
      from external_events ee
      where wd.external_event_legacy_id = ee.legacy_id
        and wd.external_event_id is null
    $sql$;
    alter table webhook_deliveries alter column external_event_legacy_id drop not null;
    alter table webhook_deliveries alter column external_event_id set not null;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'webhook_deliveries_id_uuid_key'
        and conrelid = 'webhook_deliveries'::regclass
    ) then
      alter table webhook_deliveries
        add constraint webhook_deliveries_id_uuid_key
        unique (id);
    end if;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'webhook_deliveries_subscription_id_external_event_uuid_key'
        and conrelid = 'webhook_deliveries'::regclass
    ) then
      alter table webhook_deliveries
        add constraint webhook_deliveries_subscription_id_external_event_uuid_key
        unique (subscription_id, external_event_id);
    end if;
    if not exists (
      select 1
      from pg_constraint
      where conname = 'webhook_deliveries_external_event_id_uuid_fkey'
        and conrelid = 'webhook_deliveries'::regclass
    ) then
      alter table webhook_deliveries
        add constraint webhook_deliveries_external_event_id_uuid_fkey
        foreign key (external_event_id) references external_events(id) on delete cascade;
    end if;
  end if;
end $$;

create index if not exists idx_internal_events_shard_sequence_id on internal_events(shard_id, sequence_id);
create index if not exists idx_external_events_sequence_id on external_events(sequence_id);
