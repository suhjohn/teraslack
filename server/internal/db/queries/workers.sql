-- name: ClaimLease :execrows
insert into projector_leases (name, shard_id, owner, lease_until, updated_at)
values ($1, $2, $3, $4, $5)
on conflict (name, shard_id) do update
set owner = excluded.owner,
    lease_until = excluded.lease_until,
    updated_at = excluded.updated_at
where projector_leases.owner = excluded.owner
   or projector_leases.lease_until < excluded.updated_at;

-- name: GetCheckpointForUpdate :one
select last_event_id
from projector_checkpoints
where name = $1
for update;

-- name: InsertCheckpointIfMissing :exec
insert into projector_checkpoints (name, last_event_id, updated_at)
values ($1, $2, $3)
on conflict do nothing;

-- name: UpdateCheckpoint :exec
update projector_checkpoints
set last_event_id = $2, updated_at = $3
where name = $1;

-- name: ListInternalEventsByShardAfterID :many
select id, event_type, workspace_id, payload, created_at
from internal_events
where shard_id = $1
  and id > $2
order by id asc
limit $3;

-- name: GetInternalEventForProjection :one
select id, event_type, workspace_id, payload, created_at
from internal_events
where id = sqlc.arg(id);

-- name: InsertExternalProjectionFailure :exec
insert into external_event_projection_failures (internal_event_id, error, created_at)
values ($1, $2, $3);

