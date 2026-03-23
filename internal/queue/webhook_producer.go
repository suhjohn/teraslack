package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/teraslack/internal/domain"
)

// WebhookProducer tails the external_events table, looks up matching
// event_subscriptions, and fans out one webhook delivery job per subscription
// into an S3-backed queue. It runs as a separate process from the API server.
type WebhookProducer struct {
	pool   *pgxpool.Pool
	queue  *S3Queue
	logger *slog.Logger

	// Configuration
	flushInterval time.Duration
	batchSize     int

	// In-memory buffer protected by mu
	mu     sync.Mutex
	buffer []Job

	// Shutdown coordination
	done chan struct{}
}

// WebhookProducerConfig holds configuration for the WebhookProducer.
type WebhookProducerConfig struct {
	// FlushInterval is how often to flush buffered jobs to S3.
	// Default: 5 seconds.
	FlushInterval time.Duration

	// BatchSize is the max number of jobs to buffer before forcing a flush.
	// Default: 100.
	BatchSize int
}

// NewWebhookProducer creates a new WebhookProducer.
func NewWebhookProducer(pool *pgxpool.Pool, queue *S3Queue, logger *slog.Logger, cfg WebhookProducerConfig) *WebhookProducer {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}

	return &WebhookProducer{
		pool:          pool,
		queue:         queue,
		logger:        logger,
		flushInterval: cfg.FlushInterval,
		batchSize:     cfg.BatchSize,
		buffer:        make([]Job, 0, cfg.BatchSize),
		done:          make(chan struct{}),
	}
}

// Run starts the producer loop. It tails external_events, looks up matching
// subscriptions, fans out webhook jobs, buffers them, and group-commits to S3.
// Blocks until ctx is cancelled. On cancellation, drains the buffer before returning.
func (p *WebhookProducer) Run(ctx context.Context) error {
	cursor, err := p.readCursor(ctx)
	if err != nil {
		return fmt.Errorf("read initial cursor: %w", err)
	}
	p.logger.Info("webhook producer starting", "cursor", cursor)

	pollTicker := time.NewTicker(1 * time.Second)
	defer pollTicker.Stop()

	flushTicker := time.NewTicker(p.flushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("webhook producer shutting down, draining buffer")
			if err := p.flush(context.Background()); err != nil {
				p.logger.Error("flush on shutdown", "error", err)
			}
			close(p.done)
			return nil

		case <-pollTicker.C:
			newCursor, err := p.poll(ctx, cursor)
			if err != nil {
				p.logger.Error("poll external_events", "error", err)
				continue
			}
			cursor = newCursor

			// Force flush if buffer is full
			p.mu.Lock()
			bufLen := len(p.buffer)
			p.mu.Unlock()
			if bufLen >= p.batchSize {
				if err := p.flush(ctx); err != nil {
					p.logger.Error("flush on threshold", "error", err)
				}
			}

		case <-flushTicker.C:
			if err := p.flush(ctx); err != nil {
				p.logger.Error("flush on timer", "error", err)
			}
		}
	}
}

// Done returns a channel that is closed when the producer has finished draining.
func (p *WebhookProducer) Done() <-chan struct{} {
	return p.done
}

// subscriptionRow holds a matching subscription from the DB query.
type subscriptionRow struct {
	ID              string
	URL             string
	Secret          string
	EncryptedSecret string
	CreatedAt       time.Time
}

