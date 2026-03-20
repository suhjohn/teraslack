package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// OutboxWorker polls the outbox table and delivers webhook payloads.
// It uses FOR UPDATE SKIP LOCKED so multiple instances can run safely.
type OutboxWorker struct {
	outboxRepo   repository.OutboxRepository
	httpClient   *http.Client
	logger       *slog.Logger
	pollInterval time.Duration
	batchSize    int
}

// NewOutboxWorker creates a new OutboxWorker.
func NewOutboxWorker(
	outboxRepo repository.OutboxRepository,
	logger *slog.Logger,
) *OutboxWorker {
	return &OutboxWorker{
		outboxRepo: outboxRepo,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:       logger,
		pollInterval: 5 * time.Second,
		batchSize:    50,
	}
}

// Run starts the outbox worker loop. It blocks until ctx is cancelled.
func (w *OutboxWorker) Run(ctx context.Context) {
	w.logger.Info("outbox worker started", "poll_interval", w.pollInterval, "batch_size", w.batchSize)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Also run periodic cleanup
	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("outbox worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		case <-cleanupTicker.C:
			w.cleanup(ctx)
		}
	}
}

func (w *OutboxWorker) processBatch(ctx context.Context) {
	entries, err := w.outboxRepo.ClaimBatch(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("claim outbox batch", "error", err)
		return
	}

	for _, entry := range entries {
		w.deliver(ctx, entry)
	}
}

func (w *OutboxWorker) deliver(ctx context.Context, entry domain.OutboxEntry) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, entry.URL, bytes.NewReader(entry.Payload))
	if err != nil {
		w.handleFailure(ctx, entry, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// HMAC-SHA256 signing
	if entry.Secret != "" {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(entry.Payload))
		mac := hmac.New(sha256.New, []byte(entry.Secret))
		mac.Write([]byte(sigBase))
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Slack-Signature", sig)
		req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.handleFailure(ctx, entry, fmt.Sprintf("http request: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := w.outboxRepo.MarkDelivered(ctx, entry.ID); err != nil {
			w.logger.Error("mark delivered", "error", err, "outbox_id", entry.ID)
		}
	} else {
		w.handleFailure(ctx, entry, fmt.Sprintf("http status %d", resp.StatusCode))
	}
}

func (w *OutboxWorker) handleFailure(ctx context.Context, entry domain.OutboxEntry, errMsg string) {
	// entry.Attempts was already incremented by ClaimBatch (UPDATE SET attempts = attempts + 1)
	if entry.Attempts >= entry.MaxAttempts {
		w.logger.Warn("outbox entry permanently failed",
			"outbox_id", entry.ID,
			"event_id", entry.EventID,
			"url", entry.URL,
			"attempts", entry.Attempts,
			"error", errMsg,
		)
		if err := w.outboxRepo.MarkFailed(ctx, entry.ID, errMsg); err != nil {
			w.logger.Error("mark failed", "error", err, "outbox_id", entry.ID)
		}
	} else {
		// Exponential backoff: 2^attempts seconds
		backoff := time.Duration(math.Pow(2, float64(entry.Attempts))) * time.Second
		nextAttemptAt := time.Now().Add(backoff)
		w.logger.Info("scheduling outbox retry",
			"outbox_id", entry.ID,
			"attempt", entry.Attempts,
			"next_attempt_at", nextAttemptAt,
			"error", errMsg,
		)
		if err := w.outboxRepo.ScheduleRetry(ctx, entry.ID, nextAttemptAt, errMsg); err != nil {
			w.logger.Error("schedule retry", "error", err, "outbox_id", entry.ID)
		}
	}
}

func (w *OutboxWorker) cleanup(ctx context.Context) {
	deleted, err := w.outboxRepo.CleanupDelivered(ctx, 7*24*time.Hour)
	if err != nil {
		w.logger.Error("cleanup delivered outbox", "error", err)
		return
	}
	if deleted > 0 {
		w.logger.Info("cleaned up delivered outbox entries", "count", deleted)
	}
}
