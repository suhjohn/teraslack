package eventsourcing

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
)

type InternalEvent struct {
	EventType     string
	AggregateType string
	AggregateID   uuid.UUID
	WorkspaceID   *uuid.UUID
	ActorUserID   *uuid.UUID
	ShardID       int
	Payload       any
	CreatedAt     time.Time
}

const DefaultShardCount = 16

func ShardForAggregate(aggregateID uuid.UUID) int {
	return int(binary.BigEndian.Uint32(aggregateID[:4]) % DefaultShardCount)
}

func AppendInternalEvent(ctx context.Context, tx pgx.Tx, event InternalEvent) (int64, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return 0, fmt.Errorf("marshal internal event payload: %w", err)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return dbsqlc.New(tx).InsertInternalEvent(ctx, dbsqlc.InsertInternalEventParams{
		EventType:     event.EventType,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		WorkspaceID:   event.WorkspaceID,
		ActorUserID:   event.ActorUserID,
		ShardID:       int32(event.ShardID),
		Payload:       json.RawMessage(payload),
		CreatedAt:     dbsqlc.Timestamptz(event.CreatedAt),
	})
}
