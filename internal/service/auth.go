package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// AuthService contains business logic for authentication operations.
type AuthService struct {
	repo     repository.AuthRepository
	userRepo repository.UserRepository
}

// NewAuthService creates a new AuthService.
func NewAuthService(repo repository.AuthRepository, userRepo repository.UserRepository) *AuthService {
	return &AuthService{repo: repo, userRepo: userRepo}
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

	return s.repo.CreateToken(ctx, params)
}

func (s *AuthService) ValidateToken(ctx context.Context, bearerToken string) (*domain.AuthTestResponse, error) {
	token := strings.TrimPrefix(bearerToken, "Bearer ")
	token = strings.TrimSpace(token)

	if token == "" {
		return nil, fmt.Errorf("token: %w", domain.ErrInvalidArgument)
	}

	t, err := s.repo.GetByToken(ctx, token)
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
	return s.repo.RevokeToken(ctx, token)
}
