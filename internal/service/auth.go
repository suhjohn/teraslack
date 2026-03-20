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
	logger   *slog.Logger
}

// NewAuthService creates a new AuthService.
func NewAuthService(repo repository.AuthRepository, userRepo repository.UserRepository, recorder EventRecorder, logger *slog.Logger) *AuthService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &AuthService{repo: repo, userRepo: userRepo, recorder: recorder, logger: logger}
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

	token, err := s.repo.CreateToken(ctx, params)
	if err != nil {
		return nil, err
	}
	// Redact: omit raw Token field, include only token_id, team_id, user_id, is_bot, scopes
	payload, _ := json.Marshal(token.Redacted())
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventTokenCreated,
		AggregateType: domain.AggregateToken,
		AggregateID:   token.ID,
		TeamID:        token.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record token.created event", "error", recErr)
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
	if err := s.repo.RevokeToken(ctx, token); err != nil {
		return err
	}
	// Redact: omit raw token value, include only token_hash
	payload, _ := json.Marshal(map[string]string{"token_hash": tokenHash})
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventTokenRevoked,
		AggregateType: domain.AggregateToken,
		AggregateID:   tokenHash,
		TeamID:        "",
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record token.revoked event", "error", recErr)
	}
	return nil
}
