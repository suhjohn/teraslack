package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type ExternalPrincipalAccessRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewExternalPrincipalAccessRepo(db DBTX) *ExternalPrincipalAccessRepo {
	return &ExternalPrincipalAccessRepo{q: sqlcgen.New(db), db: db}
}

func (r *ExternalPrincipalAccessRepo) WithTx(tx pgx.Tx) repository.ExternalPrincipalAccessRepository {
	return &ExternalPrincipalAccessRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ExternalPrincipalAccessRepo) Create(ctx context.Context, params domain.CreateExternalPrincipalAccessParams) (*domain.ExternalPrincipalAccess, error) {
	id := generateID("EPA")
	capsJSON, err := json.Marshal(params.AllowedCapabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed capabilities: %w", err)
	}

	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	qtx := r.q.WithTx(tx)

	if err := qtx.CreateExternalPrincipalAccess(ctx, sqlcgen.CreateExternalPrincipalAccessParams{
		ID:                  id,
		HostWorkspaceID:          params.HostWorkspaceID,
		PrincipalID:         params.PrincipalID,
		PrincipalType:       string(params.PrincipalType),
		HomeWorkspaceID:          params.HomeWorkspaceID,
		AccessMode:          string(params.AccessMode),
		AllowedCapabilities: capsJSON,
		GrantedBy:           params.GrantedBy,
		ExpiresAt:           timeToPgTimestamptz(params.ExpiresAt),
	}); err != nil {
		return nil, fmt.Errorf("insert external principal access: %w", err)
	}

	if err := replaceExternalPrincipalConversationAssignments(ctx, tx, id, params.ConversationIDs, params.GrantedBy); err != nil {
		return nil, err
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}
	return r.Get(ctx, id)
}

func (r *ExternalPrincipalAccessRepo) Get(ctx context.Context, id string) (*domain.ExternalPrincipalAccess, error) {
	row, err := r.q.GetExternalPrincipalAccess(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get external principal access: %w", err)
	}
	access := externalPrincipalAccessFromSQLC(row)
	if len(row.AllowedCapabilities) > 0 {
		if err := json.Unmarshal(row.AllowedCapabilities, &access.AllowedCapabilities); err != nil {
			return nil, fmt.Errorf("decode allowed capabilities: %w", err)
		}
	}
	conversationIDs, err := r.ListConversationIDs(ctx, access.ID)
	if err != nil {
		return nil, err
	}
	access.ConversationIDs = conversationIDs
	return access, nil
}

func (r *ExternalPrincipalAccessRepo) GetActiveByPrincipal(ctx context.Context, hostWorkspaceID, principalID string) (*domain.ExternalPrincipalAccess, error) {
	id, err := r.q.GetActiveExternalPrincipalAccessByPrincipal(ctx, sqlcgen.GetActiveExternalPrincipalAccessByPrincipalParams{
		HostWorkspaceID:  hostWorkspaceID,
		PrincipalID: principalID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active external principal access: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *ExternalPrincipalAccessRepo) List(ctx context.Context, params domain.ListExternalPrincipalAccessParams) ([]domain.ExternalPrincipalAccess, error) {
	rows, err := r.q.ListExternalPrincipalAccessIDs(ctx, params.HostWorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list external principal access: %w", err)
	}

	items := []domain.ExternalPrincipalAccess{}
	for _, id := range rows {
		item, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *ExternalPrincipalAccessRepo) Update(ctx context.Context, id string, params domain.UpdateExternalPrincipalAccessParams) (*domain.ExternalPrincipalAccess, error) {
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

	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	qtx := r.q.WithTx(tx)

	if err := qtx.UpdateExternalPrincipalAccess(ctx, sqlcgen.UpdateExternalPrincipalAccessParams{
		ID:                  id,
		AccessMode:          string(existing.AccessMode),
		AllowedCapabilities: capsJSON,
		ExpiresAt:           timeToPgTimestamptz(existing.ExpiresAt),
		RevokedAt:           timeToPgTimestamptz(existing.RevokedAt),
	}); err != nil {
		return nil, fmt.Errorf("update external principal access: %w", err)
	}

	if params.ConversationIDs != nil {
		if err := replaceExternalPrincipalConversationAssignments(ctx, tx, id, *params.ConversationIDs, existing.GrantedBy); err != nil {
			return nil, err
		}
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}
	return r.Get(ctx, id)
}

func (r *ExternalPrincipalAccessRepo) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	if err := r.q.RevokeExternalPrincipalAccess(ctx, sqlcgen.RevokeExternalPrincipalAccessParams{
		ID:        id,
		RevokedAt: timeToPgTimestamptz(&revokedAt),
	}); err != nil {
		return fmt.Errorf("revoke external principal access: %w", err)
	}
	return nil
}

func (r *ExternalPrincipalAccessRepo) HasConversationAccess(ctx context.Context, accessID, conversationID string) (bool, error) {
	exists, err := r.q.HasExternalPrincipalConversationAccess(ctx, sqlcgen.HasExternalPrincipalConversationAccessParams{
		AccessID:       accessID,
		ConversationID: conversationID,
	})
	if err != nil {
		return false, fmt.Errorf("check external principal conversation access: %w", err)
	}
	return exists, nil
}

func (r *ExternalPrincipalAccessRepo) ListConversationIDs(ctx context.Context, accessID string) ([]string, error) {
	rows, err := r.q.ListExternalPrincipalConversationAssignments(ctx, accessID)
	if err != nil {
		return nil, fmt.Errorf("list external principal conversation assignments: %w", err)
	}
	return rows, nil
}

func (r *ExternalPrincipalAccessRepo) ReplaceConversationAssignments(ctx context.Context, accessID string, conversationIDs []string, grantedBy string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	if err := replaceExternalPrincipalConversationAssignments(ctx, tx, accessID, conversationIDs, grantedBy); err != nil {
		return err
	}
	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}
	}
	return nil
}

func replaceExternalPrincipalConversationAssignments(ctx context.Context, db DBTX, accessID string, conversationIDs []string, grantedBy string) error {
	q := sqlcgen.New(db)
	if err := q.DeleteExternalPrincipalConversationAssignments(ctx, accessID); err != nil {
		return fmt.Errorf("delete external principal conversation assignments: %w", err)
	}
	for _, conversationID := range conversationIDs {
		if err := q.InsertExternalPrincipalConversationAssignment(ctx, sqlcgen.InsertExternalPrincipalConversationAssignmentParams{
			AccessID:       accessID,
			ConversationID: conversationID,
			GrantedBy:      grantedBy,
		}); err != nil {
			return fmt.Errorf("insert external principal conversation assignment: %w", err)
		}
	}
	return nil
}

func externalPrincipalAccessFromSQLC(row sqlcgen.ExternalPrincipalAccess) *domain.ExternalPrincipalAccess {
	return &domain.ExternalPrincipalAccess{
		ID:            row.ID,
		HostWorkspaceID: row.HostWorkspaceID,
		PrincipalID:   row.PrincipalID,
		PrincipalType: domain.PrincipalType(row.PrincipalType),
		HomeWorkspaceID: row.HomeWorkspaceID,
		AccessMode:    domain.ExternalPrincipalAccessMode(row.AccessMode),
		GrantedBy:     row.GrantedBy,
		CreatedAt:     tsToTime(row.CreatedAt),
		ExpiresAt:     tsToTimePtr(row.ExpiresAt),
		RevokedAt:     tsToTimePtr(row.RevokedAt),
	}
}
