package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type WorkspaceMembershipRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewWorkspaceMembershipRepo(db DBTX) *WorkspaceMembershipRepo {
	return &WorkspaceMembershipRepo{q: sqlcgen.New(db), db: db}
}

func (r *WorkspaceMembershipRepo) WithTx(tx pgx.Tx) repository.WorkspaceMembershipRepository {
	return &WorkspaceMembershipRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *WorkspaceMembershipRepo) Create(ctx context.Context, params domain.CreateWorkspaceMembershipParams) (*domain.WorkspaceMembership, error) {
	row, err := r.q.CreateWorkspaceMembership(ctx, sqlcgen.CreateWorkspaceMembershipParams{
		ID:          generateID("WM"),
		AccountID:   params.AccountID,
		WorkspaceID: params.WorkspaceID,
		UserID:      stringToText(params.UserID),
		AccountType: string(params.AccountType),
	})
	if err != nil {
		return nil, fmt.Errorf("create workspace membership: %w", err)
	}
	return workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt), nil
}

func (r *WorkspaceMembershipRepo) GetByWorkspaceAndAccount(ctx context.Context, workspaceID, accountID string) (*domain.WorkspaceMembership, error) {
	row, err := r.q.GetWorkspaceMembershipByWorkspaceAndAccount(ctx, sqlcgen.GetWorkspaceMembershipByWorkspaceAndAccountParams{
		WorkspaceID: workspaceID,
		AccountID:   accountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workspace membership by workspace/account: %w", err)
	}
	return workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt), nil
}

func (r *WorkspaceMembershipRepo) GetByLegacyUserID(ctx context.Context, userID string) (*domain.WorkspaceMembership, error) {
	row, err := r.q.GetWorkspaceMembershipByLegacyUserID(ctx, stringToText(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workspace membership by legacy user id: %w", err)
	}
	return workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt), nil
}

func (r *WorkspaceMembershipRepo) ListByAccount(ctx context.Context, accountID string) ([]domain.WorkspaceMembership, error) {
	rows, err := r.q.ListWorkspaceMembershipsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("list workspace memberships by account: %w", err)
	}
	items := make([]domain.WorkspaceMembership, 0, len(rows))
	for _, row := range rows {
		items = append(items, *workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt))
	}
	return items, nil
}

func (r *WorkspaceMembershipRepo) ListByWorkspace(ctx context.Context, workspaceID string) ([]domain.WorkspaceMembership, error) {
	rows, err := r.q.ListWorkspaceMembershipsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace memberships by workspace: %w", err)
	}
	items := make([]domain.WorkspaceMembership, 0, len(rows))
	for _, row := range rows {
		items = append(items, *workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt))
	}
	return items, nil
}

func (r *WorkspaceMembershipRepo) AttachUser(ctx context.Context, id, userID string) (*domain.WorkspaceMembership, error) {
	row, err := r.q.AttachWorkspaceMembershipUser(ctx, sqlcgen.AttachWorkspaceMembershipUserParams{
		ID:     id,
		UserID: stringToText(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("attach workspace membership user: %w", err)
	}
	return workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt), nil
}

func (r *WorkspaceMembershipRepo) UpdateAccountTypeByLegacyUserID(ctx context.Context, userID string, accountType domain.AccountType) (*domain.WorkspaceMembership, error) {
	row, err := r.q.UpdateWorkspaceMembershipAccountTypeByLegacyUserID(ctx, sqlcgen.UpdateWorkspaceMembershipAccountTypeByLegacyUserIDParams{
		UserID:      stringToText(userID),
		AccountType: string(accountType),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update workspace membership account type by legacy user id: %w", err)
	}
	return workspaceMembershipFromRow(row.ID, row.AccountID, row.WorkspaceID, row.UserID, row.AccountType, row.CreatedAt, row.UpdatedAt), nil
}

func workspaceMembershipFromRow(id, accountID, workspaceID string, userIDText any, accountType string, createdAt, updatedAt time.Time) *domain.WorkspaceMembership {
	userID := ""
	switch v := userIDText.(type) {
	case string:
		userID = v
	case pgtype.Text:
		if ptr := textToStringPtr(v); ptr != nil {
			userID = *ptr
		}
	}
	return &domain.WorkspaceMembership{
		ID:          id,
		AccountID:   accountID,
		WorkspaceID: workspaceID,
		UserID:      userID,
		AccountType: domain.AccountType(accountType),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}
