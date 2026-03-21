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
	db       repository.TxBeginner
	logger   *slog.Logger
}

// NewUserService creates a new UserService.
func NewUserService(repo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *UserService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &UserService{repo: repo, recorder: recorder, db: db, logger: logger}
}

func (s *UserService) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	user, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	user, err := s.repo.WithTx(tx).Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventUserUpdated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return user, nil
}

func (s *UserService) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}
