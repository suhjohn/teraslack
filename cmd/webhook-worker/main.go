// cmd/webhook-worker is a standalone process that claims webhook delivery
// jobs from an S3-backed queue and delivers them via HMAC-signed HTTP POST.
// It handles retries with exponential backoff and garbage-collects completed
// jobs after 7 days.
//
// Usage:
//
//	WORKER_ID=wh-1 S3_BUCKET=my-bucket go run ./cmd/webhook-worker
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/queue"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "webhook-worker: %v\n", err)
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
		QueueKey:  getEnv("WEBHOOK_QUEUE_S3_KEY", "queues/webhooks/queue.json"),
	}

	if s3Cfg.Bucket == "" {
		return fmt.Errorf("S3_BUCKET is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create S3 queue
	s3Queue, err := queue.NewS3Queue(ctx, s3Cfg)
	if err != nil {
		return fmt.Errorf("create S3 queue: %w", err)
	}

	// Initialize encryption for HMAC secret decryption (optional)
	var encryptor *crypto.Encryptor
	if encKey := os.Getenv("ENCRYPTION_KEY"); encKey != "" {
		keyProvider, keyErr := crypto.NewEnvKeyProvider(encKey, nil)
		if keyErr != nil {
			return fmt.Errorf("init encryption: %w", keyErr)
		}
		encryptor = crypto.NewEncryptor(keyProvider)
		logger.Info("encryption enabled for webhook secret decryption")
	}

	workerID := getEnv("WORKER_ID", "")

	worker := queue.NewWebhookWorker(s3Queue, encryptor, logger, queue.WebhookWorkerConfig{
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

	logger.Info("webhook worker starting",
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
