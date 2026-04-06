-- name: InsertInternalEvent :one
insert into internal_events (
  event_type,
  aggregate_type,
  aggregate_id,
  workspace_id,
  actor_user_id,
  shard_id,
  payload,
  created_at
) values (
  sqlc.arg(event_type),
  sqlc.arg(aggregate_type),
  sqlc.arg(aggregate_id),
  sqlc.narg(workspace_id),
  sqlc.narg(actor_user_id),
  sqlc.arg(shard_id),
  sqlc.arg(payload),
  sqlc.arg(created_at)
)
returning id;
