// cmd/webhook-producer is a standalone process that tails service_events,
// looks up matching webhook subscriptions, and fans out delivery jobs to an
// S3-backed queue. It runs separately from both the API server and the
// webhook worker.
//
// Usage:
//
//	DATABASE_URL=postgres://... S3_BUCKET=my-bucket go run ./cmd/webhook-producer
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/queue"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "webhook-producer: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Database connection (needed to tail service_events and query subscriptions)
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
		QueueKey:  getEnv("WEBHOOK_QUEUE_S3_KEY", "queues/webhooks/queue.json"),
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

	producer := queue.NewWebhookProducer(pool, s3Queue, logger, queue.WebhookProducerConfig{})

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	logger.Info("webhook producer starting",
		"bucket", s3Cfg.Bucket,
		"queue_key", s3Cfg.QueueKey)

	return producer.Run(ctx)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
