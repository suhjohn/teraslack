package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

// EventStoreRepo implements repository.EventStoreRepository using Postgres.
type EventStoreRepo struct {
	db DBTX
	q  *sqlcgen.Queries
}

// NewEventStoreRepo creates a new EventStoreRepo.
func NewEventStoreRepo(db DBTX) *EventStoreRepo {
	return &EventStoreRepo{db: db, q: sqlcgen.New(db)}
}

// WithTx returns a new EventStoreRepo that operates within the given transaction.
func (r *EventStoreRepo) WithTx(tx pgx.Tx) repository.EventStoreRepository {
	return &EventStoreRepo{db: tx, q: sqlcgen.New(tx)}
}

// Append writes a service event to the event store.
// This is a pure INSERT — webhook fan-out is handled by the WebhookProducer process
// which tails service_events independently via S3 queue.
func (r *EventStoreRepo) Append(ctx context.Context, event domain.ServiceEvent) (*domain.ServiceEvent, error) {
	row, err := r.q.InsertServiceEvent(ctx, sqlcgen.InsertServiceEventParams{
		EventType:     event.EventType,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		TeamID:        event.TeamID,
		ActorID:       event.ActorID,
		Payload:       event.Payload,
		Metadata:      event.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("insert service event: %w", err)
	}

	result := serviceEventToDomain(row)
	return &result, nil
}

// GetByAggregate returns all events for an aggregate ordered by ID.
func (r *EventStoreRepo) GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.ServiceEvent, error) {
	rows, err := r.q.GetServiceEventsByAggregate(ctx, sqlcgen.GetServiceEventsByAggregateParams{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
	})
	if err != nil {
		return nil, fmt.Errorf("get events by aggregate: %w", err)
	}
	events := make([]domain.ServiceEvent, len(rows))
	for i, row := range rows {
		events[i] = serviceEventToDomain(row)
	}
	return events, nil
}

// GetAllSince returns events since a given ID for incremental projection rebuilds.
func (r *EventStoreRepo) GetAllSince(ctx context.Context, sinceID int64, limit int) ([]domain.ServiceEvent, error) {
	rows, err := r.q.GetServiceEventsSince(ctx, sqlcgen.GetServiceEventsSinceParams{
		ID:    sinceID,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get events since: %w", err)
	}
	events := make([]domain.ServiceEvent, len(rows))
	for i, row := range rows {
		events[i] = serviceEventToDomain(row)
	}
	return events, nil
}

func serviceEventToDomain(e sqlcgen.ServiceEvent) domain.ServiceEvent {
	return domain.ServiceEvent{
		ID:            e.ID,
		EventType:     e.EventType,
		AggregateType: e.AggregateType,
		AggregateID:   e.AggregateID,
		TeamID:        e.TeamID,
		ActorID:       e.ActorID,
		Payload:       e.Payload,
		Metadata:      e.Metadata,
		CreatedAt:     tsToTime(e.CreatedAt),
	}
}
