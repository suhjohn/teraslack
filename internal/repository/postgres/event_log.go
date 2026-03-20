package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

// EventLogRepo implements repository.EventLogRepository using sqlc.
type EventLogRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

// NewEventLogRepo creates a new EventLogRepo.
func NewEventLogRepo(pool *pgxpool.Pool) *EventLogRepo {
	return &EventLogRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *EventLogRepo) Append(ctx context.Context, entry domain.EventLogEntry) (*domain.EventLogEntry, error) {
	eventData, err := json.Marshal(entry.EventData)
	if err != nil {
		return nil, fmt.Errorf("marshal event data: %w", err)
	}
	var metadata []byte
	if entry.Metadata != nil {
		metadata, err = json.Marshal(entry.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
	}

	row, err := r.q.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: entry.AggregateType,
		AggregateID:   entry.AggregateID,
		EventType:     entry.EventType,
		EventData:     eventData,
		Metadata:      metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}
	return eventLogToDomain(row), nil
}

func (r *EventLogRepo) GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.EventLogEntry, error) {
	rows, err := r.q.GetEventsByAggregate(ctx, sqlcgen.GetEventsByAggregateParams{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
	})
	if err != nil {
		return nil, fmt.Errorf("get events by aggregate: %w", err)
	}
	entries := make([]domain.EventLogEntry, len(rows))
	for i, row := range rows {
		entries[i] = *eventLogToDomain(row)
	}
	return entries, nil
}

func (r *EventLogRepo) GetByAggregateSince(ctx context.Context, aggregateType, aggregateID string, sinceSequenceID int64) ([]domain.EventLogEntry, error) {
	rows, err := r.q.GetEventsByAggregateSince(ctx, sqlcgen.GetEventsByAggregateSinceParams{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		SequenceID:    sinceSequenceID,
	})
	if err != nil {
		return nil, fmt.Errorf("get events by aggregate since: %w", err)
	}
	entries := make([]domain.EventLogEntry, len(rows))
	for i, row := range rows {
		entries[i] = *eventLogToDomain(row)
	}
	return entries, nil
}

func (r *EventLogRepo) GetByType(ctx context.Context, eventType string, limit int) ([]domain.EventLogEntry, error) {
	rows, err := r.q.GetEventsByType(ctx, sqlcgen.GetEventsByTypeParams{
		EventType: eventType,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get events by type: %w", err)
	}
	entries := make([]domain.EventLogEntry, len(rows))
	for i, row := range rows {
		entries[i] = *eventLogToDomain(row)
	}
	return entries, nil
}

func (r *EventLogRepo) GetAllSince(ctx context.Context, sinceSequenceID int64, limit int) ([]domain.EventLogEntry, error) {
	rows, err := r.q.GetAllEventsSince(ctx, sqlcgen.GetAllEventsSinceParams{
		SequenceID: sinceSequenceID,
		Limit:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get all events since: %w", err)
	}
	entries := make([]domain.EventLogEntry, len(rows))
	for i, row := range rows {
		entries[i] = *eventLogToDomain(row)
	}
	return entries, nil
}
