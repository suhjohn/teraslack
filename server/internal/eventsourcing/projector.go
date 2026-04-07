package eventsourcing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/queue"
)

type internalEventRow struct {
	ID          uuid.UUID
	EventType   string
	WorkspaceID *uuid.UUID
	Payload     []byte
	CreatedAt   time.Time
}

type projectionFailure struct {
	err error
}

const (
	projectorLeaseName     = "external-event-projector"
	projectorJobKind       = "external_event_projection"
	projectorBatchSize     = 200
	projectorWorkerBatch   = 100
	projectorRetryDelay    = 15 * time.Second
	projectorHeartbeatFreq = 30 * time.Second
)

type projectorJob struct {
	InternalEventID string `json:"internal_event_id"`
}

func (e *projectionFailure) Error() string {
	return e.err.Error()
}

func (e *projectionFailure) Unwrap() error {
	return e.err
}

func ProjectExternalEventsOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, consumer *queue.Consumer, workerID string) error {
	if err := enqueueProjectionJobsOnce(ctx, pool, producer, workerID); err != nil {
		return err
	}
	return processProjectionJobsOnce(ctx, pool, consumer)
}

func enqueueProjectionJobsOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, workerID string) error {
	queries := dbsqlc.New(pool)
	for shardID := 0; shardID < DefaultShardCount; shardID++ {
		claimed, err := claimProjectorShard(ctx, queries, projectorLeaseName, shardID, workerID, 15*time.Second)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := enqueueProjectorShard(ctx, queries, producer, shardID); err != nil {
			return err
		}
	}
	return nil
}

