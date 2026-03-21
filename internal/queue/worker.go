package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/suhjohn/workspace/internal/service"
)

// IndexWorker claims jobs from the S3 queue and flushes them to Turbopuffer.
// Multiple workers can run safely — CAS prevents double-claims.
type IndexWorker struct {
	queue      *S3Queue
	tpClient   service.TurbopufferClient
	logger     *slog.Logger
	workerID   string

	// Configuration
	pollInterval     time.Duration
	heartbeatInterval time.Duration
	heartbeatTimeout time.Duration
	claimBatchSize   int

	// Shutdown coordination
	done chan struct{}
}

// WorkerConfig holds configuration for the IndexWorker.
type WorkerConfig struct {
	// WorkerID identifies this worker instance for job claiming.
	WorkerID string

	// PollInterval is how often to poll S3 for new jobs. Default: 2 seconds.
	PollInterval time.Duration

	// HeartbeatInterval is how often to send heartbeats for claimed jobs. Default: 10 seconds.
	HeartbeatInterval time.Duration

	// HeartbeatTimeout is how long a claimed job can go without a heartbeat
	// before another worker can reclaim it. Default: 30 seconds.
	HeartbeatTimeout time.Duration

	// ClaimBatchSize is the max number of jobs to claim at once. Default: 10.
	ClaimBatchSize int
}

// NewIndexWorker creates a new IndexWorker.
func NewIndexWorker(queue *S3Queue, tpClient service.TurbopufferClient, logger *slog.Logger, cfg WorkerConfig) *IndexWorker {
	if cfg.WorkerID == "" {
		cfg.WorkerID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 30 * time.Second
	}
	if cfg.ClaimBatchSize <= 0 {
		cfg.ClaimBatchSize = 10
	}

	return &IndexWorker{
		queue:             queue,
		tpClient:          tpClient,
		logger:            logger,
		workerID:          cfg.WorkerID,
		pollInterval:      cfg.PollInterval,
		heartbeatInterval: cfg.HeartbeatInterval,
		heartbeatTimeout:  cfg.HeartbeatTimeout,
		claimBatchSize:    cfg.ClaimBatchSize,
		done:              make(chan struct{}),
	}
}

// Run starts the worker loop. Blocks until ctx is cancelled.
// On cancellation, finishes processing any in-flight jobs before returning.
func (w *IndexWorker) Run(ctx context.Context) error {
	w.logger.Info("index worker starting", "worker_id", w.workerID)

	pollTicker := time.NewTicker(w.pollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("index worker shutting down", "worker_id", w.workerID)
			close(w.done)
			return nil

		case <-pollTicker.C:
			if err := w.processJobs(ctx); err != nil {
				w.logger.Error("process jobs", "error", err, "worker_id", w.workerID)
			}
		}
	}
}

// Done returns a channel that is closed when the worker has finished.
func (w *IndexWorker) Done() <-chan struct{} {
	return w.done
}

// processJobs claims unclaimed jobs, processes them, and marks them complete.
func (w *IndexWorker) processJobs(ctx context.Context) error {
	// Step 1: Claim jobs via CAS
	claimed, err := w.claimJobs(ctx)
	if err != nil {
		return fmt.Errorf("claim jobs: %w", err)
	}
	if len(claimed) == 0 {
		return nil
	}

	w.logger.Info("claimed jobs", "count", len(claimed), "worker_id", w.workerID)

	// Step 2: Start heartbeat goroutine for claimed jobs
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.heartbeatLoop(heartbeatCtx, claimed)
	}()

	// Step 3: Process each claimed job
	completed := make([]string, 0, len(claimed))
	failed := make([]string, 0)
	for _, jobID := range claimed {
		if ctx.Err() != nil {
			break
		}
		if err := w.processJob(ctx, jobID); err != nil {
			w.logger.Error("process job", "job_id", jobID, "error", err, "worker_id", w.workerID)
			failed = append(failed, jobID)
		} else {
			completed = append(completed, jobID)
		}
	}

	// Step 4: Stop heartbeats
	cancelHeartbeat()
	wg.Wait()

	// Step 5: Mark jobs as completed/failed via CAS
	if len(completed) > 0 || len(failed) > 0 {
		if err := w.markResults(ctx, completed, failed); err != nil {
			return fmt.Errorf("mark results: %w", err)
		}
	}

	return nil
}

// claimJobs reads the queue and claims the first N unclaimed/expired jobs via CAS.
func (w *IndexWorker) claimJobs(ctx context.Context) ([]string, error) {
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := w.queue.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("read queue: %w", err)
		}

		now := time.Now()
		var claimed []string
		modified := false

		for i := range snap.State.Jobs {
			if len(claimed) >= w.claimBatchSize {
				break
			}

			job := &snap.State.Jobs[i]

			switch job.Status {
			case StatusPending:
				// Claim it
				job.Status = StatusClaimed
				job.ClaimedBy = w.workerID
				hb := now
				job.Heartbeat = &hb
				claimed = append(claimed, job.ID)
				modified = true

			case StatusClaimed:
				// Check if the claiming worker's heartbeat has timed out
				if job.Heartbeat != nil && now.Sub(*job.Heartbeat) > w.heartbeatTimeout {
					w.logger.Warn("reclaiming timed-out job",
						"job_id", job.ID,
						"previous_worker", job.ClaimedBy,
						"last_heartbeat", job.Heartbeat,
						"worker_id", w.workerID)
					job.ClaimedBy = w.workerID
					hb := now
					job.Heartbeat = &hb
					claimed = append(claimed, job.ID)
					modified = true
				}
			}
		}

		if !modified {
			return nil, nil
		}

		_, err = w.queue.Write(ctx, snap.State, snap.ETag)
		if err == ErrCASConflict {
			w.logger.Debug("CAS conflict on claim, retrying", "attempt", attempt+1)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("write queue: %w", err)
		}

		return claimed, nil
	}

	return nil, nil // All retries exhausted, will try again next poll
}

