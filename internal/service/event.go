package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// EventService contains business logic for event subscription operations.
// Webhook dispatch is now handled by OutboxWorker, not by this service.
type EventService struct {
	repo     repository.EventRepository
	recorder EventRecorder
	logger   *slog.Logger
}

// NewEventService creates a new EventService.
func NewEventService(repo repository.EventRepository, recorder EventRecorder, logger *slog.Logger) *EventService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &EventService{
		repo:     repo,
		recorder: recorder,
		logger:   logger,
	}
}

func (s *EventService) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.URL == "" {
		return nil, fmt.Errorf("url: %w", domain.ErrInvalidArgument)
	}
	if len(params.EventTypes) == 0 {
		return nil, fmt.Errorf("event_types: %w", domain.ErrInvalidArgument)
	}
	sub, err := s.repo.CreateSubscription(ctx, params)
	if err != nil {
		return nil, err
	}
	// Redact: omit Secret field
	payload, _ := json.Marshal(sub.Redacted())
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventSubscriptionCreated,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   sub.ID,
		TeamID:        sub.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record subscription.created event", "error", recErr)
	}
	return sub, nil
}

func (s *EventService) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.GetSubscription(ctx, id)
}

func (s *EventService) UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	sub, err := s.repo.UpdateSubscription(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(sub.Redacted())
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventSubscriptionUpdated,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   sub.ID,
		TeamID:        sub.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record subscription.updated event", "error", recErr)
	}
	return sub, nil
}

func (s *EventService) DeleteSubscription(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	// Get subscription before deleting to capture team_id for event
	sub, _ := s.repo.GetSubscription(ctx, id)
	if err := s.repo.DeleteSubscription(ctx, id); err != nil {
		return err
	}
	teamID := ""
	if sub != nil {
		teamID = sub.TeamID
	}
	payload, _ := json.Marshal(map[string]string{"subscription_id": id})
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventSubscriptionDeleted,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		TeamID:        teamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record subscription.deleted event", "error", recErr)
	}
	return nil
}

func (s *EventService) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListSubscriptions(ctx, params)
}
