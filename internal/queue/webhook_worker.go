package queue

import (
	"bytes"
	"context"
		"crypto/hmac"
		"crypto/sha256"
		"encoding/hex"
		"fmt"
		"io"
		"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/suhjohn/workspace/internal/crypto"
)

// WebhookWorker claims webhook delivery jobs from an S3-backed queue,
// delivers them via HMAC-signed HTTP POST, and handles retries with
// exponential backoff. Runs as a separate process from the API server.
type WebhookWorker struct {
	queue      *S3Queue
	encryptor  *crypto.Encryptor
	httpClient *http.Client
	logger     *slog.Logger
	workerID   string

	// Configuration
	pollInterval      time.Duration
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	claimBatchSize    int

	// Shutdown coordination
	done chan struct{}
}

// WebhookWorkerConfig holds configuration for the WebhookWorker.
type WebhookWorkerConfig struct {
	WorkerID          string
	PollInterval      time.Duration // Default: 2s
	HeartbeatInterval time.Duration // Default: 10s
	HeartbeatTimeout  time.Duration // Default: 30s
	ClaimBatchSize    int           // Default: 10
}

// NewWebhookWorker creates a new WebhookWorker.
func NewWebhookWorker(queue *S3Queue, encryptor *crypto.Encryptor, logger *slog.Logger, cfg WebhookWorkerConfig) *WebhookWorker {
	if cfg.WorkerID == "" {
		cfg.WorkerID = fmt.Sprintf("wh-worker-%d", time.Now().UnixNano())
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

	return &WebhookWorker{
		queue:     queue,
		encryptor: encryptor,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
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
func (w *WebhookWorker) Run(ctx context.Context) error {
	w.logger.Info("webhook worker starting", "worker_id", w.workerID)

	pollTicker := time.NewTicker(w.pollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("webhook worker shutting down", "worker_id", w.workerID)
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
func (w *WebhookWorker) Done() <-chan struct{} {
	return w.done
}

// processJobs claims unclaimed webhook jobs, delivers them, and marks results.
func (w *WebhookWorker) processJobs(ctx context.Context) error {
	// Step 1: Claim jobs via CAS
	claimed, err := w.claimJobs(ctx)
	if err != nil {
		return fmt.Errorf("claim jobs: %w", err)
	}
	if len(claimed) == 0 {
		return nil
	}

	w.logger.Info("claimed webhook jobs", "count", len(claimed), "worker_id", w.workerID)

	// Step 2: Start heartbeat goroutine
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.heartbeatLoop(heartbeatCtx, claimed)
	}()

	// Step 3: Deliver each webhook
	results := make([]jobResult, 0, len(claimed))

	for _, job := range claimed {
		if ctx.Err() != nil {
			break
		}
		errMsg := w.deliverWebhook(ctx, job)
		results = append(results, jobResult{
			id:          job.ID,
			success:     errMsg == "",
			errMsg:      errMsg,
			attempts:    job.Attempts,
			maxAttempts: job.MaxAttempts,
		})
	}

	// Step 4: Stop heartbeats
	cancelHeartbeat()
	wg.Wait()

	// Step 5: Mark results via CAS
	if len(results) > 0 {
		if err := w.markResults(ctx, results); err != nil {
			return fmt.Errorf("mark results: %w", err)
		}
	}

	return nil
}

// claimJobs reads the queue and claims eligible webhook jobs via CAS.
// Jobs with NextAttemptAt in the future are skipped (backoff).
func (w *WebhookWorker) claimJobs(ctx context.Context) ([]Job, error) {
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := w.queue.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("read queue: %w", err)
		}

		now := time.Now()
		var claimed []Job
		modified := false

		for i := range snap.State.Jobs {
			if len(claimed) >= w.claimBatchSize {
				break
			}

			job := &snap.State.Jobs[i]

			// Skip jobs with future NextAttemptAt (backoff)
			if job.NextAttemptAt != nil && job.NextAttemptAt.After(now) {
				continue
			}

			switch job.Status {
			case StatusPending:
				job.Status = StatusClaimed
				job.ClaimedBy = w.workerID
				hb := now
				job.Heartbeat = &hb
				job.Attempts++
				claimed = append(claimed, *job)
				modified = true

			case StatusClaimed:
				// Reclaim timed-out jobs
				if job.Heartbeat != nil && now.Sub(*job.Heartbeat) > w.heartbeatTimeout {
					w.logger.Warn("reclaiming timed-out webhook job",
						"job_id", job.ID,
						"previous_worker", job.ClaimedBy,
						"worker_id", w.workerID)
					job.ClaimedBy = w.workerID
					hb := now
					job.Heartbeat = &hb
					job.Attempts++
					claimed = append(claimed, *job)
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

	return nil, nil
}

// deliverWebhook performs HMAC-signed HTTP POST delivery.
// Returns empty string on success, error message on failure.
func (w *WebhookWorker) deliverWebhook(ctx context.Context, job Job) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.URL, bytes.NewReader(job.Payload))
	if err != nil {
		return fmt.Sprintf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// HMAC-SHA256 signing
	if job.Secret != "" {
		signingKey := job.Secret
		if w.encryptor != nil {
			if decrypted, err := w.encryptor.Decrypt(job.Secret); err == nil {
				signingKey = decrypted
			}
			// If decryption fails, it may be a plaintext secret; use as-is
		}
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(job.Payload))
		mac := hmac.New(sha256.New, []byte(signingKey))
		mac.Write([]byte(sigBase))
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Slack-Signature", sig)
		req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Sprintf("http request: %v", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ""
	}
	return fmt.Sprintf("http status %d", resp.StatusCode)
}

// markResults marks jobs as completed, failed, or retryable via CAS.
// Also garbage-collects completed jobs older than GCRetention.
func (w *WebhookWorker) markResults(ctx context.Context, results []jobResult) error {
	resultMap := make(map[string]jobResult, len(results))
	for _, r := range results {
		resultMap[r.id] = r
	}

	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		snap, err := w.queue.Read(ctx)
		if err != nil {
			return err
		}

		now := time.Now()
		for i := range snap.State.Jobs {
			r, ok := resultMap[snap.State.Jobs[i].ID]
			if !ok {
				continue
			}

			if r.success {
				snap.State.Jobs[i].Status = StatusCompleted
				snap.State.Jobs[i].CompletedAt = &now
				snap.State.Jobs[i].LastError = ""
			} else if r.attempts >= r.maxAttempts {
				// Permanent failure
				snap.State.Jobs[i].Status = StatusFailed
				snap.State.Jobs[i].LastError = r.errMsg
				w.logger.Warn("webhook permanently failed",
					"job_id", r.id,
					"url", snap.State.Jobs[i].URL,
					"attempts", r.attempts,
					"error", r.errMsg)
			} else {
				// Schedule retry with exponential backoff: 2^attempts seconds
				backoff := time.Duration(math.Pow(2, float64(r.attempts))) * time.Second
				nextAttempt := now.Add(backoff)
				snap.State.Jobs[i].Status = StatusPending
				snap.State.Jobs[i].ClaimedBy = ""
				snap.State.Jobs[i].Heartbeat = nil
				snap.State.Jobs[i].LastError = r.errMsg
				snap.State.Jobs[i].NextAttemptAt = &nextAttempt
				w.logger.Info("scheduling webhook retry",
					"job_id", r.id,
					"attempt", r.attempts,
					"next_attempt_at", nextAttempt,
					"error", r.errMsg)
			}
		}

		// Garbage collect completed jobs older than GCRetention
		gcCutoff := now.Add(-GCRetention)
		originalCount := len(snap.State.Jobs)
		filtered := make([]Job, 0, len(snap.State.Jobs))
		for _, job := range snap.State.Jobs {
			if job.Status == StatusCompleted && job.CompletedAt != nil && job.CompletedAt.Before(gcCutoff) {
				continue
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

		gcRemoved := originalCount - len(filtered)
		delivered := 0
		retried := 0
		failed := 0
		for _, r := range results {
			if r.success {
				delivered++
			} else if r.attempts >= r.maxAttempts {
				failed++
			} else {
				retried++
			}
		}
		w.logger.Info("marked webhook results",
			"delivered", delivered, "retried", retried, "failed", failed, "gc_removed", gcRemoved)
		return nil
	}

	return fmt.Errorf("mark results failed after %d CAS retries", maxRetries)
}

// heartbeatLoop periodically updates heartbeats for claimed jobs.
func (w *WebhookWorker) heartbeatLoop(ctx context.Context, jobs []Job) {
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()

	jobSet := make(map[string]struct{}, len(jobs))
	for _, j := range jobs {
		jobSet[j.ID] = struct{}{}
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
func (w *WebhookWorker) sendHeartbeats(ctx context.Context, jobSet map[string]struct{}) error {
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

// jobResult holds the result of a single webhook delivery attempt.
type jobResult struct {
	id          string
	success     bool
	errMsg      string
	attempts    int
	maxAttempts int
}
