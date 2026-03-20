package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// UsergroupService contains business logic for usergroup operations.
type UsergroupService struct {
	repo      repository.UsergroupRepository
	userRepo  repository.UserRepository
	publisher EventPublisher
	logger    *slog.Logger
}

// NewUsergroupService creates a new UsergroupService.
func NewUsergroupService(repo repository.UsergroupRepository, userRepo repository.UserRepository, publisher EventPublisher, logger *slog.Logger) *UsergroupService {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &UsergroupService{repo: repo, userRepo: userRepo, publisher: publisher, logger: logger}
}

func (s *UsergroupService) Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	if params.Handle == "" {
		return nil, fmt.Errorf("handle: %w", domain.ErrInvalidArgument)
	}
	if params.CreatedBy == "" {
		return nil, fmt.Errorf("created_by: %w", domain.ErrInvalidArgument)
	}
	ug, err := s.repo.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, params.TeamID, domain.EventUsergroupCreated, ug); pubErr != nil {
		s.logger.Warn("publish usergroup.created event", "error", pubErr)
	}
	return ug, nil
}

func (s *UsergroupService) Get(ctx context.Context, id string) (*domain.Usergroup, error) {
	if id == "" {
		return nil, fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Get(ctx, id)
}

func (s *UsergroupService) Update(ctx context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error) {
	if id == "" {
		return nil, fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	ug, err := s.repo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, ug.TeamID, domain.EventUsergroupUpdated, ug); pubErr != nil {
		s.logger.Warn("publish usergroup.updated event", "error", pubErr)
	}
	return ug, nil
}

func (s *UsergroupService) List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}

func (s *UsergroupService) Enable(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	if err := s.repo.Enable(ctx, id); err != nil {
		return err
	}
	ug, _ := s.repo.Get(ctx, id)
	if ug != nil {
		if pubErr := s.publisher.Publish(ctx, ug.TeamID, domain.EventUsergroupEnabled, ug); pubErr != nil {
			s.logger.Warn("publish usergroup.enabled event", "error", pubErr)
		}
	}
	return nil
}

func (s *UsergroupService) Disable(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	if err := s.repo.Disable(ctx, id); err != nil {
		return err
	}
	ug, _ := s.repo.Get(ctx, id)
	if ug != nil {
		if pubErr := s.publisher.Publish(ctx, ug.TeamID, domain.EventUsergroupDisabled, ug); pubErr != nil {
			s.logger.Warn("publish usergroup.disabled event", "error", pubErr)
		}
	}
	return nil
}

func (s *UsergroupService) ListUsers(ctx context.Context, usergroupID string) ([]string, error) {
	if usergroupID == "" {
		return nil, fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListUsers(ctx, usergroupID)
}

func (s *UsergroupService) SetUsers(ctx context.Context, usergroupID string, userIDs []string) error {
	if usergroupID == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	// Verify usergroup exists
	if _, err := s.repo.Get(ctx, usergroupID); err != nil {
		return err
	}
	if err := s.repo.SetUsers(ctx, usergroupID, userIDs); err != nil {
		return err
	}
	ug, _ := s.repo.Get(ctx, usergroupID)
	if ug != nil {
		if pubErr := s.publisher.Publish(ctx, ug.TeamID, domain.EventUsergroupUserSet, ug); pubErr != nil {
			s.logger.Warn("publish usergroup.users_set event", "error", pubErr)
		}
	}
	return nil
}
