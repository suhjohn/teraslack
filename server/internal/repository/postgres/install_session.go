package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type InstallSessionRepo struct {
	db DBTX
}

func NewInstallSessionRepo(db DBTX) *InstallSessionRepo {
	return &InstallSessionRepo{db: db}
}

func (r *InstallSessionRepo) WithTx(tx pgx.Tx) repository.InstallSessionRepository {
	return &InstallSessionRepo{db: tx}
}

func (r *InstallSessionRepo) Create(ctx context.Context, params domain.CreateInstallSessionParams) (*domain.InstallSession, string, error) {
	id := generateID("IS")
	rawPollToken, err := randomInstallPollToken()
	if err != nil {
		return nil, "", err
	}

	row := r.db.QueryRow(ctx, `
		INSERT INTO install_sessions (
			id,
			poll_token_hash,
			status,
			device_name,
			client_kind,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			poll_token_hash,
			status,
			workspace_id,
			approved_by_user_id,
			credential_id,
			raw_credential_encrypted,
			device_name,
			client_kind,
			expires_at,
			approved_at,
			consumed_at,
			created_at,
			updated_at
	`, id, crypto.HashToken(rawPollToken), string(domain.InstallSessionStatusPending), params.DeviceName, params.ClientKind, params.ExpiresAt.UTC())

	session, err := scanInstallSession(row)
	if err != nil {
		return nil, "", err
	}
	return session, rawPollToken, nil
}

func (r *InstallSessionRepo) Get(ctx context.Context, id string) (*domain.InstallSession, error) {
	row := r.db.QueryRow(ctx, `
		SELECT
			id,
			poll_token_hash,
			status,
			workspace_id,
			approved_by_user_id,
			credential_id,
			raw_credential_encrypted,
			device_name,
			client_kind,
			expires_at,
			approved_at,
			consumed_at,
			created_at,
			updated_at
		FROM install_sessions
		WHERE id = $1
	`, id)
	return scanInstallSession(row)
}

func (r *InstallSessionRepo) GetByPollTokenHash(ctx context.Context, id, pollTokenHash string) (*domain.InstallSession, error) {
	row := r.db.QueryRow(ctx, `
		SELECT
			id,
			poll_token_hash,
			status,
			workspace_id,
			approved_by_user_id,
			credential_id,
			raw_credential_encrypted,
			device_name,
			client_kind,
			expires_at,
			approved_at,
			consumed_at,
			created_at,
			updated_at
		FROM install_sessions
		WHERE id = $1 AND poll_token_hash = $2
	`, id, pollTokenHash)
	return scanInstallSession(row)
}

func (r *InstallSessionRepo) Approve(ctx context.Context, params domain.ApproveInstallSessionParams) (*domain.InstallSession, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE install_sessions
		SET
			status = $2,
			workspace_id = $3,
			approved_by_user_id = $4,
			credential_id = $5,
			raw_credential_encrypted = $6,
			approved_at = $7,
			updated_at = now()
		WHERE id = $1
		RETURNING
			id,
			poll_token_hash,
			status,
			workspace_id,
			approved_by_user_id,
			credential_id,
			raw_credential_encrypted,
			device_name,
			client_kind,
			expires_at,
			approved_at,
			consumed_at,
			created_at,
			updated_at
	`, params.ID, string(domain.InstallSessionStatusApproved), params.WorkspaceID, params.ApprovedByUserID, params.CredentialID, params.RawCredentialEncrypted, params.ApprovedAt.UTC())
	return scanInstallSession(row)
}

func (r *InstallSessionRepo) Consume(ctx context.Context, params domain.ConsumeInstallSessionParams) (*domain.InstallSession, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE install_sessions
		SET
			status = $2,
			raw_credential_encrypted = '',
			consumed_at = $3,
			updated_at = now()
		WHERE id = $1
		RETURNING
			id,
			poll_token_hash,
			status,
			workspace_id,
			approved_by_user_id,
			credential_id,
			raw_credential_encrypted,
			device_name,
			client_kind,
			expires_at,
			approved_at,
			consumed_at,
			created_at,
			updated_at
	`, params.ID, string(domain.InstallSessionStatusConsumed), params.ConsumedAt.UTC())
	return scanInstallSession(row)
}

func (r *InstallSessionRepo) ExpirePending(ctx context.Context, now time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE install_sessions
		SET
			status = $2,
			updated_at = now()
		WHERE status = $1 AND expires_at <= $3
	`, string(domain.InstallSessionStatusPending), string(domain.InstallSessionStatusExpired), now.UTC())
	if err != nil {
		return fmt.Errorf("expire install sessions: %w", err)
	}
	return nil
}

func scanInstallSession(row pgx.Row) (*domain.InstallSession, error) {
	var session domain.InstallSession
	var status string
	err := row.Scan(
		&session.ID,
		&session.PollTokenHash,
		&status,
		&session.WorkspaceID,
		&session.ApprovedByUserID,
		&session.CredentialID,
		&session.RawCredentialEncrypted,
		&session.DeviceName,
		&session.ClientKind,
		&session.ExpiresAt,
		&session.ApprovedAt,
		&session.ConsumedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan install session: %w", err)
	}
	session.Status = domain.InstallSessionStatus(status)
	return &session, nil
}

func randomInstallPollToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate install poll token: %w", err)
	}
	return "ispoll_" + hex.EncodeToString(buf), nil
}