func claimProjectorShard(ctx context.Context, queries *dbsqlc.Queries, name string, shardID int, workerID string, leaseDuration time.Duration) (bool, error) {
	now := time.Now().UTC()
	rowsAffected, err := queries.ClaimLease(ctx, dbsqlc.ClaimLeaseParams{
		Name:       name,
		ShardID:    int32(shardID),
		Owner:      workerID,
		LeaseUntil: dbsqlc.Timestamptz(now.Add(leaseDuration)),
		UpdatedAt:  dbsqlc.Timestamptz(now),
	})
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func enqueueProjectorShard(ctx context.Context, queries *dbsqlc.Queries, producer *queue.Producer, shardID int) error {
	checkpointName := fmt.Sprintf("external-event-projector:%d", shardID)
	checkpoint, err := loadProjectionCheckpoint(ctx, queries, checkpointName)
	if err != nil {
		return err
	}

	batchRows, err := queries.ListInternalEventsByShardAfterSequenceID(ctx, dbsqlc.ListInternalEventsByShardAfterSequenceIDParams{
		ShardID:    int32(shardID),
		SequenceID: checkpoint,
		Limit:      projectorBatchSize,
	})
	if err != nil {
		return err
	}
	if len(batchRows) == 0 {
		return nil
	}

	items := make([]queue.EnqueueItem, 0, len(batchRows))
	lastSequenceID := checkpoint
	for _, item := range batchRows {
		payload, err := json.Marshal(projectorJob{InternalEventID: item.ID.String()})
		if err != nil {
			return err
		}
		items = append(items, queue.EnqueueItem{
			Kind:    projectorJobKind,
			Payload: payload,
		})
		lastSequenceID = item.SequenceID
	}

	if err := producer.Enqueue(ctx, items...); err != nil {
		return err
	}

	return queries.UpdateCheckpoint(ctx, dbsqlc.UpdateCheckpointParams{
		Name:           checkpointName,
		LastSequenceID: lastSequenceID,
		UpdatedAt:      dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func loadProjectionCheckpoint(ctx context.Context, queries *dbsqlc.Queries, name string) (int64, error) {
	checkpoint, err := queries.GetCheckpointForUpdate(ctx, name)
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err := queries.InsertCheckpointIfMissing(ctx, dbsqlc.InsertCheckpointIfMissingParams{
		Name:           name,
		LastSequenceID: 0,
		UpdatedAt:      dbsqlc.Timestamptz(time.Now().UTC()),
	}); err != nil {
		return 0, err
	}
	return 0, nil
}

func processProjectionJobsOnce(ctx context.Context, pool *pgxpool.Pool, consumer *queue.Consumer) error {
	return queue.ConsumeOnce(ctx, consumer, projectorWorkerBatch, projectorHeartbeatFreq, projectorRetryDelay, func(ctx context.Context, job queue.ClaimedJob) error {
		return processProjectionJob(ctx, pool, job)
	})
}

func processProjectionJob(ctx context.Context, pool *pgxpool.Pool, job queue.ClaimedJob) error {
	if job.Kind != projectorJobKind {
		return nil
	}

	var payload projectorJob
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	internalEventID, err := uuid.Parse(payload.InternalEventID)
	if err != nil {
		return err
	}

	return withTransaction(ctx, pool, func(tx pgx.Tx) error {
		txQueries := dbsqlc.New(tx)
		item, err := txQueries.GetInternalEventForProjection(ctx, internalEventID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}

		row := internalEventRow{
			ID:          item.ID,
			EventType:   item.EventType,
			WorkspaceID: item.WorkspaceID,
			Payload:     item.Payload,
			CreatedAt:   dbsqlc.TimeValue(item.CreatedAt),
		}
		if err := projectEvent(ctx, txQueries, row); err != nil {
			var failure *projectionFailure
			if errors.As(err, &failure) {
				return txQueries.InsertExternalProjectionFailure(ctx, dbsqlc.InsertExternalProjectionFailureParams{
					InternalEventID: row.ID,
					Error:           failure.Error(),
					CreatedAt:       dbsqlc.Timestamptz(time.Now().UTC()),
				})
			}
			return err
		}
		return nil
	})
}

func projectEvent(ctx context.Context, queries *dbsqlc.Queries, row internalEventRow) error {
	payload := map[string]any{}
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return &projectionFailure{err: fmt.Errorf("%s: decode payload: %w", row.EventType, err)}
		}
	}

	type eventSpec struct {
		Type           string
		ResourceType   string
		ResourceID     uuid.UUID
		ConversationID *uuid.UUID
		UserID         *uuid.UUID
	}

	specs := make([]eventSpec, 0, 1)
	switch row.EventType {
	case "user.created", "user.updated":
		userID, err := requiredUUIDFromPayload(payload, "user_id", row.EventType)
		if err != nil {
			return err
		}
		specs = append(specs, eventSpec{Type: row.EventType, ResourceType: "user", ResourceID: userID, UserID: &userID})
	case "workspace.created", "workspace.updated":
		workspaceID, err := requiredUUIDFromPayload(payload, "workspace_id", row.EventType)
		if err != nil {
			return err
		}
		specs = append(specs, eventSpec{Type: row.EventType, ResourceType: "workspace", ResourceID: workspaceID})
	case "conversation.created", "conversation.updated", "conversation.archived", "conversation.participant.added", "conversation.participant.removed":
		conversationID, err := requiredUUIDFromPayload(payload, "conversation_id", row.EventType)
		if err != nil {
			return err
		}
		specs = append(specs, eventSpec{
			Type:           row.EventType,
			ResourceType:   "conversation",
			ResourceID:     conversationID,
			ConversationID: &conversationID,
		})
	case "message.posted", "message.updated", "message.deleted":
		if _, err := requiredUUIDFromPayload(payload, "message_id", row.EventType); err != nil {
			return err
		}
		conversationID, err := requiredUUIDFromPayload(payload, "conversation_id", row.EventType)
		if err != nil {
			return err
		}
		publicType := map[string]string{
			"message.posted":  "conversation.message.created",
			"message.updated": "conversation.message.updated",
			"message.deleted": "conversation.message.deleted",
		}[row.EventType]
		specs = append(specs, eventSpec{
			Type:           publicType,
			ResourceType:   "conversation",
			ResourceID:     conversationID,
			ConversationID: &conversationID,
		})
	default:
		return nil
	}

	for _, spec := range specs {
		dedupeKey := fmt.Sprintf("internal:%d:%s", row.ID, spec.Type)
		externalEventID, err := queries.InsertExternalEvent(ctx, dbsqlc.InsertExternalEventParams{
			WorkspaceID:           row.WorkspaceID,
			Type:                  spec.Type,
			ResourceType:          spec.ResourceType,
			ResourceID:            spec.ResourceID,
			OccurredAt:            dbsqlc.Timestamptz(row.CreatedAt),
			Payload:               json.RawMessage(row.Payload),
			SourceInternalEventID: &row.ID,
			DedupeKey:             dedupeKey,
			CreatedAt:             dbsqlc.Timestamptz(time.Now().UTC()),
		})
		if err != nil {
			return err
		}
		if row.WorkspaceID != nil {
			if err := queries.InsertWorkspaceEventFeed(ctx, dbsqlc.InsertWorkspaceEventFeedParams{
				WorkspaceID:     *row.WorkspaceID,
				ExternalEventID: externalEventID,
			}); err != nil {
				return err
			}
		}
		if spec.ConversationID != nil {
			if err := queries.InsertConversationEventFeed(ctx, dbsqlc.InsertConversationEventFeedParams{
				ConversationID:  *spec.ConversationID,
				ExternalEventID: externalEventID,
			}); err != nil {
				return err
			}
		}
		if spec.UserID != nil {
			if err := queries.InsertUserEventFeed(ctx, dbsqlc.InsertUserEventFeedParams{
				UserID:          *spec.UserID,
				ExternalEventID: externalEventID,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

func uuidFromPayload(payload map[string]any, key string) (uuid.UUID, error) {
	value, ok := payload[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return uuid.UUID{}, fmt.Errorf("missing %s in payload", key)
	}
	return uuid.Parse(value)
}

func requiredUUIDFromPayload(payload map[string]any, key string, eventType string) (uuid.UUID, error) {
	value, err := uuidFromPayload(payload, key)
	if err != nil {
		return uuid.UUID{}, &projectionFailure{err: fmt.Errorf("%s: %w", eventType, err)}
	}
	return value, nil
}

func withTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