-- name: InsertExternalEvent :one
insert into external_events (
  workspace_id,
  type,
  resource_type,
  resource_id,
  occurred_at,
  payload,
  source_internal_event_id,
  dedupe_key,
  created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
on conflict (dedupe_key) do update
set dedupe_key = excluded.dedupe_key
returning id;

-- name: InsertWorkspaceEventFeed :exec
insert into workspace_event_feed (workspace_id, external_event_id)
values ($1, $2)
on conflict do nothing;

-- name: InsertConversationEventFeed :exec
insert into conversation_event_feed (conversation_id, external_event_id)
values ($1, $2)
on conflict do nothing;

-- name: InsertUserEventFeed :exec
insert into user_event_feed (user_id, external_event_id)
values ($1, $2)
on conflict do nothing;

-- name: ListExternalEventsAfterID :many
select id, resource_type, resource_id
from external_events
where id > $1
order by id asc
limit $2;

-- name: ListExternalEventsForWebhookQueueAfterID :many
select id, workspace_id, type, resource_type, resource_id
from external_events
where id > sqlc.arg(id)
order by id asc
limit sqlc.arg(batch_limit);

-- name: ListWebhookSubscriptionsForExternalEvent :many
select es.id
from event_subscriptions es
where es.enabled = true
  and (
    exists (
      select 1
      from user_event_feed uef
      where uef.external_event_id = sqlc.arg(external_event_id)
        and uef.user_id = es.owner_user_id
    )
    or exists (
      select 1
      from workspace_event_feed wef
      join workspace_memberships wm
        on wm.workspace_id = wef.workspace_id
       and wm.user_id = es.owner_user_id
       and wm.status = 'active'
      where wef.external_event_id = sqlc.arg(external_event_id)
    )
    or exists (
      select 1
      from conversation_event_feed cef
      join conversations c on c.id = cef.conversation_id
      where cef.external_event_id = sqlc.arg(external_event_id)
        and (
          (c.workspace_id is null and c.access_policy = 'authenticated')
          or (
            c.workspace_id is null
            and c.access_policy = 'members'
            and exists (
              select 1
              from conversation_participants cp
              where cp.conversation_id = c.id
                and cp.user_id = es.owner_user_id
            )
          )
          or (
            c.workspace_id is not null
            and exists (
              select 1
              from workspace_memberships wm
              where wm.workspace_id = c.workspace_id
                and wm.user_id = es.owner_user_id
                and wm.status = 'active'
            )
            and (
              c.access_policy = 'workspace'
              or exists (
                select 1
                from conversation_participants cp
                where cp.conversation_id = c.id
                  and cp.user_id = es.owner_user_id
              )
            )
          )
        )
    )
  )
  and (sqlc.narg(workspace_id)::uuid is null or es.workspace_id = sqlc.narg(workspace_id))
  and (sqlc.arg(type)::text = '' or es.event_type = sqlc.arg(type))
  and (sqlc.arg(resource_type)::text = '' or es.resource_type = sqlc.arg(resource_type))
  and (sqlc.narg(resource_id)::uuid is null or es.resource_id = sqlc.narg(resource_id))
order by es.id asc;

-- name: GetWebhookDeliverySource :one
select es.url, es.encrypted_secret, ee.id as event_id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload
from event_subscriptions es
join external_events ee on ee.id = sqlc.arg(external_event_id)
where es.id = sqlc.arg(subscription_id)
  and es.enabled = true;

-- name: GetUserSearchSource :one
select p.display_name, p.handle, u.email
from users u
join user_profiles p on p.user_id = u.id
where u.id = $1 and u.status = 'active';

-- name: DeleteUserSearchDocument :exec
delete from search_documents
where entity_type = 'user' and entity_id = $1;

-- name: UpsertUserSearchDocument :exec
insert into search_documents (entity_type, entity_id, workspace_id, title, subtitle, content, updated_at)
values ('user', $1, null, $2, $3, $4, $5)
on conflict (entity_type, entity_id) do update
set title = excluded.title,
    subtitle = excluded.subtitle,
    content = excluded.content,
    updated_at = excluded.updated_at;

-- name: GetWorkspaceSearchSource :one
select name, slug
from workspaces
where id = $1;

-- name: DeleteWorkspaceSearchDocument :exec
delete from search_documents
where entity_type = 'workspace' and entity_id = $1;

-- name: UpsertWorkspaceSearchDocument :exec
insert into search_documents (entity_type, entity_id, workspace_id, title, subtitle, content, updated_at)
values ('workspace', $1, $1, $2, $3, $4, $5)
on conflict (entity_type, entity_id) do update
set workspace_id = excluded.workspace_id,
    title = excluded.title,
    subtitle = excluded.subtitle,
    content = excluded.content,
    updated_at = excluded.updated_at;

-- name: GetConversationSearchSource :one
select workspace_id, title, description, access_policy
from conversations
where id = $1;

-- name: ListConversationParticipantIdentities :many
select p.display_name, p.handle
from conversation_participants cp
join user_profiles p on p.user_id = cp.user_id
where cp.conversation_id = $1
order by p.display_name asc;

-- name: DeleteConversationSearchDocument :exec
delete from search_documents
where entity_type = 'conversation' and entity_id = $1;

-- name: UpsertConversationSearchDocument :exec
insert into search_documents (entity_type, entity_id, workspace_id, title, subtitle, content, updated_at)
values ('conversation', $1, $2, $3, $4, $5, $6)
on conflict (entity_type, entity_id) do update
set workspace_id = excluded.workspace_id,
    title = excluded.title,
    subtitle = excluded.subtitle,
    content = excluded.content,
    updated_at = excluded.updated_at;

-- name: EnqueueWebhookDeliveries :exec
insert into webhook_deliveries (subscription_id, external_event_id, status, next_attempt_at, created_at, updated_at)
select es.id, ee.id, 'pending', now(), now(), now()
from event_subscriptions es
join external_events ee on true
where es.enabled = true
  and (
    exists (
      select 1
      from user_event_feed uef
      where uef.external_event_id = ee.id
        and uef.user_id = es.owner_user_id
    )
    or exists (
      select 1
      from workspace_event_feed wef
      join workspace_memberships wm
        on wm.workspace_id = wef.workspace_id
       and wm.user_id = es.owner_user_id
       and wm.status = 'active'
      where wef.external_event_id = ee.id
    )
    or exists (
      select 1
      from conversation_event_feed cef
      join conversations c on c.id = cef.conversation_id
      where cef.external_event_id = ee.id
        and (
          (c.workspace_id is null and c.access_policy = 'authenticated')
          or (
            c.workspace_id is null
            and c.access_policy = 'members'
            and exists (
              select 1
              from conversation_participants cp
              where cp.conversation_id = c.id
                and cp.user_id = es.owner_user_id
            )
          )
          or (
            c.workspace_id is not null
            and exists (
              select 1
              from workspace_memberships wm
              where wm.workspace_id = c.workspace_id
                and wm.user_id = es.owner_user_id
                and wm.status = 'active'
            )
            and (
              c.access_policy = 'workspace'
              or exists (
                select 1
                from conversation_participants cp
                where cp.conversation_id = c.id
                  and cp.user_id = es.owner_user_id
              )
            )
          )
        )
    )
  )
  and (es.workspace_id is null or es.workspace_id = ee.workspace_id)
  and (es.event_type is null or es.event_type = ee.type)
  and (es.resource_type is null or es.resource_type = ee.resource_type)
  and (es.resource_id is null or es.resource_id = ee.resource_id)
on conflict (subscription_id, external_event_id) do nothing;

-- name: ClaimPendingWebhookDeliveries :many
with claimed as (
  select wd.id
  from webhook_deliveries wd
  where (
    wd.status in ('pending', 'failed')
    and wd.next_attempt_at <= now()
  ) or (
    wd.status = 'processing'
    and wd.updated_at <= now() - interval '5 minutes'
  )
  order by wd.id asc
  for update skip locked
  limit $1
)
update webhook_deliveries wd
set status = 'processing',
    updated_at = now()
from claimed, event_subscriptions es, external_events ee
where wd.id = claimed.id
  and es.id = wd.subscription_id
  and ee.id = wd.external_event_id
returning wd.id, es.url, es.encrypted_secret, ee.id as event_id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload;

-- name: MarkWebhookDeliveryDelivered :exec
update webhook_deliveries
set status = 'delivered',
    delivered_at = now(),
    attempt_count = attempt_count + 1,
    updated_at = now(),
    last_error = null
where id = $1;

-- name: MarkWebhookDeliveryFailed :exec
update webhook_deliveries
set status = 'failed',
    attempt_count = attempt_count + 1,
    updated_at = now(),
    next_attempt_at = $2,
    last_error = $3
where id = $1;
