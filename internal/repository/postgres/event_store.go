package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

// EventStoreRepo implements repository.EventStoreRepository using Postgres.
type EventStoreRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

// NewEventStoreRepo creates a new EventStoreRepo.
func NewEventStoreRepo(pool *pgxpool.Pool) *EventStoreRepo {
	return &EventStoreRepo{pool: pool, q: sqlcgen.New(pool)}
}

// Append writes a service event and creates outbox entries for matching subscriptions atomically.
func (r *EventStoreRepo) Append(ctx context.Context, event domain.ServiceEvent) (*domain.ServiceEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)

	// Insert the service event
	row, err := qtx.InsertServiceEvent(ctx, sqlcgen.InsertServiceEventParams{
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

	// Find matching subscriptions and create outbox entries
	subs, err := qtx.GetMatchingSubscriptions(ctx, sqlcgen.GetMatchingSubscriptionsParams{
		TeamID:  event.TeamID,
		Column2: event.EventType,
	})
	if err != nil {
		return nil, fmt.Errorf("get matching subscriptions: %w", err)
	}

	for _, sub := range subs {
		secret := sub.Secret
		if secret == "" {
			secret = sub.EncryptedSecret
		}
		if err := qtx.InsertOutboxEntry(ctx, sqlcgen.InsertOutboxEntryParams{
			EventID:        row.ID,
			SubscriptionID: sub.ID,
			Url:            sub.Url,
			Payload:        event.Payload,
			Secret:         secret,
			MaxAttempts:     5,
		}); err != nil {
			return nil, fmt.Errorf("insert outbox entry for subscription %s: %w", sub.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
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
