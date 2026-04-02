package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type ExternalMemberRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewExternalMemberRepo(db DBTX) *ExternalMemberRepo {
	return &ExternalMemberRepo{q: sqlcgen.New(db), db: db}
}

func (r *ExternalMemberRepo) WithTx(tx pgx.Tx) repository.ExternalMemberRepository {
	return &ExternalMemberRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ExternalMemberRepo) Create(ctx context.Context, params domain.CreateExternalMemberParams, hostWorkspaceID string) (*domain.ExternalMember, error) {
	capsJSON, err := json.Marshal(params.AllowedCapabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed capabilities: %w", err)
	}
	row, err := r.q.CreateExternalMember(ctx, sqlcgen.CreateExternalMemberParams{
		ID:                  generateID("EM"),
		ConversationID:      params.ConversationID,
		HostWorkspaceID:     hostWorkspaceID,
		ExternalWorkspaceID: params.ExternalWorkspaceID,
		AccountID:           params.AccountID,
		AccessMode:          string(params.AccessMode),
		AllowedCapabilities: capsJSON,
		InvitedBy:           params.InvitedBy,
		ExpiresAt:           params.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("create external member: %w", err)
	}
	return externalMemberFromRow(row.ID, row.ConversationID, row.HostWorkspaceID, row.ExternalWorkspaceID, row.AccountID, row.AccessMode, row.AllowedCapabilities, row.InvitedBy, row.CreatedAt, row.ExpiresAt, row.RevokedAt)
}

func (r *ExternalMemberRepo) Get(ctx context.Context, id string) (*domain.ExternalMember, error) {
	row, err := r.q.GetExternalMember(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get external member: %w", err)
	}
	return externalMemberFromRow(row.ID, row.ConversationID, row.HostWorkspaceID, row.ExternalWorkspaceID, row.AccountID, row.AccessMode, row.AllowedCapabilities, row.InvitedBy, row.CreatedAt, row.ExpiresAt, row.RevokedAt)
}

func (r *ExternalMemberRepo) GetActiveByConversationAndAccount(ctx context.Context, conversationID, accountID string) (*domain.ExternalMember, error) {
	row, err := r.q.GetActiveExternalMemberByConversationAndAccount(ctx, sqlcgen.GetActiveExternalMemberByConversationAndAccountParams{
		ConversationID: conversationID,
		AccountID:      accountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active external member by conversation/account: %w", err)
	}
	return externalMemberFromRow(row.ID, row.ConversationID, row.HostWorkspaceID, row.ExternalWorkspaceID, row.AccountID, row.AccessMode, row.AllowedCapabilities, row.InvitedBy, row.CreatedAt, row.ExpiresAt, row.RevokedAt)
}

func (r *ExternalMemberRepo) ListActiveByAccountAndWorkspace(ctx context.Context, accountID, workspaceID string) ([]domain.ExternalMember, error) {
	rows, err := r.q.ListActiveExternalMembersByAccountAndWorkspace(ctx, sqlcgen.ListActiveExternalMembersByAccountAndWorkspaceParams{
		AccountID:       accountID,
		HostWorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list active external members by account/workspace: %w", err)
	}
	items := make([]domain.ExternalMember, 0, len(rows))
	for _, row := range rows {
		item, err := externalMemberFromRow(row.ID, row.ConversationID, row.HostWorkspaceID, row.ExternalWorkspaceID, row.AccountID, row.AccessMode, row.AllowedCapabilities, row.InvitedBy, row.CreatedAt, row.ExpiresAt, row.RevokedAt)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *ExternalMemberRepo) ListByConversation(ctx context.Context, conversationID string) ([]domain.ExternalMember, error) {
	rows, err := r.q.ListExternalMembersByConversation(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list external members by conversation: %w", err)
	}
	items := make([]domain.ExternalMember, 0, len(rows))
	for _, row := range rows {
		item, err := externalMemberFromRow(row.ID, row.ConversationID, row.HostWorkspaceID, row.ExternalWorkspaceID, row.AccountID, row.AccessMode, row.AllowedCapabilities, row.InvitedBy, row.CreatedAt, row.ExpiresAt, row.RevokedAt)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *ExternalMemberRepo) Update(ctx context.Context, id string, params domain.UpdateExternalMemberParams) (*domain.ExternalMember, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if params.AccessMode != nil {
		existing.AccessMode = *params.AccessMode
	}
	if params.AllowedCapabilities != nil {
		existing.AllowedCapabilities = append([]string(nil), (*params.AllowedCapabilities)...)
	}
	if params.ExpiresAt != nil {
		existing.ExpiresAt = params.ExpiresAt
	}
	capsJSON, err := json.Marshal(existing.AllowedCapabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed capabilities: %w", err)
	}
	if err := r.q.UpdateExternalMember(ctx, sqlcgen.UpdateExternalMemberParams{
		ID:                  id,
		AccessMode:          string(existing.AccessMode),
		AllowedCapabilities: capsJSON,
		ExpiresAt:           existing.ExpiresAt,
		RevokedAt:           existing.RevokedAt,
	}); err != nil {
		return nil, fmt.Errorf("update external member: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *ExternalMemberRepo) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	if err := r.q.RevokeExternalMember(ctx, sqlcgen.RevokeExternalMemberParams{
		ID:        id,
		RevokedAt: &revokedAt,
	}); err != nil {
		return fmt.Errorf("revoke external member: %w", err)
	}
	return nil
}

func (r *ExternalMemberRepo) RevokeByExternalWorkspace(ctx context.Context, hostWorkspaceID, externalWorkspaceID string, revokedAt time.Time) error {
	if err := r.q.RevokeExternalMembersByExternalWorkspace(ctx, sqlcgen.RevokeExternalMembersByExternalWorkspaceParams{
		HostWorkspaceID:     hostWorkspaceID,
		ExternalWorkspaceID: externalWorkspaceID,
		RevokedAt:           &revokedAt,
	}); err != nil {
		return fmt.Errorf("revoke external members by external workspace: %w", err)
	}
	return nil
}

func externalMemberFromRow(id, conversationID, hostWorkspaceID, externalWorkspaceID, accountID, accessMode string, capsJSON []byte, invitedBy string, createdAt time.Time, expiresAt, revokedAt *time.Time) (*domain.ExternalMember, error) {
	item := &domain.ExternalMember{
		ID:                  id,
		ConversationID:      conversationID,
		HostWorkspaceID:     hostWorkspaceID,
		ExternalWorkspaceID: externalWorkspaceID,
		AccountID:           accountID,
		AccessMode:          domain.ExternalPrincipalAccessMode(accessMode),
		InvitedBy:           invitedBy,
		CreatedAt:           createdAt,
		ExpiresAt:           expiresAt,
		RevokedAt:           revokedAt,
	}
	if len(capsJSON) > 0 {
		if err := json.Unmarshal(capsJSON, &item.AllowedCapabilities); err != nil {
			return nil, fmt.Errorf("decode external member capabilities: %w", err)
		}
	}
	return item, nil
}
