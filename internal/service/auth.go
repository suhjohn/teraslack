package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/suhjohn/workspace/internal/crypto"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// AuthService contains business logic for authentication operations.
type AuthService struct {
	repo     repository.AuthRepository
	userRepo repository.UserRepository
	recorder EventRecorder
	db       repository.TxBeginner
	logger   *slog.Logger
}

// NewAuthService creates a new AuthService.
func NewAuthService(repo repository.AuthRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *AuthService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &AuthService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

func (s *AuthService) CreateToken(ctx context.Context, params domain.CreateTokenParams) (*domain.Token, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.UserID == "" {
		return nil, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}

	// Verify user exists
	if _, err := s.userRepo.Get(ctx, params.UserID); err != nil {
		return nil, fmt.Errorf("user: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	token, err := s.repo.WithTx(tx).CreateToken(ctx, params)
	if err != nil {
		return nil, err
	}
	// Redact: omit raw Token field, include only token_id, team_id, user_id, is_bot, scopes
	payload, _ := json.Marshal(token.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventTokenCreated,
		AggregateType: domain.AggregateToken,
		AggregateID:   token.ID,
		TeamID:        token.TeamID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record token.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return token, nil
}

func (s *AuthService) ValidateToken(ctx context.Context, bearerToken string) (*domain.AuthTestResponse, error) {
	token := strings.TrimPrefix(bearerToken, "Bearer ")
	token = strings.TrimSpace(token)

	if token == "" {
		return nil, fmt.Errorf("token: %w", domain.ErrInvalidArgument)
	}

	// Hash the raw token and look up by hash.
	tokenHash := crypto.HashToken(token)
	t, err := s.repo.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}

	if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
		return nil, domain.ErrTokenRevoked
	}

	return &domain.AuthTestResponse{
		TeamID: t.TeamID,
		UserID: t.UserID,
		IsBot:  t.IsBot,
	}, nil
}

func (s *AuthService) RevokeToken(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("token: %w", domain.ErrInvalidArgument)
	}
	// Hash the token to get the token_id for the event before revoking
	tokenHash := crypto.HashToken(token)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).RevokeToken(ctx, token); err != nil {
		return err
	}
	// Redact: omit raw token value, include only token_hash
	payload, _ := json.Marshal(map[string]string{"token_hash": tokenHash})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventTokenRevoked,
		AggregateType: domain.AggregateToken,
		AggregateID:   tokenHash,
		TeamID:        "",
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record token.revoked event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
