package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// UserService contains business logic for user operations.
type UserService struct {
	repo      repository.UserRepository
	publisher EventPublisher
	logger    *slog.Logger
}

// NewUserService creates a new UserService.
func NewUserService(repo repository.UserRepository, publisher EventPublisher, logger *slog.Logger) *UserService {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &UserService{repo: repo, publisher: publisher, logger: logger}
}

func (s *UserService) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	user, err := s.repo.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, params.TeamID, domain.EventTypeMessage, user); pubErr != nil {
		s.logger.Warn("publish user.created event", "error", pubErr)
	}
	return user, nil
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
	user, err := s.repo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, user.TeamID, domain.EventUserUpdated, user); pubErr != nil {
		s.logger.Warn("publish user.updated event", "error", pubErr)
	}
	return user, nil
}

func (s *UserService) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}
