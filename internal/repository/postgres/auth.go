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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateToken(ctx, sqlcgen.CreateTokenParams{
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

	token := createTokenRowToDomain(row)

	// Redact sensitive fields before writing to event log.
	// The raw token is NEVER stored in event_data.
	redacted := token.Redacted()
	eventData, _ := json.Marshal(redacted)
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

	// Return the token with the raw value so the caller can give it to the user.
	// This is the ONLY time the raw token is available.
	return token, nil
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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	// FIX: Fetch the token BEFORE deletion so we can snapshot it in the event log.
	// Previously, the code deleted first and then tried to fetch — which always failed.
	tRow, tErr := qtx.GetByTokenHash(ctx, tokenHash)
	if tErr != nil {
		if errors.Is(tErr, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("get token before revoke: %w", tErr)
	}

	// Now delete the token.
	if err := qtx.RevokeTokenByHash(ctx, tokenHash); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	// Append revoke event with redacted snapshot (no raw token in event_data).
	revokedToken := tokenHashRowToDomain(tRow)
	redacted := revokedToken.Redacted()
	eventData, _ := json.Marshal(redacted)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateToken,
		AggregateID:   revokedToken.ID,
		EventType:     domain.EventTokenRevoked,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
