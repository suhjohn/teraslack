package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/config"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/queue"
	s3store "github.com/johnsuh/teraslack/server/internal/s3"
	"github.com/johnsuh/teraslack/server/internal/webhooks"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	store, err := s3store.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	webhookQueue := queue.NewManager(store, cfg.WebhookQueueS3Key)
	consumer := webhookQueue.Consumer(cfg.WebhookWorkerID)
	protector, err := teracrypto.NewStringProtector(ctx, teracrypto.Options{
		EnvKey:         cfg.EncryptionKey,
		AWSKMSKeyID:    cfg.AWSKMSKeyID,
		AWSKMSRegion:   cfg.AWSKMSRegion,
		AWSKMSEndpoint: cfg.AWSKMSEndpoint,
	})
	if err != nil {
		log.Fatal(err)
	}
	for {
		if err := webhooks.ProcessDeliveriesOnce(ctx, pool, consumer, protector); err != nil {
			log.Printf("webhook worker error: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}
