package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// AuthRepo implements repository.AuthRepository using Postgres.
type AuthRepo struct {
	pool *pgxpool.Pool
}

// NewAuthRepo creates a new AuthRepo.
func NewAuthRepo(pool *pgxpool.Pool) *AuthRepo {
	return &AuthRepo{pool: pool}
}

func (r *AuthRepo) CreateToken(ctx context.Context, params domain.CreateTokenParams) (*domain.Token, error) {
	id := generateID("TK")
	token := generateBearerToken(params.IsBot)

	var t domain.Token
	err := r.pool.QueryRow(ctx, `
		INSERT INTO tokens (id, team_id, user_id, token, scopes, is_bot)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, team_id, user_id, token, scopes, is_bot, expires_at, created_at`,
		id, params.TeamID, params.UserID, token, params.Scopes, params.IsBot,
	).Scan(
		&t.ID, &t.TeamID, &t.UserID, &t.Token, &t.Scopes, &t.IsBot, &t.ExpiresAt, &t.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert token: %w", err)
	}
	return &t, nil
}

func (r *AuthRepo) GetByToken(ctx context.Context, token string) (*domain.Token, error) {
	var t domain.Token
	err := r.pool.QueryRow(ctx, `
		SELECT id, team_id, user_id, token, scopes, is_bot, expires_at, created_at
		FROM tokens WHERE token = $1`, token,
	).Scan(
		&t.ID, &t.TeamID, &t.UserID, &t.Token, &t.Scopes, &t.IsBot, &t.ExpiresAt, &t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get token: %w", err)
	}
	return &t, nil
}

func (r *AuthRepo) RevokeToken(ctx context.Context, token string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tokens WHERE token = $1`, token)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func generateBearerToken(isBot bool) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	prefix := "xoxp-"
	if isBot {
		prefix = "xoxb-"
	}
	return prefix + hex.EncodeToString(b)
}
