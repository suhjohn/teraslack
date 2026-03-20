package service

import (
	"context"
	"fmt"

	"github.com/suhjohn/agentslack/internal/domain"
	"github.com/suhjohn/agentslack/internal/repository"
)

// UserService contains business logic for user operations.
type UserService struct {
	repo repository.UserRepository
}

// NewUserService creates a new UserService.
func NewUserService(repo repository.UserRepository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Create(ctx, params)
}

func (s *UserService) Get(ctx context.Context, id string) (*domain.User, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Get(ctx, id)
}

func (s *UserService) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if email == "" {
		return nil, fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}
	return s.repo.GetByEmail(ctx, email)
}

func (s *UserService) Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Update(ctx, id, params)
}

func (s *UserService) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}
