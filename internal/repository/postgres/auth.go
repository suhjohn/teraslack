package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/crypto"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type AuthRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAuthRepo(pool *pgxpool.Pool) *AuthRepo {
	return &AuthRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *AuthRepo) CreateToken(ctx context.Context, params domain.CreateTokenParams) (*domain.Token, error) {
	id := generateID("TK")

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	prefix := "xoxb-"
	if !params.IsBot {
		prefix = "xoxp-"
	}
	rawToken := prefix + hex.EncodeToString(tokenBytes)
	tokenHash := crypto.HashToken(rawToken)

	row, err := r.q.CreateToken(ctx, sqlcgen.CreateTokenParams{
		ID:        id,
		TeamID:    params.TeamID,
		UserID:    params.UserID,
		Token:     rawToken,
		TokenHash: tokenHash,
		Scopes:    params.Scopes,
		IsBot:     params.IsBot,
	})
	if err != nil {
		return nil, fmt.Errorf("insert token: %w", err)
	}

	// Return the token with the raw value so the caller can give it to the user.
	// This is the ONLY time the raw token is available.
	return createTokenRowToDomain(row), nil
}

func (r *AuthRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.Token, error) {
	row, err := r.q.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, fmt.Errorf("get token by hash: %w", err)
	}
	return tokenHashRowToDomain(row), nil
}

func (r *AuthRepo) RevokeToken(ctx context.Context, token string) error {
	tokenHash := crypto.HashToken(token)
	return r.q.RevokeTokenByHash(ctx, tokenHash)
}
