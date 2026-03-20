// cmd/indexer is a standalone process that claims index jobs from the S3 queue
// and flushes them to Turbopuffer. It runs separately from the API server so
// multiple server replicas don't contend over indexing.
//
// Usage:
//
//	WORKER_ID=worker-1 S3_BUCKET=my-bucket go run ./cmd/indexer
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/suhjohn/workspace/internal/queue"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "indexer: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// S3 queue configuration
	s3Cfg := queue.S3Config{
		Bucket:    os.Getenv("S3_BUCKET"),
		Region:    getEnv("S3_REGION", "us-east-1"),
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		QueueKey:  getEnv("QUEUE_S3_KEY", "queue/index-queue.json"),
	}

	if s3Cfg.Bucket == "" {
		return fmt.Errorf("S3_BUCKET is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s3Queue, err := queue.NewS3Queue(ctx, s3Cfg)
	if err != nil {
		return fmt.Errorf("create S3 queue: %w", err)
	}

	workerID := getEnv("WORKER_ID", "")

	// TurbopufferClient is nil for now — pass a real implementation when configured.
	// The worker will still claim and mark jobs as completed (dry-run mode).
	worker := queue.NewIndexWorker(s3Queue, nil, logger, queue.WorkerConfig{
		WorkerID: workerID,
	})

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	logger.Info("indexer starting",
		"bucket", s3Cfg.Bucket,
		"queue_key", s3Cfg.QueueKey,
		"worker_id", workerID)

	return worker.Run(ctx)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
