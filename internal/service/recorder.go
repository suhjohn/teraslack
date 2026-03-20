package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/suhjohn/workspace/internal/ctxutil"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// EventRecorder defines the interface for recording service-level events.
// This replaces the old EventPublisher interface. Services call Record()
// after successful mutations to append events to the event store with
// actor identity and explicit payload control.
type EventRecorder interface {
	Record(ctx context.Context, event domain.ServiceEvent) error
}

// eventRecorder is the production implementation backed by EventStoreRepository.
type eventRecorder struct {
	store repository.EventStoreRepository
}

// NewEventRecorder creates a new EventRecorder backed by the given EventStoreRepository.
func NewEventRecorder(store repository.EventStoreRepository) EventRecorder {
	return &eventRecorder{store: store}
}

// Record appends a service event to the event store.
// It extracts the actor_id from the request context if not already set.
func (r *eventRecorder) Record(ctx context.Context, event domain.ServiceEvent) error {
	// Extract actor_id from auth context if not explicitly set
	if event.ActorID == "" {
		event.ActorID = ctxutil.GetUserID(ctx)
	}

	// Ensure payload is valid JSON
	if event.Payload == nil {
		event.Payload = json.RawMessage("{}")
	}

	_, err := r.store.Append(ctx, event)
	if err != nil {
		return fmt.Errorf("record event %s: %w", event.EventType, err)
	}
	return nil
}

// noopRecorder is a no-op implementation used when no event recorder is configured.
type noopRecorder struct{}

func (noopRecorder) Record(ctx context.Context, event domain.ServiceEvent) error {
	return nil
}
