package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	tokenStr := prefix + hex.EncodeToString(tokenBytes)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateToken(ctx, sqlcgen.CreateTokenParams{
		ID:     id,
		TeamID: params.TeamID,
		UserID: params.UserID,
		Token:  tokenStr,
		Scopes: params.Scopes,
		IsBot:  params.IsBot,
	})
	if err != nil {
		return nil, fmt.Errorf("insert token: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"token_id": id, "team_id": params.TeamID, "user_id": params.UserID})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateToken,
		AggregateID:   id,
		EventType:     domain.EventTokenCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return tokenToDomain(row), nil
}

func (r *AuthRepo) GetByToken(ctx context.Context, token string) (*domain.Token, error) {
	row, err := r.q.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, fmt.Errorf("get token: %w", err)
	}
	return tokenToDomain(row), nil
}

func (r *AuthRepo) RevokeToken(ctx context.Context, token string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.RevokeToken(ctx, token); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"token": token})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateToken,
		AggregateID:   token,
		EventType:     domain.EventTokenRevoked,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
