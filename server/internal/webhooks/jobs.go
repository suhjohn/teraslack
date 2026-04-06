package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/queue"
)

const (
	producerLeaseName      = "webhook-producer"
	producerCheckpointName = "webhook-producer"
	webhookJobKind         = "webhook_delivery"
	producerBatchSize      = 100
	workerBatchSize        = 25
	workerRetryDelay       = time.Minute
)

type deliveryJob struct {
	SubscriptionID  string `json:"subscription_id"`
	ExternalEventID int64  `json:"external_event_id"`
}

type externalEventEnvelope struct {
	ID           int64           `json:"id"`
	WorkspaceID  *string         `json:"workspace_id,omitempty"`
	Type         string          `json:"type"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Payload      json.RawMessage `json:"payload"`
}

func EnqueueDeliveriesOnce(ctx context.Context, pool *pgxpool.Pool, producer *queue.Producer, workerID string) error {
	queries := dbsqlc.New(pool)
	claimed, err := claimLease(ctx, queries, producerLeaseName, 0, workerID)
	if err != nil || !claimed {
		return err
	}

	checkpoint, err := loadCheckpoint(ctx, queries, producerCheckpointName)
	if err != nil {
		return err
	}

	events, err := queries.ListExternalEventsForWebhookQueueAfterID(ctx, dbsqlc.ListExternalEventsForWebhookQueueAfterIDParams{
		ID:         checkpoint,
		BatchLimit: producerBatchSize,
	})
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	items := make([]queue.EnqueueItem, 0)
	lastEventID := checkpoint
	for _, event := range events {
		resourceID := event.ResourceID
		subscriptions, err := queries.ListWebhookSubscriptionsForExternalEvent(ctx, dbsqlc.ListWebhookSubscriptionsForExternalEventParams{
			ExternalEventID: event.ID,
			WorkspaceID:     event.WorkspaceID,
			Type:            event.Type,
			ResourceType:    event.ResourceType,
			ResourceID:      &resourceID,
		})
		if err != nil {
			return err
		}
		for _, subscriptionID := range subscriptions {
			payload, err := json.Marshal(deliveryJob{
				SubscriptionID:  subscriptionID.String(),
				ExternalEventID: event.ID,
			})
			if err != nil {
				return err
			}
			items = append(items, queue.EnqueueItem{
				Kind:    webhookJobKind,
				Payload: payload,
			})
		}
		lastEventID = event.ID
	}

	if len(items) > 0 {
		if err := producer.Enqueue(ctx, items...); err != nil {
			return err
		}
	}

	return queries.UpdateCheckpoint(ctx, dbsqlc.UpdateCheckpointParams{
		Name:        producerCheckpointName,
		LastEventID: lastEventID,
		UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
	})
}

func ProcessDeliveriesOnce(ctx context.Context, pool *pgxpool.Pool, consumer *queue.Consumer, protector *teracrypto.StringProtector) error {
	if protector == nil {
		return fmt.Errorf("string protector is required to process webhook deliveries")
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	queries := dbsqlc.New(pool)
	return queue.ConsumeOnce(ctx, consumer, workerBatchSize, 30*time.Second, workerRetryDelay, func(ctx context.Context, job queue.ClaimedJob) error {
		return processDelivery(ctx, httpClient, queries, protector, job)
	})
}

func processDelivery(ctx context.Context, client *http.Client, queries *dbsqlc.Queries, protector *teracrypto.StringProtector, job queue.ClaimedJob) error {
	if job.Kind != webhookJobKind {
		return nil
	}

	var payload deliveryJob
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}

	subscriptionID, err := uuid.Parse(payload.SubscriptionID)
	if err != nil {
		return err
	}

	row, err := queries.GetWebhookDeliverySource(ctx, dbsqlc.GetWebhookDeliverySourceParams{
		ExternalEventID: payload.ExternalEventID,
		SubscriptionID:  subscriptionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	secret := ""
	if row.EncryptedSecret != "" {
		decrypted, err := protector.DecryptString(ctx, row.EncryptedSecret)
		if err != nil {
			return err
		}
		secret = decrypted
	}

	event := externalEventEnvelope{
		ID:           row.EventID,
		Type:         row.Type,
		ResourceType: row.ResourceType,
		ResourceID:   row.ResourceID.String(),
		OccurredAt:   dbsqlc.TimeValue(row.OccurredAt),
		Payload:      row.Payload,
	}
	if row.WorkspaceID != nil {
		workspaceID := row.WorkspaceID.String()
		event.WorkspaceID = &workspaceID
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, row.Url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if secret != "" {
		request.Header.Set("X-Teraslack-Signature", teracrypto.HMACSHA256Hex(secret, string(body)))
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", response.StatusCode)
	}
	return nil
}

func claimLease(ctx context.Context, queries *dbsqlc.Queries, name string, shardID int, workerID string) (bool, error) {
	now := time.Now().UTC()
	rowsAffected, err := queries.ClaimLease(ctx, dbsqlc.ClaimLeaseParams{
		Name:       name,
		ShardID:    int32(shardID),
		Owner:      workerID,
		LeaseUntil: dbsqlc.Timestamptz(now.Add(15 * time.Second)),
		UpdatedAt:  dbsqlc.Timestamptz(now),
	})
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func loadCheckpoint(ctx context.Context, queries *dbsqlc.Queries, name string) (int64, error) {
	checkpoint, err := queries.GetCheckpointForUpdate(ctx, name)
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err := queries.InsertCheckpointIfMissing(ctx, dbsqlc.InsertCheckpointIfMissingParams{
		Name:        name,
		LastEventID: 0,
		UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
	}); err != nil {
		return 0, err
	}
	return 0, nil
}
