package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// EventService contains business logic for event subscription operations.
// Webhook dispatch is handled by the WebhookProducer/WebhookWorker processes via S3 queue.
type EventService struct {
	repo     repository.EventRepository
	userRepo repository.UserRepository
	recorder EventRecorder
	db       repository.TxBeginner
	logger   *slog.Logger
}

// NewEventService creates a new EventService.
func NewEventService(repo repository.EventRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *EventService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &EventService{
		repo:     repo,
		userRepo: userRepo,
		recorder: recorder,
		db:       db,
		logger:   logger,
	}
}

func (s *EventService) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	if params.URL == "" {
		return nil, fmt.Errorf("url: %w", domain.ErrInvalidArgument)
	}
	if !domain.IsSupportedSubscriptionEventType(params.Type) {
		return nil, fmt.Errorf("type: %w", domain.ErrInvalidArgument)
	}
	if params.ResourceID != "" && params.ResourceType == "" {
		return nil, fmt.Errorf("resource_type: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	sub, err := s.repo.WithTx(tx).CreateSubscription(ctx, params)
	if err != nil {
		return nil, err
	}
	// Redact: omit Secret field
	payload, _ := json.Marshal(sub.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventSubscriptionCreated,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   sub.ID,
		WorkspaceID:        sub.WorkspaceID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record event_subscription.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return sub, nil
}

func (s *EventService) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return nil, err
		}
	}
	sub, err := s.repo.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, sub.WorkspaceID); err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *EventService) UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	if params.Type != nil && !domain.IsSupportedSubscriptionEventType(*params.Type) {
		return nil, fmt.Errorf("type: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return nil, err
		}
	}

	sub, err := s.repo.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, sub.WorkspaceID); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	sub, err = s.repo.WithTx(tx).UpdateSubscription(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(sub.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventSubscriptionUpdated,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   sub.ID,
		WorkspaceID:        sub.WorkspaceID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record event_subscription.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return sub, nil
}

func (s *EventService) DeleteSubscription(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return err
		}
	}
	// Get subscription before deleting to capture workspace_id for event
	sub, err := s.repo.GetSubscription(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureWorkspaceAccess(ctx, sub.WorkspaceID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).DeleteSubscription(ctx, id); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"id": id})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventSubscriptionDeleted,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		WorkspaceID:        sub.WorkspaceID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record event_subscription.deleted event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *EventService) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	if requiresAuthenticatedActor(ctx) {
		if _, err := requireWorkspaceAdminActor(ctx, s.userRepo); err != nil {
			return nil, err
		}
	}
	workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	return s.repo.ListSubscriptions(ctx, params)
}
