package search

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
	"github.com/johnsuh/teraslack/server/internal/eventsourcing"
	"github.com/johnsuh/teraslack/server/internal/queue"
)

type targetJobs struct {
	target syncTarget
	jobIDs []string
}

type mutationJobs struct {
	mutation preparedMutation
	jobIDs   []string
}

type namespaceJobBatch struct {
	batch  namespaceBatch
	jobIDs []string
}

type jobDisposition struct {
	ack      bool
	retryErr error
}

func IndexOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, consumer *queue.Consumer, workerID string, runtime *Runtime) error {
	if err := enqueueSyncJobsOnce(ctx, pool, producer, workerID); err != nil {
		return err
	}
	return processSyncJobsOnce(ctx, runtime, consumer)
}

func enqueueSyncJobsOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, workerID string) error {
	queries := dbsqlc.New(pool)
	for shardID := 0; shardID < eventsourcing.DefaultShardCount; shardID++ {
		claimed, err := claimIndexerShard(ctx, queries, shardID, workerID, 15*time.Second)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := enqueueIndexerShard(ctx, queries, producer, shardID); err != nil {
			return err
		}
	}
	return nil
}

func claimIndexerShard(ctx context.Context, queries *dbsqlc.Queries, shardID int, workerID string, leaseDuration time.Duration) (bool, error) {
	now := time.Now().UTC()
	rowsAffected, err := queries.ClaimLease(ctx, dbsqlc.ClaimLeaseParams{
		Name:       indexerLeaseName,
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

func enqueueIndexerShard(ctx context.Context, queries *dbsqlc.Queries, producer *queue.Producer, shardID int) error {
	checkpointName := fmt.Sprintf("%s:%d", indexerLeaseName, shardID)
	checkpoint, err := loadIndexerCheckpoint(ctx, queries, checkpointName)
	if err != nil {
		return err
	}

	rows, err := queries.ListInternalEventsByShardAfterSequenceID(ctx, dbsqlc.ListInternalEventsByShardAfterSequenceIDParams{
		ShardID:    int32(shardID),
		SequenceID: checkpoint,
		Limit:      indexerBatchSize,
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	items := make([]queue.EnqueueItem, 0, len(rows)*2)
	lastSequenceID := checkpoint
	for _, row := range rows {
		payloads, err := searchJobsForInternalEvent(row)
		if err != nil {
			return err
		}
		for _, payload := range payloads {
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			items = append(items, queue.EnqueueItem{
				Kind:    indexerJobKind,
				Payload: raw,
			})
		}
		lastSequenceID = row.SequenceID
	}
	if len(items) > 0 {
		if err := producer.Enqueue(ctx, items...); err != nil {
			return err
		}
	}
	return queries.UpdateCheckpoint(ctx, dbsqlc.UpdateCheckpointParams{
		Name:           checkpointName,
		LastSequenceID: lastSequenceID,
		UpdatedAt:      dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func loadIndexerCheckpoint(ctx context.Context, queries *dbsqlc.Queries, name string) (int64, error) {
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

func searchJobsForInternalEvent(row dbsqlc.ListInternalEventsByShardAfterSequenceIDRow) ([]syncJobPayload, error) {
	payload := map[string]any{}
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, err
		}
	}

	requiredUUID := func(key string) (string, error) {
		raw, ok := payload[key]
		if !ok {
			return "", fmt.Errorf("%s missing %s", row.EventType, key)
		}
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("%s has invalid %s", row.EventType, key)
		}
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			return "", fmt.Errorf("%s has invalid %s", row.EventType, key)
		}
		return strings.TrimSpace(value), nil
	}

	jobs := make([]syncJobPayload, 0, 3)
	addExternal := func() {
		jobs = append(jobs, syncJobPayload{ResourceKind: documentKindEvent, SourceEventID: row.ID.String()})
	}

	switch row.EventType {
	case "user.created", "user.updated":
		userID, err := requiredUUID("user_id")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, syncJobPayload{ResourceKind: documentKindUser, ResourceID: userID})
		addExternal()
	case "workspace.created", "workspace.updated":
		workspaceID, err := requiredUUID("workspace_id")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, syncJobPayload{ResourceKind: documentKindWorkspace, ResourceID: workspaceID})
		addExternal()
	case "workspace.membership.added", "workspace.membership.updated":
		userID, err := requiredUUID("user_id")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, syncJobPayload{ResourceKind: documentKindUser, ResourceID: userID})
	case "conversation.created", "conversation.updated", "conversation.archived":
		conversationID, err := requiredUUID("conversation_id")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, syncJobPayload{ResourceKind: documentKindConversation, ResourceID: conversationID})
		addExternal()
	case "conversation.participant.added", "conversation.participant.removed":
		conversationID, err := requiredUUID("conversation_id")
		if err != nil {
			return nil, err
		}
		userID, err := requiredUUID("user_id")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs,
			syncJobPayload{ResourceKind: documentKindConversation, ResourceID: conversationID},
			syncJobPayload{ResourceKind: documentKindUser, ResourceID: userID},
		)
		addExternal()
	case "message.posted", "message.updated", "message.deleted":
		messageID, err := requiredUUID("message_id")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, syncJobPayload{ResourceKind: documentKindMessage, ResourceID: messageID})
		addExternal()
	}
	return jobs, nil
}

