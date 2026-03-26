-- name: ListUserRoleAssignments :many
SELECT role_key
FROM user_role_assignments
WHERE workspace_id = $1 AND user_id = $2
ORDER BY role_key ASC;

-- name: DeleteUserRoleAssignments :exec
DELETE FROM user_role_assignments
WHERE workspace_id = $1 AND user_id = $2;

-- name: InsertUserRoleAssignment :exec
INSERT INTO user_role_assignments (id, workspace_id, user_id, role_key, assigned_by)
VALUES ($1, $2, $3, $4, $5);
