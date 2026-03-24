package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type WorkspaceInviteRepo struct {
	db DBTX
}

func NewWorkspaceInviteRepo(db DBTX) *WorkspaceInviteRepo {
	return &WorkspaceInviteRepo{db: db}
}

func (r *WorkspaceInviteRepo) WithTx(tx pgx.Tx) repository.WorkspaceInviteRepository {
	return &WorkspaceInviteRepo{db: tx}
}

func (r *WorkspaceInviteRepo) Create(ctx context.Context, params domain.CreateWorkspaceInviteParams, tokenHash string) (*domain.WorkspaceInvite, error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO workspace_invites (id, team_id, email, invited_by, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, team_id, email, invited_by, accepted_by_user_id, expires_at, accepted_at, created_at, updated_at
	`,
		generateID("WI"),
		params.TeamID,
		params.Email,
		params.InvitedBy,
		tokenHash,
		params.ExpiresAt,
	)
	invite, err := scanWorkspaceInvite(row)
	if err != nil {
		return nil, fmt.Errorf("insert workspace invite: %w", err)
	}
	return invite, nil
}

func (r *WorkspaceInviteRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.WorkspaceInvite, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, team_id, email, invited_by, accepted_by_user_id, expires_at, accepted_at, created_at, updated_at
		FROM workspace_invites
		WHERE token_hash = $1
	`, tokenHash)
	invite, err := scanWorkspaceInvite(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workspace invite: %w", err)
	}
	return invite, nil
}

func (r *WorkspaceInviteRepo) MarkAccepted(ctx context.Context, id, acceptedByUserID string, acceptedAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE workspace_invites
		SET accepted_by_user_id = $2,
			accepted_at = $3
		WHERE id = $1
	`, id, acceptedByUserID, acceptedAt)
	if err != nil {
		return fmt.Errorf("accept workspace invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanWorkspaceInvite(row rowScanner) (*domain.WorkspaceInvite, error) {
	var invite domain.WorkspaceInvite
	var acceptedByUserID *string
	if err := row.Scan(
		&invite.ID,
		&invite.TeamID,
		&invite.Email,
		&invite.InvitedBy,
		&acceptedByUserID,
		&invite.ExpiresAt,
		&invite.AcceptedAt,
		&invite.CreatedAt,
		&invite.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if acceptedByUserID != nil {
		invite.AcceptedByUserID = *acceptedByUserID
	}
	return &invite, nil
}
