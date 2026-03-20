package service

import (
	"context"
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
	repo      repository.AuthRepository
	userRepo  repository.UserRepository
	publisher EventPublisher
	logger    *slog.Logger
}

// NewAuthService creates a new AuthService.
func NewAuthService(repo repository.AuthRepository, userRepo repository.UserRepository, publisher EventPublisher, logger *slog.Logger) *AuthService {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &AuthService{repo: repo, userRepo: userRepo, publisher: publisher, logger: logger}
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
	if pubErr := s.publisher.Publish(ctx, params.TeamID, domain.EventTokenCreated, token); pubErr != nil {
		s.logger.Warn("publish token.created event", "error", pubErr)
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
	if err := s.repo.RevokeToken(ctx, token); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventTokenRevoked, map[string]string{"token": token}); pubErr != nil {
		s.logger.Warn("publish token.revoked event", "error", pubErr)
	}
	return nil
}