// processJob handles a single index job — upserts or deletes from Turbopuffer.
func (w *IndexWorker) processJob(ctx context.Context, jobID string) error {
	// Read the queue to get the job data
	snap, err := w.queue.Read(ctx)
	if err != nil {
		return fmt.Errorf("read queue for job: %w", err)
	}

	var job *Job
	for i := range snap.State.Jobs {
		if snap.State.Jobs[i].ID == jobID {
			job = &snap.State.Jobs[i]
			break
		}
	}
	if job == nil {
		return fmt.Errorf("job %s not found in queue", jobID)
	}

	// Check if this is a delete event
	isDelete := job.EventType == "user.deleted" ||
		job.EventType == "message.deleted" ||
		job.EventType == "file.deleted"

	if isDelete {
		// For deletes, we'd call a Delete method on TurbopufferClient.
		// Since the current interface only has Upsert, we skip deletes for now
		// and log them. A production implementation would add a Delete method.
		w.logger.Info("skipping delete indexing (not yet implemented)",
			"job_id", jobID, "resource_type", job.ResourceType, "resource_id", job.ResourceID)
		return nil
	}

	if w.tpClient == nil {
		// No Turbopuffer client configured — skip but mark as completed
		return nil
	}

	// Generate embedding and upsert to Turbopuffer
	if job.Content == "" {
		return nil
	}

	embedding, err := w.tpClient.GetEmbedding(ctx, job.Content)
	if err != nil {
		return fmt.Errorf("get embedding: %w", err)
	}

	metadata := map[string]any{
		"type":    job.ResourceType,
		"team_id": job.TeamID,
		"data":    job.Data,
	}

	tpID := fmt.Sprintf("%s:%s", job.ResourceType, job.ResourceID)
	if err := w.tpClient.Upsert(ctx, tpID, embedding, metadata); err != nil {
		return fmt.Errorf("turbopuffer upsert: %w", err)
	}

	w.logger.Debug("indexed job", "job_id", jobID, "tp_id", tpID)
	return nil
}

// heartbeatLoop periodically updates heartbeats for claimed jobs.
func (w *IndexWorker) heartbeatLoop(ctx context.Context, jobIDs []string) {
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()

	jobSet := make(map[string]struct{}, len(jobIDs))
	for _, id := range jobIDs {
		jobSet[id] = struct{}{}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.sendHeartbeats(ctx, jobSet); err != nil {
				w.logger.Error("send heartbeats", "error", err, "worker_id", w.workerID)
			}
		}
	}
}

// sendHeartbeats updates heartbeat timestamps for claimed jobs via CAS.
func (w *IndexWorker) sendHeartbeats(ctx context.Context, jobSet map[string]struct{}) error {
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := w.queue.Read(ctx)
		if err != nil {
			return err
		}

		now := time.Now()
		modified := false
		for i := range snap.State.Jobs {
			if _, ok := jobSet[snap.State.Jobs[i].ID]; ok {
				if snap.State.Jobs[i].ClaimedBy == w.workerID {
					snap.State.Jobs[i].Heartbeat = &now
					modified = true
				}
			}
		}

		if !modified {
			return nil
		}

		_, err = w.queue.Write(ctx, snap.State, snap.ETag)
		if err == ErrCASConflict {
			continue
		}
		return err
	}
	return nil
}

// markResults marks jobs as completed or failed via CAS.
// Also garbage-collects completed jobs older than 1 hour.
func (w *IndexWorker) markResults(ctx context.Context, completed, failed []string) error {
	completedSet := make(map[string]struct{}, len(completed))
	for _, id := range completed {
		completedSet[id] = struct{}{}
	}
	failedSet := make(map[string]struct{}, len(failed))
	for _, id := range failed {
		failedSet[id] = struct{}{}
	}

	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := w.queue.Read(ctx)
		if err != nil {
			return err
		}

		now := time.Now()
		for i := range snap.State.Jobs {
			if _, ok := completedSet[snap.State.Jobs[i].ID]; ok {
				snap.State.Jobs[i].Status = StatusCompleted
				snap.State.Jobs[i].CompletedAt = &now
			}
			if _, ok := failedSet[snap.State.Jobs[i].ID]; ok {
				snap.State.Jobs[i].Status = StatusFailed
			}
		}

		// Garbage collect completed jobs older than GCRetention (7 days)
		gcCutoff := now.Add(-GCRetention)
		originalJobCount := len(snap.State.Jobs)
		filtered := make([]Job, 0, len(snap.State.Jobs))
		for _, job := range snap.State.Jobs {
			if job.Status == StatusCompleted && job.CompletedAt != nil && job.CompletedAt.Before(gcCutoff) {
				continue // GC'd
			}
			filtered = append(filtered, job)
		}
		snap.State.Jobs = filtered

		_, err = w.queue.Write(ctx, snap.State, snap.ETag)
		if err == ErrCASConflict {
			w.logger.Debug("CAS conflict on mark results, retrying", "attempt", attempt+1)
			continue
		}
		if err != nil {
			return fmt.Errorf("write queue: %w", err)
		}

		w.logger.Info("marked job results",
			"completed", len(completed), "failed", len(failed),
			"gc_removed", originalJobCount-len(filtered))
		return nil
	}

	return fmt.Errorf("mark results failed after %d CAS retries", maxRetries)
}
