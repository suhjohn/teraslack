// cmd/indexer is a standalone process that runs both the IndexProducer
// (tails service_events → S3 queue) and the IndexWorker (claims jobs from
// S3 queue → Turbopuffer). It runs separately from the API server so
// multiple server replicas don't contend over indexing.
//
// Usage:
//
//	DATABASE_URL=postgres://... S3_BUCKET=my-bucket WORKER_ID=idx-1 go run ./cmd/indexer
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/teraslack/internal/queue"
	"github.com/suhjohn/teraslack/internal/search"
	"github.com/suhjohn/teraslack/internal/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "indexer: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Database connection (needed by IndexProducer to tail service_events)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	// S3 queue configuration
	s3Cfg := queue.S3Config{
		Bucket:    os.Getenv("S3_BUCKET"),
		Region:    getEnv("S3_REGION", "us-east-1"),
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		QueueKey:  getEnv("INDEX_QUEUE_S3_KEY", "queues/index/queue.json"),
	}

	if s3Cfg.Bucket == "" {
		return fmt.Errorf("S3_BUCKET is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to database
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Create S3 queue
	s3Queue, err := queue.NewS3Queue(ctx, s3Cfg)
	if err != nil {
		return fmt.Errorf("create S3 queue: %w", err)
	}

	workerID := getEnv("WORKER_ID", "")

	// Start IndexProducer (tails service_events → S3 queue)
	producer := queue.NewIndexProducer(pool, s3Queue, logger, queue.ProducerConfig{})
	producerErrCh := make(chan error, 1)
	go func() {
		producerErrCh <- producer.Run(ctx)
	}()

	// Initialize TurbopufferClient (optional — nil means dry-run mode).
	var tpClient service.TurbopufferClient
	if apiKey := os.Getenv("TURBOPUFFER_API_KEY"); apiKey != "" {
		nsPrefix := getEnv("TURBOPUFFER_NS_PREFIX", "teraslack")
		tpClient = search.NewClient(apiKey, nsPrefix)
		logger.Info("turbopuffer client initialized", "ns_prefix", nsPrefix)
	} else {
		logger.Warn("TURBOPUFFER_API_KEY not set — indexer running in dry-run mode")
	}

	worker := queue.NewIndexWorker(s3Queue, tpClient, logger, queue.WorkerConfig{
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

	// Run worker in foreground; producer runs in background goroutine.
	// Both shut down when ctx is cancelled.
	workerErr := worker.Run(ctx)

	// Wait for producer to finish draining
	<-producer.Done()

	if producerErr := <-producerErrCh; producerErr != nil {
		logger.Error("producer error", "error", producerErr)
	}

	return workerErr
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
