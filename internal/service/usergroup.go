package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// UsergroupService contains business logic for usergroup operations.
type UsergroupService struct {
	repo     repository.UsergroupRepository
	userRepo repository.UserRepository
	recorder EventRecorder
	logger   *slog.Logger
}

// NewUsergroupService creates a new UsergroupService.
func NewUsergroupService(repo repository.UsergroupRepository, userRepo repository.UserRepository, recorder EventRecorder, logger *slog.Logger) *UsergroupService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &UsergroupService{repo: repo, userRepo: userRepo, recorder: recorder, logger: logger}
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
	payload, _ := json.Marshal(ug)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventUsergroupCreated,
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   ug.ID,
		TeamID:        ug.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record usergroup.created event", "error", recErr)
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
	payload, _ := json.Marshal(ug)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventUsergroupUpdated,
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   ug.ID,
		TeamID:        ug.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record usergroup.updated event", "error", recErr)
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
		payload, _ := json.Marshal(ug)
		if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
			EventType:     domain.EventUsergroupEnabled,
			AggregateType: domain.AggregateUsergroup,
			AggregateID:   id,
			TeamID:        ug.TeamID,
			Payload:       payload,
		}); recErr != nil {
			s.logger.Warn("record usergroup.enabled event", "error", recErr)
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
		payload, _ := json.Marshal(ug)
		if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
			EventType:     domain.EventUsergroupDisabled,
			AggregateType: domain.AggregateUsergroup,
			AggregateID:   id,
			TeamID:        ug.TeamID,
			Payload:       payload,
		}); recErr != nil {
			s.logger.Warn("record usergroup.disabled event", "error", recErr)
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
		payload, _ := json.Marshal(ug)
		if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
			EventType:     domain.EventUsergroupUserSet,
			AggregateType: domain.AggregateUsergroup,
			AggregateID:   usergroupID,
			TeamID:        ug.TeamID,
			Payload:       payload,
		}); recErr != nil {
			s.logger.Warn("record usergroup.users_set event", "error", recErr)
		}
	}
	return nil
}
