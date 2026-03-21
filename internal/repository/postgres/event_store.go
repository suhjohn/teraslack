package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/workspace/internal/crypto"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

// EventStoreRepo implements repository.EventStoreRepository using Postgres.
type EventStoreRepo struct {
	db        DBTX
	q         *sqlcgen.Queries
	encryptor *crypto.Encryptor
}

// NewEventStoreRepo creates a new EventStoreRepo.
func NewEventStoreRepo(db DBTX, encryptor *crypto.Encryptor) *EventStoreRepo {
	return &EventStoreRepo{db: db, q: sqlcgen.New(db), encryptor: encryptor}
}

// WithTx returns a new EventStoreRepo that operates within the given transaction.
func (r *EventStoreRepo) WithTx(tx pgx.Tx) repository.EventStoreRepository {
	return &EventStoreRepo{db: tx, q: sqlcgen.New(tx), encryptor: r.encryptor}
}

// Append writes a service event and creates outbox entries for matching subscriptions.
// When called via WithTx, it operates within the caller's transaction (no nested tx).
// When called directly (r.db is a pool), it uses r.q directly — the event insert and
// outbox entries share the same implicit connection.
func (r *EventStoreRepo) Append(ctx context.Context, event domain.ServiceEvent) (*domain.ServiceEvent, error) {
	// Insert the service event
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

	// Find matching subscriptions and create outbox entries
	subs, err := r.q.GetMatchingSubscriptions(ctx, sqlcgen.GetMatchingSubscriptionsParams{
		TeamID:  event.TeamID,
		Column2: event.EventType,
	})
	if err != nil {
		return nil, fmt.Errorf("get matching subscriptions: %w", err)
	}

	for _, sub := range subs {
		// Store the encrypted secret in the outbox — the worker decrypts at delivery time.
		// This avoids persisting plaintext secrets in the outbox table.
		outboxSecret := sub.EncryptedSecret
		if outboxSecret == "" {
			outboxSecret = sub.Secret // fallback for unencrypted subscriptions
		}
		if err := r.q.InsertOutboxEntry(ctx, sqlcgen.InsertOutboxEntryParams{
			EventID:        row.ID,
			SubscriptionID: sub.ID,
			Url:            sub.Url,
			Payload:        event.Payload,
			Secret:         outboxSecret,
			MaxAttempts:     5,
		}); err != nil {
			return nil, fmt.Errorf("insert outbox entry for subscription %s: %w", sub.ID, err)
		}
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
