package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// UserService contains business logic for user operations.
type UserService struct {
	repo     repository.UserRepository
	recorder EventRecorder
	logger   *slog.Logger
}

// NewUserService creates a new UserService.
func NewUserService(repo repository.UserRepository, recorder EventRecorder, logger *slog.Logger) *UserService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &UserService{repo: repo, recorder: recorder, logger: logger}
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
	payload, _ := json.Marshal(user)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record user.created event", "error", recErr)
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
	payload, _ := json.Marshal(user)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventUserUpdated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record user.updated event", "error", recErr)
	}
	return user, nil
}

func (s *UserService) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}
