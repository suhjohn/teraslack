package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// EventRecorder defines the interface for recording internal events.
// Services call Record() after successful mutations to append events to
// the event store with actor identity and explicit payload control.
type EventRecorder interface {
	Record(ctx context.Context, event domain.InternalEvent) error
	// WithTx returns an EventRecorder that operates within the given transaction.
	WithTx(tx pgx.Tx) EventRecorder
}

// eventRecorder is the production implementation backed by InternalEventStoreRepository.
type eventRecorder struct {
	store repository.InternalEventStoreRepository
}

// NewEventRecorder creates a new EventRecorder backed by the given InternalEventStoreRepository.
func NewEventRecorder(store repository.InternalEventStoreRepository) EventRecorder {
	return &eventRecorder{store: store}
}

// Record appends an internal event to the event store.
// It extracts the actor_id from the request context if not already set.
func (r *eventRecorder) Record(ctx context.Context, event domain.InternalEvent) error {
	actor := actorFromContext(ctx)
	if event.ActorID == "" {
		event.ActorID = actor.CompatibilityUserID()
	}
	metadata, err := mergeActorMetadata(event.Metadata, actor)
	if err != nil {
		return fmt.Errorf("merge actor metadata: %w", err)
	}
	event.Metadata = metadata

	// Ensure payload is valid JSON
	if event.Payload == nil {
		event.Payload = json.RawMessage("{}")
	}

	_, err = r.store.Append(ctx, event)
	if err != nil {
		return fmt.Errorf("record event %s: %w", event.EventType, err)
	}
	return nil
}

// WithTx returns a new eventRecorder that operates within the given transaction.
func (r *eventRecorder) WithTx(tx pgx.Tx) EventRecorder {
	return &eventRecorder{store: r.store.WithTx(tx)}
}

// noopRecorder is a no-op implementation used when no event recorder is configured.
type noopRecorder struct{}

func (noopRecorder) Record(ctx context.Context, event domain.InternalEvent) error {
	return nil
}

func (noopRecorder) WithTx(tx pgx.Tx) EventRecorder {
	return noopRecorder{}
}