// poll reads new events from external_events since the given cursor,
// looks up matching subscriptions for each event, and buffers webhook jobs.
func (p *WebhookProducer) poll(ctx context.Context, cursor int64) (int64, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, team_id, type, resource_type, resource_id, occurred_at, payload, source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
		 FROM external_events
		 WHERE id > $1
		 ORDER BY id ASC
		 LIMIT 500`, cursor)
	if err != nil {
		return cursor, fmt.Errorf("query external_events: %w", err)
	}
	defer rows.Close()

	var events []domain.ExternalEvent
	newCursor := cursor

	for rows.Next() {
		var evt domain.ExternalEvent
		var sourceInternalEventID *int64
		var sourceInternalEventIDs json.RawMessage
		if err := rows.Scan(
			&evt.ID,
			&evt.TeamID,
			&evt.Type,
			&evt.ResourceType,
			&evt.ResourceID,
			&evt.OccurredAt,
			&evt.Payload,
			&sourceInternalEventID,
			&sourceInternalEventIDs,
			&evt.DedupeKey,
			&evt.CreatedAt,
		); err != nil {
			return cursor, fmt.Errorf("scan event: %w", err)
		}
		evt.SourceInternalEventID = sourceInternalEventID
		if len(sourceInternalEventIDs) > 0 {
			if err := json.Unmarshal(sourceInternalEventIDs, &evt.SourceInternalEventIDs); err != nil {
				return cursor, fmt.Errorf("decode source internal event ids: %w", err)
			}
		}
		events = append(events, evt)
		newCursor = evt.ID
	}
	if err := rows.Err(); err != nil {
		return cursor, fmt.Errorf("iterate events: %w", err)
	}

	if len(events) == 0 {
		return newCursor, nil
	}

	// For each event, look up matching subscriptions and create webhook jobs
	var jobs []Job
	for _, evt := range events {
		subs, err := p.getMatchingSubscriptions(ctx, evt.TeamID, evt.Type, evt.ResourceType, evt.ResourceID, evt.OccurredAt)
		if err != nil {
			p.logger.Error("get matching subscriptions", "error", err, "event_id", evt.ID)
			continue
		}

		for _, sub := range subs {
			body, err := marshalWebhookEnvelope(evt)
			if err != nil {
				p.logger.Error("marshal webhook payload", "error", err, "event_id", evt.ID)
				continue
			}

			jobs = append(jobs, Job{
				ID:             fmt.Sprintf("wh-%d-%s", evt.ID, sub.ID),
				EventID:        evt.ID,
				TeamID:         evt.TeamID,
				EventType:      evt.Type,
				Status:         StatusPending,
				SubscriptionID: sub.ID,
				URL:            sub.URL,
				Secret:         sub.EncryptedSecret,
				Payload:        body,
				Attempts:       0,
				MaxAttempts:    5,
				CreatedAt:      time.Now(),
			})
		}
	}

	if len(jobs) > 0 {
		p.mu.Lock()
		p.buffer = append(p.buffer, jobs...)
		p.mu.Unlock()
		p.logger.Debug("buffered webhook jobs", "count", len(jobs), "cursor", newCursor)
	}

	return newCursor, nil
}

func marshalWebhookEnvelope(evt domain.ExternalEvent) (json.RawMessage, error) {
	body, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("marshal external event: %w", err)
	}
	return body, nil
}

// getMatchingSubscriptions queries event_subscriptions for active subscriptions
// matching the given team and external event filters.
func (p *WebhookProducer) getMatchingSubscriptions(ctx context.Context, teamID, eventType, resourceType, resourceID string, occurredAt time.Time) ([]subscriptionRow, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, url, secret, encrypted_secret, created_at
		 FROM event_subscriptions
		 WHERE team_id = $1
		   AND enabled = TRUE
		   AND (event_type = '' OR event_type = $2)
		   AND (resource_type = '' OR resource_type = $3)
		   AND (resource_id = '' OR resource_id = $4)
		   AND created_at <= $5`,
		teamID, eventType, resourceType, resourceID, occurredAt)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []subscriptionRow
	for rows.Next() {
		var s subscriptionRow
		if err := rows.Scan(&s.ID, &s.URL, &s.Secret, &s.EncryptedSecret, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// flush writes all buffered jobs to the S3 queue in a single CAS write (group commit).
func (p *WebhookProducer) flush(ctx context.Context) error {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	jobs := make([]Job, len(p.buffer))
	copy(jobs, p.buffer)
	p.buffer = p.buffer[:0]
	p.mu.Unlock()

	maxRetries := 10
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := p.queue.Read(ctx)
		if err != nil {
			return fmt.Errorf("read queue: %w", err)
		}

		snap.State.Jobs = append(snap.State.Jobs, jobs...)

		// Update cursor to the latest event ID
		if len(jobs) > 0 {
			lastEventID := jobs[len(jobs)-1].EventID
			if lastEventID > snap.State.Cursor {
				snap.State.Cursor = lastEventID
			}
		}

		_, err = p.queue.Write(ctx, snap.State, snap.ETag)
		if err == ErrCASConflict {
			p.logger.Debug("CAS conflict on flush, retrying", "attempt", attempt+1)
			continue
		}
		if err != nil {
			p.mu.Lock()
			p.buffer = append(jobs, p.buffer...)
			p.mu.Unlock()
			return fmt.Errorf("write queue: %w", err)
		}

		p.logger.Info("flushed webhook jobs to queue", "count", len(jobs))
		return nil
	}

	p.mu.Lock()
	p.buffer = append(jobs, p.buffer...)
	p.mu.Unlock()
	return fmt.Errorf("flush failed after %d CAS retries", maxRetries)
}

// readCursor reads the current cursor position from the queue state.
func (p *WebhookProducer) readCursor(ctx context.Context) (int64, error) {
	snap, err := p.queue.Read(ctx)
	if err != nil {
		return 0, err
	}
	return snap.State.Cursor, nil
}
