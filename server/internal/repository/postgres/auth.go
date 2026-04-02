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
		ID:           id,
		WorkspaceID:  params.WorkspaceID,
		AccountID:    stringToText(params.AccountID),
		MembershipID: stringToText(params.MembershipID),
		UserID:       stringToText(params.UserID),
		SessionHash:  crypto.HashToken(raw),
		Provider:     string(params.Provider),
		ExpiresAt:    params.ExpiresAt,
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

func (r *AuthRepo) DeletePendingEmailVerificationChallenges(ctx context.Context, email string) error {
	return r.q.DeletePendingEmailVerificationChallenges(ctx, email)
}

func (r *AuthRepo) CreateEmailVerificationChallenge(ctx context.Context, params domain.CreateEmailVerificationChallengeParams) (*domain.EmailVerificationChallenge, error) {
	row, err := r.q.CreateEmailVerificationChallenge(ctx, sqlcgen.CreateEmailVerificationChallengeParams{
		ID:        generateID("EV"),
		Email:     params.Email,
		CodeHash:  params.CodeHash,
		ExpiresAt: params.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("create email verification challenge: %w", err)
	}
	return emailVerificationChallengeToDomain(row), nil
}

func (r *AuthRepo) GetEmailVerificationChallenge(ctx context.Context, email, codeHash string) (*domain.EmailVerificationChallenge, error) {
	row, err := r.q.GetEmailVerificationChallenge(ctx, sqlcgen.GetEmailVerificationChallengeParams{
		Lower:    email,
		CodeHash: codeHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, fmt.Errorf("get email verification challenge: %w", err)
	}
	return emailVerificationChallengeToDomain(row), nil
}

func (r *AuthRepo) ConsumeEmailVerificationChallenge(ctx context.Context, id string, consumedAt time.Time) error {
	rows, err := r.q.ConsumeEmailVerificationChallenge(ctx, sqlcgen.ConsumeEmailVerificationChallengeParams{
		ID:         id,
		ConsumedAt: &consumedAt,
	})
	if err != nil {
		return fmt.Errorf("consume email verification challenge: %w", err)
	}
	if rows == 0 {
		return domain.ErrInvalidAuth
	}
	return nil
}

func (r *AuthRepo) GetOAuthAccount(ctx context.Context, workspaceID string, provider domain.AuthProvider, providerSubject string) (*domain.OAuthAccount, error) {
	row, err := r.q.GetOAuthAccount(ctx, sqlcgen.GetOAuthAccountParams{
		WorkspaceID:     workspaceID,
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
	rows, err := r.q.ListOAuthAccountsBySubject(ctx, sqlcgen.ListOAuthAccountsBySubjectParams{
		Provider:        string(provider),
		ProviderSubject: providerSubject,
	})
	if err != nil {
		return nil, fmt.Errorf("list oauth accounts by subject: %w", err)
	}

	accounts := make([]domain.OAuthAccount, 0)
	for _, row := range rows {
		accounts = append(accounts, *oauthAccountToDomain(row))
	}
	return accounts, nil
}

func (r *AuthRepo) UpsertOAuthAccount(ctx context.Context, params domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
	row, err := r.q.UpsertOAuthAccount(ctx, sqlcgen.UpsertOAuthAccountParams{
		ID:              generateID("OA"),
		WorkspaceID:     params.WorkspaceID,
		AccountID:       stringToText(params.AccountID),
		MembershipID:    stringToText(params.MembershipID),
		UserID:          stringToText(params.UserID),
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

func emailVerificationChallengeToDomain(row sqlcgen.EmailVerificationChallenge) *domain.EmailVerificationChallenge {
	return &domain.EmailVerificationChallenge{
		ID:         row.ID,
		Email:      row.Email,
		CodeHash:   row.CodeHash,
		ExpiresAt:  row.ExpiresAt,
		ConsumedAt: row.ConsumedAt,
		CreatedAt:  row.CreatedAt,
	}
}
