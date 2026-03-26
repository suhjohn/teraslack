package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type RoleAssignmentRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewRoleAssignmentRepo(db DBTX) *RoleAssignmentRepo {
	return &RoleAssignmentRepo{q: sqlcgen.New(db), db: db}
}

func (r *RoleAssignmentRepo) WithTx(tx pgx.Tx) repository.RoleAssignmentRepository {
	return &RoleAssignmentRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *RoleAssignmentRepo) ListByUser(ctx context.Context, workspaceID, userID string) ([]domain.DelegatedRole, error) {
	rows, err := r.q.ListUserRoleAssignments(ctx, sqlcgen.ListUserRoleAssignmentsParams{
		WorkspaceID: workspaceID,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
	}

	roles := []domain.DelegatedRole{}
	for _, role := range rows {
		roles = append(roles, domain.DelegatedRole(role))
	}
	return roles, nil
}

func (r *RoleAssignmentRepo) ReplaceForUser(ctx context.Context, workspaceID, userID string, roles []domain.DelegatedRole, assignedBy string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteUserRoleAssignments(ctx, sqlcgen.DeleteUserRoleAssignmentsParams{WorkspaceID: workspaceID, UserID: userID}); err != nil {
		return fmt.Errorf("delete existing user roles: %w", err)
	}
	for _, role := range roles {
		if err := qtx.InsertUserRoleAssignment(ctx, sqlcgen.InsertUserRoleAssignmentParams{
			ID:         generateID("URA"),
			WorkspaceID:     workspaceID,
			UserID:     userID,
			RoleKey:    string(role),
			AssignedBy: assignedBy,
		}); err != nil {
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
