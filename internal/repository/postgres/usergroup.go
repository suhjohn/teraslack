package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// UsergroupRepo implements repository.UsergroupRepository using Postgres.
type UsergroupRepo struct {
	pool *pgxpool.Pool
}

// NewUsergroupRepo creates a new UsergroupRepo.
func NewUsergroupRepo(pool *pgxpool.Pool) *UsergroupRepo {
	return &UsergroupRepo{pool: pool}
}

func (r *UsergroupRepo) Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	id := generateID("S")

	var ug domain.Usergroup
	err := r.pool.QueryRow(ctx, `
		INSERT INTO usergroups (id, team_id, name, handle, description, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING id, team_id, name, handle, description, is_external, enabled,
		          user_count, created_by, updated_by, created_at, updated_at`,
		id, params.TeamID, params.Name, params.Handle, params.Description, params.CreatedBy,
	).Scan(
		&ug.ID, &ug.TeamID, &ug.Name, &ug.Handle, &ug.Description,
		&ug.IsExternal, &ug.Enabled, &ug.UserCount,
		&ug.CreatedBy, &ug.UpdatedBy, &ug.CreatedAt, &ug.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, domain.ErrNameTaken
		}
		return nil, fmt.Errorf("insert usergroup: %w", err)
	}

	// Add initial users
	for _, userID := range params.Users {
		if err := r.AddUser(ctx, id, userID); err != nil {
			return nil, fmt.Errorf("add initial user: %w", err)
		}
	}
	if len(params.Users) > 0 {
		ug.UserCount = len(params.Users)
	}

	return &ug, nil
}

func (r *UsergroupRepo) Get(ctx context.Context, id string) (*domain.Usergroup, error) {
	var ug domain.Usergroup
	err := r.pool.QueryRow(ctx, `
		SELECT id, team_id, name, handle, description, is_external, enabled,
		       user_count, created_by, updated_by, created_at, updated_at
		FROM usergroups WHERE id = $1`, id,
	).Scan(
		&ug.ID, &ug.TeamID, &ug.Name, &ug.Handle, &ug.Description,
		&ug.IsExternal, &ug.Enabled, &ug.UserCount,
		&ug.CreatedBy, &ug.UpdatedBy, &ug.CreatedAt, &ug.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get usergroup: %w", err)
	}
	return &ug, nil
}

func (r *UsergroupRepo) Update(ctx context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if params.Name != nil {
		name = *params.Name
	}
	handle := existing.Handle
	if params.Handle != nil {
		handle = *params.Handle
	}
	description := existing.Description
	if params.Description != nil {
		description = *params.Description
	}

	var ug domain.Usergroup
	err = r.pool.QueryRow(ctx, `
		UPDATE usergroups SET name = $2, handle = $3, description = $4, updated_by = $5
		WHERE id = $1
		RETURNING id, team_id, name, handle, description, is_external, enabled,
		          user_count, created_by, updated_by, created_at, updated_at`,
		id, name, handle, description, params.UpdatedBy,
	).Scan(
		&ug.ID, &ug.TeamID, &ug.Name, &ug.Handle, &ug.Description,
		&ug.IsExternal, &ug.Enabled, &ug.UserCount,
		&ug.CreatedBy, &ug.UpdatedBy, &ug.CreatedAt, &ug.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, domain.ErrNameTaken
		}
		return nil, fmt.Errorf("update usergroup: %w", err)
	}
	return &ug, nil
}

func (r *UsergroupRepo) List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	query := `
		SELECT id, team_id, name, handle, description, is_external, enabled,
		       user_count, created_by, updated_by, created_at, updated_at
		FROM usergroups WHERE team_id = $1`
	args := []any{params.TeamID}

	if !params.IncludeDisabled {
		query += ` AND enabled = TRUE`
	}
	query += ` ORDER BY name ASC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list usergroups: %w", err)
	}
	defer rows.Close()

	var groups []domain.Usergroup
	for rows.Next() {
		var ug domain.Usergroup
		if err := rows.Scan(
			&ug.ID, &ug.TeamID, &ug.Name, &ug.Handle, &ug.Description,
			&ug.IsExternal, &ug.Enabled, &ug.UserCount,
			&ug.CreatedBy, &ug.UpdatedBy, &ug.CreatedAt, &ug.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan usergroup: %w", err)
		}
		groups = append(groups, ug)
	}
	if groups == nil {
		groups = []domain.Usergroup{}
	}
	return groups, nil
}

func (r *UsergroupRepo) Enable(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE usergroups SET enabled = TRUE WHERE id = $1 AND enabled = FALSE`, id)
	if err != nil {
		return fmt.Errorf("enable usergroup: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UsergroupRepo) Disable(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE usergroups SET enabled = FALSE WHERE id = $1 AND enabled = TRUE`, id)
	if err != nil {
		return fmt.Errorf("disable usergroup: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UsergroupRepo) AddUser(ctx context.Context, usergroupID, userID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO usergroup_members (usergroup_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, usergroupID, userID)
	if err != nil {
		return fmt.Errorf("add usergroup member: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE usergroups SET user_count = (
			SELECT COUNT(*) FROM usergroup_members WHERE usergroup_id = $1
		) WHERE id = $1`, usergroupID)
	if err != nil {
		return fmt.Errorf("update user count: %w", err)
	}
	return nil
}

func (r *UsergroupRepo) ListUsers(ctx context.Context, usergroupID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT user_id FROM usergroup_members WHERE usergroup_id = $1 ORDER BY added_at ASC`, usergroupID)
	if err != nil {
		return nil, fmt.Errorf("list usergroup members: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan usergroup member: %w", err)
		}
		users = append(users, uid)
	}
	if users == nil {
		users = []string{}
	}
	return users, nil
}

func (r *UsergroupRepo) SetUsers(ctx context.Context, usergroupID string, userIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM usergroup_members WHERE usergroup_id = $1`, usergroupID)
	if err != nil {
		return fmt.Errorf("clear members: %w", err)
	}

	for _, uid := range userIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO usergroup_members (usergroup_id, user_id) VALUES ($1, $2)`,
			usergroupID, uid)
		if err != nil {
			return fmt.Errorf("insert member: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `UPDATE usergroups SET user_count = $2 WHERE id = $1`, usergroupID, len(userIDs))
	if err != nil {
		return fmt.Errorf("update user count: %w", err)
	}

	return tx.Commit(ctx)
}

func isDuplicateKey(err error) bool {
	return err != nil && (errors.Is(err, pgx.ErrNoRows) == false) &&
		(fmt.Sprintf("%v", err) != "" && containsDuplicate(err.Error()))
}

func containsDuplicate(s string) bool {
	for i := 0; i+len("duplicate key")-1 < len(s); i++ {
		if s[i:i+len("duplicate key")] == "duplicate key" {
			return true
		}
	}
	return false
}
