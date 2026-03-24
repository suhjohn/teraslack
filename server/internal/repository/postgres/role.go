package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type RoleAssignmentRepo struct {
	db DBTX
}

func NewRoleAssignmentRepo(db DBTX) *RoleAssignmentRepo {
	return &RoleAssignmentRepo{db: db}
}

func (r *RoleAssignmentRepo) WithTx(tx pgx.Tx) repository.RoleAssignmentRepository {
	return &RoleAssignmentRepo{db: tx}
}

func (r *RoleAssignmentRepo) ListByUser(ctx context.Context, teamID, userID string) ([]domain.DelegatedRole, error) {
	rows, err := r.db.Query(ctx, `
		SELECT role_key
		FROM user_role_assignments
		WHERE team_id = $1 AND user_id = $2
		ORDER BY role_key ASC
	`, teamID, userID)
	if err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
	}
	defer rows.Close()

	roles := []domain.DelegatedRole{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		roles = append(roles, domain.DelegatedRole(role))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user roles: %w", err)
	}
	return roles, nil
}

func (r *RoleAssignmentRepo) ReplaceForUser(ctx context.Context, teamID, userID string, roles []domain.DelegatedRole, assignedBy string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM user_role_assignments WHERE team_id = $1 AND user_id = $2`, teamID, userID); err != nil {
		return fmt.Errorf("delete existing user roles: %w", err)
	}
	for _, role := range roles {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_role_assignments (id, team_id, user_id, role_key, assigned_by)
			VALUES ($1, $2, $3, $4, $5)
		`, generateID("URA"), teamID, userID, string(role), assignedBy); err != nil {
			return fmt.Errorf("insert user role: %w", err)
		}
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}
	}
	return nil
}
