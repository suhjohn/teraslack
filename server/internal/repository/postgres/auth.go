package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type AuthRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewAuthRepo(db DBTX) *AuthRepo {
	return &AuthRepo{q: sqlcgen.New(db), db: db}
}

func (r *AuthRepo) WithTx(tx pgx.Tx) repository.AuthRepository {
	return &AuthRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *AuthRepo) CreateSession(ctx context.Context, params domain.CreateAuthSessionParams) (*domain.AuthSession, error) {
	id := generateID("AS")

	raw, err := randomSessionToken()
	if err != nil {
		return nil, err
	}

	row, err := r.q.CreateAuthSession(ctx, sqlcgen.CreateAuthSessionParams{
		ID:          id,
		TeamID:      params.TeamID,
		UserID:      params.UserID,
		SessionHash: crypto.HashToken(raw),
		Provider:    string(params.Provider),
		ExpiresAt:   params.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("insert auth session: %w", err)
	}

	session := authSessionToDomain(row)
	session.Token = raw
	return session, nil
}

func (r *AuthRepo) GetSessionByHash(ctx context.Context, sessionHash string) (*domain.AuthSession, error) {
	row, err := r.q.GetAuthSessionByHash(ctx, sessionHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, fmt.Errorf("get auth session by hash: %w", err)
	}
	return authSessionToDomain(row), nil
}

func (r *AuthRepo) RevokeSessionByHash(ctx context.Context, sessionHash string) error {
	return r.q.RevokeAuthSessionByHash(ctx, sessionHash)
}

func (r *AuthRepo) GetOAuthAccount(ctx context.Context, teamID string, provider domain.AuthProvider, providerSubject string) (*domain.OAuthAccount, error) {
	row, err := r.q.GetOAuthAccount(ctx, sqlcgen.GetOAuthAccountParams{
		TeamID:          teamID,
		Provider:        string(provider),
		ProviderSubject: providerSubject,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get oauth account: %w", err)
	}
	return oauthAccountToDomain(row), nil
}

func (r *AuthRepo) ListOAuthAccountsBySubject(ctx context.Context, provider domain.AuthProvider, providerSubject string) ([]domain.OAuthAccount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, team_id, user_id, provider, provider_subject, email, created_at, updated_at
		FROM oauth_accounts
		WHERE provider = $1 AND provider_subject = $2
		ORDER BY created_at ASC, id ASC
	`, string(provider), providerSubject)
	if err != nil {
		return nil, fmt.Errorf("list oauth accounts by subject: %w", err)
	}
	defer rows.Close()

	accounts := make([]domain.OAuthAccount, 0)
	for rows.Next() {
		var account domain.OAuthAccount
		if err := rows.Scan(
			&account.ID,
			&account.TeamID,
			&account.UserID,
			&account.Provider,
			&account.ProviderSubject,
			&account.Email,
			&account.CreatedAt,
			&account.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan oauth account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oauth accounts: %w", err)
	}
	return accounts, nil
}

func (r *AuthRepo) UpsertOAuthAccount(ctx context.Context, params domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
	row, err := r.q.UpsertOAuthAccount(ctx, sqlcgen.UpsertOAuthAccountParams{
		ID:              generateID("OA"),
		TeamID:          params.TeamID,
		UserID:          params.UserID,
		Provider:        string(params.Provider),
		ProviderSubject: params.ProviderSubject,
		Email:           params.Email,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert oauth account: %w", err)
	}
	return oauthAccountToDomain(row), nil
}

func randomSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return "sess_" + hex.EncodeToString(buf), nil
}
