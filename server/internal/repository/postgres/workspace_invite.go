package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type WorkspaceInviteRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewWorkspaceInviteRepo(db DBTX) *WorkspaceInviteRepo {
	return &WorkspaceInviteRepo{q: sqlcgen.New(db), db: db}
}

func (r *WorkspaceInviteRepo) WithTx(tx pgx.Tx) repository.WorkspaceInviteRepository {
	return &WorkspaceInviteRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *WorkspaceInviteRepo) Create(ctx context.Context, params domain.CreateWorkspaceInviteParams, tokenHash string) (*domain.WorkspaceInvite, error) {
	row, err := r.q.CreateWorkspaceInvite(ctx, sqlcgen.CreateWorkspaceInviteParams{
		ID:          generateID("WI"),
		WorkspaceID: params.WorkspaceID,
		Email:       params.Email,
		InvitedBy:   params.InvitedBy,
		TokenHash:   tokenHash,
		ExpiresAt:   params.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace invite: %w", err)
	}
	return workspaceInviteFromCreateRow(row), nil
}

func (r *WorkspaceInviteRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.WorkspaceInvite, error) {
	row, err := r.q.GetWorkspaceInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workspace invite: %w", err)
	}
	return workspaceInviteFromGetRow(row), nil
}

func (r *WorkspaceInviteRepo) MarkAccepted(ctx context.Context, id, acceptedByAccountID string, acceptedAt time.Time) error {
	rowsAffected, err := r.q.MarkWorkspaceInviteAccepted(ctx, sqlcgen.MarkWorkspaceInviteAcceptedParams{
		ID:                  id,
		AcceptedByAccountID: stringPtrToText(&acceptedByAccountID),
		AcceptedAt:          tsToTimePtr(acceptedAt),
	})
	if err != nil {
		return fmt.Errorf("accept workspace invite: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func workspaceInviteFromCreateRow(row sqlcgen.CreateWorkspaceInviteRow) *domain.WorkspaceInvite {
	invite := &domain.WorkspaceInvite{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Email:       row.Email,
		InvitedBy:   row.InvitedBy,
		ExpiresAt:   row.ExpiresAt,
		AcceptedAt:  tsToTimePtr(row.AcceptedAt),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if acceptedByAccountID := textToStringPtr(row.AcceptedByAccountID); acceptedByAccountID != nil {
		invite.AcceptedByAccountID = *acceptedByAccountID
	}
	return invite
}

func workspaceInviteFromGetRow(row sqlcgen.GetWorkspaceInviteByTokenHashRow) *domain.WorkspaceInvite {
	invite := &domain.WorkspaceInvite{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Email:       row.Email,
		InvitedBy:   row.InvitedBy,
		ExpiresAt:   row.ExpiresAt,
		AcceptedAt:  tsToTimePtr(row.AcceptedAt),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if acceptedByAccountID := textToStringPtr(row.AcceptedByAccountID); acceptedByAccountID != nil {
		invite.AcceptedByAccountID = *acceptedByAccountID
	}
	return invite
}
