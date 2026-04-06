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
	"github.com/johnsuh/teraslack/server/internal/queue"
)

const (
	indexJobKind      = "search_document_upsert"
	indexRetryDelay   = 30 * time.Second
	indexBatchSize    = 200
	indexWorkerBatch  = 50
	heartbeatInterval = 30 * time.Second
)

type indexJob struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

func RunIndexerOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, consumer *queue.Consumer, workerID string) error {
	if err := enqueueIndexJobsOnce(ctx, pool, producer, workerID); err != nil {
		return err
	}
	return processIndexJobsOnce(ctx, pool, consumer)
}

func enqueueIndexJobsOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, workerID string) error {
	queries := dbsqlc.New(pool)
	claimed, err := claimIndexerLease(ctx, queries, workerID)
	if err != nil || !claimed {
		return err
	}

	checkpoint, err := loadIndexerCheckpoint(ctx, queries)
	if err != nil {
		return err
	}

	batch, err := queries.ListExternalEventsAfterID(ctx, dbsqlc.ListExternalEventsAfterIDParams{
		ID:    checkpoint,
		Limit: indexBatchSize,
	})
	if err != nil {
		return fmt.Errorf("load external events for indexing: %w", err)
	}
	if len(batch) == 0 {
		return nil
	}

	items := make([]queue.EnqueueItem, 0, len(batch))
	lastID := checkpoint
	for _, row := range batch {
		payload, err := json.Marshal(indexJob{
			ResourceType: row.ResourceType,
			ResourceID:   row.ResourceID.String(),
		})
		if err != nil {
			return err
		}
		items = append(items, queue.EnqueueItem{
			Kind:    indexJobKind,
			Payload: payload,
		})
		lastID = row.ID
	}

	if err := producer.Enqueue(ctx, items...); err != nil {
		return err
	}

	return queries.UpdateCheckpoint(ctx, dbsqlc.UpdateCheckpointParams{
		Name:        "indexer",
		LastEventID: lastID,
		UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func processIndexJobsOnce(ctx context.Context, pool *pgxpool.Pool, consumer *queue.Consumer) error {
	queries := dbsqlc.New(pool)
	return queue.ConsumeOnce(ctx, consumer, indexWorkerBatch, heartbeatInterval, indexRetryDelay, func(ctx context.Context, job queue.ClaimedJob) error {
		return processIndexJob(ctx, queries, job)
	})
}

func claimIndexerLease(ctx context.Context, queries *dbsqlc.Queries, workerID string) (bool, error) {
	now := time.Now().UTC()
	rowsAffected, err := queries.ClaimLease(ctx, dbsqlc.ClaimLeaseParams{
		Name:       "indexer",
		ShardID:    0,
		Owner:      workerID,
		LeaseUntil: dbsqlc.Timestamptz(now.Add(15 * time.Second)),
		UpdatedAt:  dbsqlc.Timestamptz(now),
	})
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func loadIndexerCheckpoint(ctx context.Context, queries *dbsqlc.Queries) (int64, error) {
	checkpoint, err := queries.GetCheckpointForUpdate(ctx, "indexer")
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err := queries.InsertCheckpointIfMissing(ctx, dbsqlc.InsertCheckpointIfMissingParams{
		Name:        "indexer",
		LastEventID: 0,
		UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
	}); err != nil {
		return 0, err
	}
	return 0, nil
}

func processIndexJob(ctx context.Context, queries *dbsqlc.Queries, job queue.ClaimedJob) error {
	if job.Kind != indexJobKind {
		return nil
	}

	var payload indexJob
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}

	resourceID, err := uuid.Parse(payload.ResourceID)
	if err != nil {
		return err
	}

	switch payload.ResourceType {
	case "user":
		return upsertUserDocument(ctx, queries, resourceID)
	case "workspace":
		return upsertWorkspaceDocument(ctx, queries, resourceID)
	case "conversation":
		return upsertConversationDocument(ctx, queries, resourceID)
	default:
		return nil
	}
}

func upsertUserDocument(ctx context.Context, queries *dbsqlc.Queries, userID uuid.UUID) error {
	row, err := queries.GetUserSearchSource(ctx, userID)
	if err == pgx.ErrNoRows {
		return queries.DeleteUserSearchDocument(ctx, userID)
	}
	if err != nil {
		return err
	}
	title := row.DisplayName
	handle := row.Handle
	email := row.Email
	subtitle := "@" + handle
	content := strings.TrimSpace(strings.Join([]string{title, subtitle, derefString(email)}, " "))
	return queries.UpsertUserSearchDocument(ctx, dbsqlc.UpsertUserSearchDocumentParams{
		EntityID:  userID,
		Title:     title,
		Subtitle:  &subtitle,
		Content:   content,
		UpdatedAt: dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func upsertWorkspaceDocument(ctx context.Context, queries *dbsqlc.Queries, workspaceID uuid.UUID) error {
	row, err := queries.GetWorkspaceSearchSource(ctx, workspaceID)
	if err == pgx.ErrNoRows {
		return queries.DeleteWorkspaceSearchDocument(ctx, workspaceID)
	}
	if err != nil {
		return err
	}
	name := row.Name
	slug := row.Slug
	content := strings.TrimSpace(strings.Join([]string{name, slug}, " "))
	return queries.UpsertWorkspaceSearchDocument(ctx, dbsqlc.UpsertWorkspaceSearchDocumentParams{
		EntityID:  workspaceID,
		Title:     name,
		Subtitle:  &slug,
		Content:   content,
		UpdatedAt: dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func upsertConversationDocument(ctx context.Context, queries *dbsqlc.Queries, conversationID uuid.UUID) error {
	row, err := queries.GetConversationSearchSource(ctx, conversationID)
	if err == pgx.ErrNoRows {
		return queries.DeleteConversationSearchDocument(ctx, conversationID)
	}
	if err != nil {
		return err
	}
	participants, err := queries.ListConversationParticipantIdentities(ctx, conversationID)
	if err != nil {
		return err
	}

	var names []string
	var handles []string
	for _, participant := range participants {
		names = append(names, participant.DisplayName)
		handles = append(handles, "@"+participant.Handle)
	}

	searchTitle := derefString(row.Title)
	if searchTitle == "" {
		searchTitle = strings.Join(names, ", ")
	}
	if searchTitle == "" {
		searchTitle = conversationID.String()
	}
	subtitle := row.AccessPolicy
	content := strings.TrimSpace(strings.Join([]string{
		searchTitle,
		derefString(row.Description),
		strings.Join(names, " "),
		strings.Join(handles, " "),
		row.AccessPolicy,
	}, " "))

	return queries.UpsertConversationSearchDocument(ctx, dbsqlc.UpsertConversationSearchDocumentParams{
		EntityID:    conversationID,
		WorkspaceID: row.WorkspaceID,
		Title:       searchTitle,
		Subtitle:    &subtitle,
		Content:     content,
		UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
