package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/eventsourcing"
	"github.com/johnsuh/teraslack/server/internal/queue"
	s3store "github.com/johnsuh/teraslack/server/internal/s3"
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
	projectorQueue := queue.NewManager(store, cfg.ProjectorQueueS3Key)
	producer := projectorQueue.Producer()
	consumer := projectorQueue.Consumer(cfg.ProjectorWorkerID)
	for {
		if err := eventsourcing.ProjectExternalEventsOnce(ctx, pool, producer, consumer, cfg.ProjectorWorkerID); err != nil {
			log.Printf("projector error: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}
