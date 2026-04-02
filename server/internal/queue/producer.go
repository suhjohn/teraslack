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
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
	"github.com/suhjohn/teraslack/internal/service"
)

// IndexProducer tails the internal_events table and pushes index jobs to the
// S3-backed queue using group commit. It buffers jobs in memory and flushes
// them to S3 on a timer or when the buffer reaches a threshold.
type IndexProducer struct {
	pool   *pgxpool.Pool
	q      *sqlcgen.Queries
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

// ProducerConfig holds configuration for the IndexProducer.
type ProducerConfig struct {
	// FlushInterval is how often to flush buffered jobs to S3.
	// Default: 5 seconds.
	FlushInterval time.Duration

	// BatchSize is the max number of jobs to buffer before forcing a flush.
	// Default: 100.
	BatchSize int
}

// NewIndexProducer creates a new IndexProducer.
func NewIndexProducer(pool *pgxpool.Pool, queue *S3Queue, logger *slog.Logger, cfg ProducerConfig) *IndexProducer {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}

	return &IndexProducer{
		pool:          pool,
		q:             sqlcgen.New(pool),
		queue:         queue,
		logger:        logger,
		flushInterval: cfg.FlushInterval,
		batchSize:     cfg.BatchSize,
		buffer:        make([]Job, 0, cfg.BatchSize),
		done:          make(chan struct{}),
	}
}

