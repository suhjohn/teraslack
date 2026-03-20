-- name: CreateUsergroup :one
INSERT INTO usergroups (id, team_id, name, handle, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING id, team_id, name, handle, description, is_external, enabled,
          user_count, created_by, updated_by, created_at, updated_at;

-- name: GetUsergroup :one
SELECT id, team_id, name, handle, description, is_external, enabled,
       user_count, created_by, updated_by, created_at, updated_at
FROM usergroups WHERE id = $1;

-- name: UpdateUsergroup :one
UPDATE usergroups SET name = $2, handle = $3, description = $4, updated_by = $5
WHERE id = $1
RETURNING id, team_id, name, handle, description, is_external, enabled,
          user_count, created_by, updated_by, created_at, updated_at;

-- name: ListUsergroups :many
SELECT id, team_id, name, handle, description, is_external, enabled,
       user_count, created_by, updated_by, created_at, updated_at
FROM usergroups WHERE team_id = $1 AND enabled = TRUE
ORDER BY name ASC;

-- name: ListUsergroupsIncludeDisabled :many
SELECT id, team_id, name, handle, description, is_external, enabled,
       user_count, created_by, updated_by, created_at, updated_at
FROM usergroups WHERE team_id = $1
ORDER BY name ASC;

-- name: EnableUsergroup :exec
UPDATE usergroups SET enabled = TRUE WHERE id = $1 AND enabled = FALSE;

-- name: DisableUsergroup :exec
UPDATE usergroups SET enabled = FALSE WHERE id = $1 AND enabled = TRUE;

-- name: AddUsergroupMember :exec
INSERT INTO usergroup_members (usergroup_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListUsergroupMembers :many
SELECT user_id FROM usergroup_members WHERE usergroup_id = $1 ORDER BY added_at ASC;

-- name: DeleteUsergroupMembers :exec
DELETE FROM usergroup_members WHERE usergroup_id = $1;

-- name: InsertUsergroupMember :exec
INSERT INTO usergroup_members (usergroup_id, user_id) VALUES ($1, $2);

-- name: UpdateUsergroupUserCount :exec
UPDATE usergroups SET user_count = (
    SELECT COUNT(*) FROM usergroup_members WHERE usergroup_id = $1
) WHERE id = $1;

-- name: SetUsergroupUserCount :exec
UPDATE usergroups SET user_count = $2 WHERE id = $1;