func processSyncJobsOnce(ctx context.Context, runtime *Runtime, consumer *queue.Consumer) error {
	jobs, err := consumer.Claim(ctx, indexerWorkerBatch)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	stopHeartbeats := startJobHeartbeats(ctx, consumer, jobs)
	defer stopHeartbeats()

	outcomes := map[string]*jobDisposition{}
	for _, job := range jobs {
		outcomes[job.ID] = &jobDisposition{}
	}

	targetMap := map[string]*targetJobs{}
	for _, job := range jobs {
		if job.Kind != indexerJobKind {
			outcomes[job.ID].ack = true
			continue
		}
		target, err := parseSyncTarget(job.Payload)
		if err != nil {
			outcomes[job.ID].retryErr = err
			continue
		}
		key := target.key()
		entry := targetMap[key]
		if entry == nil {
			entry = &targetJobs{target: target}
			targetMap[key] = entry
		}
		entry.jobIDs = append(entry.jobIDs, job.ID)
	}

	prepared := make([]mutationJobs, 0, len(targetMap))
	for _, entry := range targetMap {
		mutation, err := runtime.prepareSyncMutation(ctx, entry.target)
		if err != nil {
			for _, jobID := range entry.jobIDs {
				outcomes[jobID].retryErr = err
			}
			continue
		}
		prepared = append(prepared, mutationJobs{
			mutation: mutation,
			jobIDs:   uniqueSortedStrings(entry.jobIDs),
		})
	}

	if len(prepared) > 0 {
		mutations := make([]preparedMutation, 0, len(prepared))
		for _, entry := range prepared {
			mutations = append(mutations, entry.mutation)
		}
		mutations = runtime.embedPreparedMutations(ctx, mutations)
		for i := range prepared {
			prepared[i].mutation = mutations[i]
		}

		namespaceBatches := map[string]*namespaceJobBatch{}
		for _, entry := range prepared {
			batch := namespaceBatches[entry.mutation.Namespace]
			if batch == nil {
				batch = &namespaceJobBatch{
					batch: namespaceBatch{
						Namespace: entry.mutation.Namespace,
					},
				}
				namespaceBatches[entry.mutation.Namespace] = batch
			}
			batch.batch.ResultKeys = append(batch.batch.ResultKeys, entry.mutation.ResultKey)
			batch.batch.Documents = append(batch.batch.Documents, entry.mutation.Documents...)
			batch.jobIDs = append(batch.jobIDs, entry.jobIDs...)
		}

		for _, batch := range namespaceBatches {
			batch.batch.ResultKeys = uniqueSortedStrings(batch.batch.ResultKeys)
			batch.batch.Documents = mergeDocumentsByID(batch.batch.Documents)
			batch.jobIDs = uniqueSortedStrings(batch.jobIDs)
			if err := runtime.applyNamespaceBatch(ctx, batch.batch); err != nil {
				for _, jobID := range batch.jobIDs {
					outcomes[jobID].retryErr = err
					outcomes[jobID].ack = false
				}
				continue
			}
			for _, jobID := range batch.jobIDs {
				if outcomes[jobID].retryErr == nil {
					outcomes[jobID].ack = true
				}
			}
		}
	}

	ackIDs := make([]string, 0, len(outcomes))
	retryGroups := map[string]struct {
		err    error
		jobIDs []string
	}{}
	for jobID, disposition := range outcomes {
		if disposition == nil {
			continue
		}
		if disposition.retryErr != nil {
			key := disposition.retryErr.Error()
			group := retryGroups[key]
			group.err = disposition.retryErr
			group.jobIDs = append(group.jobIDs, jobID)
			retryGroups[key] = group
			continue
		}
		if disposition.ack {
			ackIDs = append(ackIDs, jobID)
		}
	}

	ackIDs = uniqueSortedStrings(ackIDs)
	if len(ackIDs) > 0 {
		if err := consumer.Ack(ctx, ackIDs...); err != nil {
			return err
		}
	}
	for _, group := range retryGroups {
		jobIDs := uniqueSortedStrings(group.jobIDs)
		if len(jobIDs) == 0 {
			continue
		}
		if err := consumer.Retry(ctx, indexerRetryDelay, group.err, jobIDs...); err != nil {
			return err
		}
	}
	return nil
}

func parseSyncTarget(raw []byte) (syncTarget, error) {
	var payload syncJobPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return syncTarget{}, err
	}
	if strings.TrimSpace(payload.SourceEventID) != "" {
		return syncTarget{SourceEventID: payload.SourceEventID}, nil
	}
	if payload.ResourceKind == "" || payload.ResourceID == "" {
		return syncTarget{}, fmt.Errorf("invalid search sync job payload")
	}
	return syncTarget{
		ResourceKind: payload.ResourceKind,
		ResourceID:   payload.ResourceID,
	}, nil
}

func startJobHeartbeats(ctx context.Context, consumer *queue.Consumer, jobs []queue.ClaimedJob) func() {
	cancels := make([]context.CancelFunc, 0, len(jobs))
	for _, job := range jobs {
		cancels = append(cancels, queue.StartHeartbeat(ctx, consumer, job.ID, indexerHeartbeatFreq, nil))
	}
	return func() {
		for _, cancel := range cancels {
			cancel()
		}
	}
}
