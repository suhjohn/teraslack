do $$
declare
  constraint_name text;
begin
  execute 'alter table conversations drop constraint if exists conversations_scope_access_policy_check';

  for constraint_name in
    select conname
    from pg_constraint
    where conrelid = 'conversations'::regclass
      and pg_get_constraintdef(oid) like '%workspace_id is null and access_policy in (''members'', ''authenticated'')%'
  loop
    execute format('alter table conversations drop constraint %I', constraint_name);
  end loop;
end $$;

insert into conversation_participants (conversation_id, user_id, added_by_user_id, joined_at)
select c.id, c.created_by_user_id, c.created_by_user_id, c.created_at
from conversations c
where c.workspace_id is null
  and c.access_policy = 'authenticated'
on conflict do nothing;

update conversations
set access_policy = 'members',
    updated_at = now()
where workspace_id is null
  and access_policy = 'authenticated';

alter table conversations
  alter column access_policy set default 'members';

alter table conversations
  add constraint conversations_scope_access_policy_check
  check (
    (workspace_id is null and access_policy = 'members') or
    (workspace_id is not null and access_policy in ('members', 'workspace'))
  );
