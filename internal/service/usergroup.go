package service

import (
	"context"
	"fmt"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// UsergroupService contains business logic for usergroup operations.
type UsergroupService struct {
	repo     repository.UsergroupRepository
	userRepo repository.UserRepository
}

// NewUsergroupService creates a new UsergroupService.
func NewUsergroupService(repo repository.UsergroupRepository, userRepo repository.UserRepository) *UsergroupService {
	return &UsergroupService{repo: repo, userRepo: userRepo}
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
	return s.repo.Create(ctx, params)
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
	return s.repo.Update(ctx, id, params)
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
	return s.repo.Enable(ctx, id)
}

func (s *UsergroupService) Disable(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("usergroup: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Disable(ctx, id)
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
	return s.repo.SetUsers(ctx, usergroupID, userIDs)
}
