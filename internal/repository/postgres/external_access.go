package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ExternalPrincipalAccessRepo struct {
	db DBTX
}

func NewExternalPrincipalAccessRepo(db DBTX) *ExternalPrincipalAccessRepo {
	return &ExternalPrincipalAccessRepo{db: db}
}

func (r *ExternalPrincipalAccessRepo) WithTx(tx pgx.Tx) repository.ExternalPrincipalAccessRepository {
	return &ExternalPrincipalAccessRepo{db: tx}
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

	if _, err := tx.Exec(ctx, `
		INSERT INTO external_principal_access (
			id, host_team_id, principal_id, principal_type, home_team_id, access_mode,
			allowed_capabilities, granted_by, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, params.HostTeamID, params.PrincipalID, string(params.PrincipalType), params.HomeTeamID, string(params.AccessMode), capsJSON, params.GrantedBy, params.ExpiresAt); err != nil {
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
	var (
		access   domain.ExternalPrincipalAccess
		mode     string
		ptype    string
		capsJSON []byte
	)
	err := r.db.QueryRow(ctx, `
		SELECT id, host_team_id, principal_id, principal_type, home_team_id, access_mode,
		       allowed_capabilities, granted_by, created_at, expires_at, revoked_at
		FROM external_principal_access
		WHERE id = $1
	`, id).Scan(
		&access.ID,
		&access.HostTeamID,
		&access.PrincipalID,
		&ptype,
		&access.HomeTeamID,
		&mode,
		&capsJSON,
		&access.GrantedBy,
		&access.CreatedAt,
		&access.ExpiresAt,
		&access.RevokedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get external principal access: %w", err)
	}
	access.PrincipalType = domain.PrincipalType(ptype)
	access.AccessMode = domain.ExternalPrincipalAccessMode(mode)
	if len(capsJSON) > 0 {
		if err := json.Unmarshal(capsJSON, &access.AllowedCapabilities); err != nil {
			return nil, fmt.Errorf("decode allowed capabilities: %w", err)
		}
	}
	conversationIDs, err := r.ListConversationIDs(ctx, access.ID)
	if err != nil {
		return nil, err
	}
	access.ConversationIDs = conversationIDs
	return &access, nil
}

func (r *ExternalPrincipalAccessRepo) GetActiveByPrincipal(ctx context.Context, hostTeamID, principalID string) (*domain.ExternalPrincipalAccess, error) {
	var id string
	err := r.db.QueryRow(ctx, `
		SELECT id
		FROM external_principal_access
		WHERE host_team_id = $1
		  AND principal_id = $2
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC
		LIMIT 1
	`, hostTeamID, principalID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active external principal access: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *ExternalPrincipalAccessRepo) List(ctx context.Context, params domain.ListExternalPrincipalAccessParams) ([]domain.ExternalPrincipalAccess, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id
		FROM external_principal_access
		WHERE host_team_id = $1
		ORDER BY created_at DESC, id DESC
	`, params.HostTeamID)
	if err != nil {
		return nil, fmt.Errorf("list external principal access: %w", err)
	}
	defer rows.Close()

	items := []domain.ExternalPrincipalAccess{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan external principal access id: %w", err)
		}
		item, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external principal access: %w", err)
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

	if _, err := tx.Exec(ctx, `
		UPDATE external_principal_access
		SET access_mode = $2,
		    allowed_capabilities = $3,
		    expires_at = $4,
		    revoked_at = $5
		WHERE id = $1
	`, id, string(existing.AccessMode), capsJSON, existing.ExpiresAt, existing.RevokedAt); err != nil {
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
	_, err := r.db.Exec(ctx, `UPDATE external_principal_access SET revoked_at = $2 WHERE id = $1`, id, revokedAt)
	if err != nil {
		return fmt.Errorf("revoke external principal access: %w", err)
	}
	return nil
}

func (r *ExternalPrincipalAccessRepo) HasConversationAccess(ctx context.Context, accessID, conversationID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM external_principal_conversation_assignments
			WHERE access_id = $1 AND conversation_id = $2
		)
	`, accessID, conversationID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check external principal conversation access: %w", err)
	}
	return exists, nil
}

func (r *ExternalPrincipalAccessRepo) ListConversationIDs(ctx context.Context, accessID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT conversation_id
		FROM external_principal_conversation_assignments
		WHERE access_id = $1
		ORDER BY conversation_id ASC
	`, accessID)
	if err != nil {
		return nil, fmt.Errorf("list external principal conversation assignments: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan external principal conversation assignment: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external principal conversation assignments: %w", err)
	}
	return ids, nil
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
	if _, err := db.Exec(ctx, `DELETE FROM external_principal_conversation_assignments WHERE access_id = $1`, accessID); err != nil {
		return fmt.Errorf("delete external principal conversation assignments: %w", err)
	}
	for _, conversationID := range conversationIDs {
		if _, err := db.Exec(ctx, `
			INSERT INTO external_principal_conversation_assignments (access_id, conversation_id, granted_by)
			VALUES ($1, $2, $3)
		`, accessID, conversationID, grantedBy); err != nil {
			return fmt.Errorf("insert external principal conversation assignment: %w", err)
		}
	}
	return nil
}
