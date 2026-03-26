package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

// EventStoreRepo implements repository.InternalEventStoreRepository using Postgres.
type EventStoreRepo struct {
	db DBTX
	q  *sqlcgen.Queries
}

// NewEventStoreRepo creates a new EventStoreRepo.
func NewEventStoreRepo(db DBTX) *EventStoreRepo {
	return &EventStoreRepo{db: db, q: sqlcgen.New(db)}
}

// WithTx returns a new EventStoreRepo that operates within the given transaction.
func (r *EventStoreRepo) WithTx(tx pgx.Tx) repository.InternalEventStoreRepository {
	return &EventStoreRepo{db: tx, q: sqlcgen.New(tx)}
}

// Append writes an internal event to the event store.
// This is a pure INSERT — webhook fan-out is handled by the WebhookProducer process
// which tails external_events independently via S3 queue.
func (r *EventStoreRepo) Append(ctx context.Context, event domain.InternalEvent) (*domain.InternalEvent, error) {
	shardKey, shardID := deriveInternalEventShard(event)
	row, err := r.q.InsertInternalEvent(ctx, sqlcgen.InsertInternalEventParams{
		EventType:     event.EventType,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		WorkspaceID:        event.WorkspaceID,
		ActorID:       event.ActorID,
		ShardKey:      shardKey,
		ShardID:       int32(shardID),
		Payload:       event.Payload,
		Metadata:      event.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("insert internal event: %w", err)
	}

	result := internalEventToDomain(row)
	return &result, nil
}

// GetByAggregate returns all events for an aggregate ordered by ID.
func (r *EventStoreRepo) GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.InternalEvent, error) {
	rows, err := r.q.GetInternalEventsByAggregate(ctx, sqlcgen.GetInternalEventsByAggregateParams{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
	})
	if err != nil {
		return nil, fmt.Errorf("get events by aggregate: %w", err)
	}
	events := make([]domain.InternalEvent, len(rows))
	for i, row := range rows {
		events[i] = internalEventToDomain(row)
	}
	return events, nil
}

// GetAllSince returns events since a given ID for incremental projection rebuilds.
func (r *EventStoreRepo) GetAllSince(ctx context.Context, sinceID int64, limit int) ([]domain.InternalEvent, error) {
	rows, err := r.q.GetInternalEventsSince(ctx, sqlcgen.GetInternalEventsSinceParams{
		ID:    sinceID,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get events since: %w", err)
	}
	events := make([]domain.InternalEvent, len(rows))
	for i, row := range rows {
		events[i] = internalEventToDomain(row)
	}
	return events, nil
}

func (r *EventStoreRepo) GetAllSinceByShard(ctx context.Context, shardID int, sinceID int64, limit int) ([]domain.InternalEvent, error) {
	rows, err := r.q.GetInternalEventsSinceByShard(ctx, sqlcgen.GetInternalEventsSinceByShardParams{
		ShardID: int32(shardID),
		ID:      sinceID,
		Limit:   int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get events since by shard: %w", err)
	}
	events := make([]domain.InternalEvent, len(rows))
	for i, row := range rows {
		events[i] = internalEventToDomain(row)
	}
	return events, nil
}

func internalEventToDomain(row any) domain.InternalEvent {
	switch e := row.(type) {
	case sqlcgen.InternalEvent:
		return internalEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, e.ShardKey, e.ShardID, e.Payload, e.Metadata, e.CreatedAt)
	case sqlcgen.InsertInternalEventRow:
		return internalEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, e.ShardKey, e.ShardID, e.Payload, e.Metadata, e.CreatedAt)
	case sqlcgen.GetInternalEventsByAggregateRow:
		return internalEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, e.ShardKey, e.ShardID, e.Payload, e.Metadata, e.CreatedAt)
	case sqlcgen.GetInternalEventsSinceRow:
		return internalEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, e.ShardKey, e.ShardID, e.Payload, e.Metadata, e.CreatedAt)
	case sqlcgen.GetInternalEventsSinceByShardRow:
		return internalEventFieldsToDomain(e.ID, e.EventType, e.AggregateType, e.AggregateID, e.WorkspaceID, e.ActorID, e.ShardKey, e.ShardID, e.Payload, e.Metadata, e.CreatedAt)
	default:
		panic("unsupported internal event row type")
	}
}

func internalEventFieldsToDomain(
	id int64,
	eventType, aggregateType, aggregateID, workspaceID, actorID, shardKey string,
	shardID int32,
	payload, metadata json.RawMessage,
	createdAt any,
) domain.InternalEvent {
	return domain.InternalEvent{
		ID:            id,
		EventType:     eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		WorkspaceID:        workspaceID,
		ActorID:       actorID,
		ShardKey:      shardKey,
		ShardID:       int(shardID),
		Payload:       payload,
		Metadata:      metadata,
		CreatedAt:     tsToTime(createdAt),
	}
}

func deriveInternalEventShard(event domain.InternalEvent) (string, int) {
	shardKey := internalEventShardKey(event)
	if shardKey == "" {
		shardKey = event.AggregateID
	}
	if shardKey == "" {
		shardKey = event.WorkspaceID
	}
	if shardKey == "" {
		shardKey = event.EventType
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(shardKey))
	return shardKey, int(hasher.Sum32() % uint32(domain.InternalEventShardCount))
}

func internalEventShardKey(event domain.InternalEvent) string {
	switch event.AggregateType {
	case domain.AggregateConversation:
		return event.AggregateID
	case domain.AggregateMessage:
		var msg domain.Message
		if err := json.Unmarshal(event.Payload, &msg); err == nil && msg.ChannelID != "" {
			return msg.ChannelID
		}
		var reactionEnvelope struct {
			Reaction struct {
				ChannelID string `json:"channel_id"`
			} `json:"reaction"`
		}
		if err := json.Unmarshal(event.Payload, &reactionEnvelope); err == nil && reactionEnvelope.Reaction.ChannelID != "" {
			return reactionEnvelope.Reaction.ChannelID
		}
	case domain.AggregateBookmark:
		var bookmark struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(event.Payload, &bookmark); err == nil && bookmark.ChannelID != "" {
			return bookmark.ChannelID
		}
	case domain.AggregatePin:
		var pin struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(event.Payload, &pin); err == nil && pin.ChannelID != "" {
			return pin.ChannelID
		}
	case domain.AggregateFile:
		if event.EventType == domain.EventFileShared {
			var payload struct {
				ChannelID string `json:"channel_id"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err == nil && payload.ChannelID != "" {
				return payload.ChannelID
			}
		}
	}
	return event.WorkspaceID
}
