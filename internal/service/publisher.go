package service

import "context"

// EventPublisher defines the interface for publishing events to webhook subscribers.
// This is extracted as an interface to allow services to publish events without
// circular dependencies on EventService.
type EventPublisher interface {
	Publish(ctx context.Context, teamID, eventType string, payload any) error
}

// noopPublisher is a no-op implementation used when no event publisher is configured.
type noopPublisher struct{}

func (noopPublisher) Publish(ctx context.Context, teamID, eventType string, payload any) error {
	return nil
}