// Run starts the producer loop. It tails internal_events, converts them to
// index jobs, buffers them in memory, and group-commits to S3 periodically.
// Blocks until ctx is cancelled. On cancellation, drains the buffer before returning.
func (p *IndexProducer) Run(ctx context.Context) error {
	// Read current cursor from queue state
	cursor, err := p.readCursor(ctx)
	if err != nil {
		return fmt.Errorf("read initial cursor: %w", err)
	}
	p.logger.Info("index producer starting", "cursor", cursor)

	pollTicker := time.NewTicker(1 * time.Second)
	defer pollTicker.Stop()

	flushTicker := time.NewTicker(p.flushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("index producer shutting down, draining buffer")
			if err := p.flush(context.Background()); err != nil {
				p.logger.Error("flush on shutdown", "error", err)
			}
			close(p.done)
			return nil

		case <-pollTicker.C:
			newCursor, err := p.poll(ctx, cursor)
			if err != nil {
				p.logger.Error("poll internal_events", "error", err)
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
func (p *IndexProducer) Done() <-chan struct{} {
	return p.done
}

// poll reads new events from internal_events since the given cursor and buffers them.
func (p *IndexProducer) poll(ctx context.Context, cursor int64) (int64, error) {
	rows, err := p.q.GetInternalEventsSince(ctx, sqlcgen.GetInternalEventsSinceParams{
		ID:    cursor,
		Limit: 500,
	})
	if err != nil {
		return cursor, fmt.Errorf("query internal_events: %w", err)
	}

	newCursor := cursor
	var jobs []Job

	for _, row := range rows {
		evt := domain.InternalEvent{
			ID:            row.ID,
			EventType:     row.EventType,
			AggregateType: row.AggregateType,
			AggregateID:   row.AggregateID,
			WorkspaceID:   row.WorkspaceID,
			Payload:       row.Payload,
		}

		job := p.eventToJob(evt)
		if job != nil {
			jobs = append(jobs, *job)
		}
		newCursor = evt.ID
	}

	if len(jobs) > 0 {
		p.mu.Lock()
		p.buffer = append(p.buffer, jobs...)
		p.mu.Unlock()
		p.logger.Debug("buffered index jobs", "count", len(jobs), "cursor", newCursor)
	}

	return newCursor, nil
}

// eventToJob converts a service event into an index job.
// Returns nil for events that don't need indexing (e.g., deletes, reactions).
func (p *IndexProducer) eventToJob(evt domain.InternalEvent) *Job {
	switch evt.EventType {
	case domain.EventUserCreated, domain.EventUserUpdated:
		var u domain.User
		if err := json.Unmarshal(evt.Payload, &u); err != nil {
			p.logger.Warn("unmarshal user for indexing", "error", err)
			return nil
		}
		content := u.Name
		if u.RealName != "" {
			content += " " + u.RealName
		}
		if u.Email != "" {
			content += " " + u.Email
		}
		if u.DisplayName != "" {
			content += " " + u.DisplayName
		}
		return &Job{
			ID:           fmt.Sprintf("evt-%d", evt.ID),
			EventID:      evt.ID,
			ResourceType: "user",
			ResourceID:   u.ID,
			WorkspaceID:  evt.WorkspaceID,
			EventType:    evt.EventType,
			Content:      content,
			Data:         evt.Payload,
			Status:       StatusPending,
			CreatedAt:    time.Now(),
		}

	case domain.EventConversationCreated, domain.EventConversationUpdated,
		domain.EventConversationTopicSet, domain.EventConversationPurposeSet:
		var c domain.Conversation
		if err := json.Unmarshal(evt.Payload, &c); err != nil {
			p.logger.Warn("unmarshal conversation for indexing", "error", err)
			return nil
		}
		content := c.Name
		if c.Topic.Value != "" {
			content += " " + c.Topic.Value
		}
		if c.Purpose.Value != "" {
			content += " " + c.Purpose.Value
		}
		return &Job{
			ID:           fmt.Sprintf("evt-%d", evt.ID),
			EventID:      evt.ID,
			ResourceType: "conversation",
			ResourceID:   c.ID,
			WorkspaceID:  evt.WorkspaceID,
			EventType:    evt.EventType,
			Content:      content,
			Data:         evt.Payload,
			Status:       StatusPending,
			CreatedAt:    time.Now(),
		}

	case domain.EventMessagePosted, domain.EventMessageUpdated:
		var m domain.Message
		if err := json.Unmarshal(evt.Payload, &m); err != nil {
			p.logger.Warn("unmarshal message for indexing", "error", err)
			return nil
		}
		if m.Text == "" {
			return nil
		}
		return &Job{
			ID:           fmt.Sprintf("evt-%d", evt.ID),
			EventID:      evt.ID,
			ResourceType: "message",
			ResourceID:   service.MessageSearchID(m.ChannelID, m.TS),
			WorkspaceID:  evt.WorkspaceID,
			EventType:    evt.EventType,
			Content:      m.Text,
			Data:         evt.Payload,
			Status:       StatusPending,
			CreatedAt:    time.Now(),
		}

	case domain.EventMessageDeleted:
		var m domain.Message
		if err := json.Unmarshal(evt.Payload, &m); err == nil && m.ChannelID != "" && m.TS != "" {
			return &Job{
				ID:           fmt.Sprintf("evt-%d", evt.ID),
				EventID:      evt.ID,
				ResourceType: "message",
				ResourceID:   service.MessageSearchID(m.ChannelID, m.TS),
				WorkspaceID:  evt.WorkspaceID,
				EventType:    evt.EventType,
				Content:      "",
				Data:         evt.Payload,
				Status:       StatusPending,
				CreatedAt:    time.Now(),
			}
		}

		var data struct {
			ChannelID string `json:"channel_id"`
			TS        string `json:"ts"`
		}
		if err := json.Unmarshal(evt.Payload, &data); err != nil {
			p.logger.Warn("unmarshal deleted message for indexing", "error", err)
			return nil
		}
		return &Job{
			ID:           fmt.Sprintf("evt-%d", evt.ID),
			EventID:      evt.ID,
			ResourceType: "message",
			ResourceID:   service.MessageSearchID(data.ChannelID, data.TS),
			WorkspaceID:  evt.WorkspaceID,
			EventType:    evt.EventType,
			Content:      "",
			Data:         evt.Payload,
			Status:       StatusPending,
			CreatedAt:    time.Now(),
		}

	case domain.EventFileCreated, domain.EventFileUpdated:
		var f domain.File
		if err := json.Unmarshal(evt.Payload, &f); err != nil {
			p.logger.Warn("unmarshal file for indexing", "error", err)
			return nil
		}
		content := f.Name
		if f.Title != "" {
			content += " " + f.Title
		}
		return &Job{
			ID:           fmt.Sprintf("evt-%d", evt.ID),
			EventID:      evt.ID,
			ResourceType: "file",
			ResourceID:   f.ID,
			WorkspaceID:  evt.WorkspaceID,
			EventType:    evt.EventType,
			Content:      content,
			Data:         evt.Payload,
			Status:       StatusPending,
			CreatedAt:    time.Now(),
		}

	case domain.EventUserDeleted, domain.EventFileDeleted:
		// For deletes, we still need to remove from the search index.
		return &Job{
			ID:           fmt.Sprintf("evt-%d", evt.ID),
			EventID:      evt.ID,
			ResourceType: evt.AggregateType,
			ResourceID:   evt.AggregateID,
			WorkspaceID:  evt.WorkspaceID,
			EventType:    evt.EventType,
			Content:      "", // No content needed for deletes
			Data:         evt.Payload,
			Status:       StatusPending,
			CreatedAt:    time.Now(),
		}

	default:
		// Skip events that don't need indexing (reactions, tokens, etc.)
		return nil
	}
}

// dedup removes duplicate jobs from a batch by keeping only the latest job
// per (resource_type, resource_id) pair. This is the producer-side dedup
// (Option A) — prevents writing redundant jobs to the S3 queue when the
// same resource is updated multiple times within a single flush interval.
func dedup(jobs []Job) []Job {
	// Walk backwards so the last (newest) job per key wins.
	seen := make(map[string]struct{}, len(jobs))
	deduped := make([]Job, 0, len(jobs))
	for i := len(jobs) - 1; i >= 0; i-- {
		key := jobs[i].ResourceType + ":" + jobs[i].ResourceID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, jobs[i])
	}
	// Reverse to restore chronological order.
	for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
		deduped[i], deduped[j] = deduped[j], deduped[i]
	}
	return deduped
}

// dedupWithExisting removes jobs from newJobs whose resource is already
// pending in the existing queue (Option B — queue-level dedup). This avoids
// writing a new job for a resource that already has an unclaimed job.
func dedupWithExisting(newJobs []Job, existingJobs []Job) []Job {
	pending := make(map[string]struct{}, len(existingJobs))
	for _, j := range existingJobs {
		if j.Status == StatusPending {
			pending[j.ResourceType+":"+j.ResourceID] = struct{}{}
		}
	}
	result := make([]Job, 0, len(newJobs))
	for _, j := range newJobs {
		key := j.ResourceType + ":" + j.ResourceID
		if _, ok := pending[key]; ok {
			continue // already pending in queue
		}
		result = append(result, j)
	}
	return result
}

// flush writes all buffered jobs to the S3 queue in a single CAS write (group commit).
// Applies two levels of dedup before writing:
//  1. Producer-side (Option A): dedup within the current batch by (resource_type, resource_id).
//  2. Queue-level (Option B): skip jobs whose resource already has a pending job in the queue.
func (p *IndexProducer) flush(ctx context.Context) error {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	jobs := make([]Job, len(p.buffer))
	copy(jobs, p.buffer)
	p.buffer = p.buffer[:0]
	p.mu.Unlock()

	// Option A: dedup within the batch
	jobs = dedup(jobs)

	// CAS retry loop
	maxRetries := 10
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := p.queue.Read(ctx)
		if err != nil {
			return fmt.Errorf("read queue: %w", err)
		}

		// Option B: dedup against pending jobs already in the queue
		toAppend := dedupWithExisting(jobs, snap.State.Jobs)

		// Append new (deduped) jobs
		snap.State.Jobs = append(snap.State.Jobs, toAppend...)

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
			// Put jobs back in buffer on failure
			p.mu.Lock()
			p.buffer = append(jobs, p.buffer...)
			p.mu.Unlock()
			return fmt.Errorf("write queue: %w", err)
		}

		p.logger.Info("flushed index jobs to queue",
			"batch_total", len(jobs),
			"after_dedup", len(toAppend))
		return nil
	}

	// Put jobs back in buffer if all retries exhausted
	p.mu.Lock()
	p.buffer = append(jobs, p.buffer...)
	p.mu.Unlock()
	return fmt.Errorf("flush failed after %d CAS retries", maxRetries)
}

// readCursor reads the current cursor position from the queue state.
func (p *IndexProducer) readCursor(ctx context.Context) (int64, error) {
	snap, err := p.queue.Read(ctx)
	if err != nil {
		return 0, err
	}
	return snap.State.Cursor, nil
}
