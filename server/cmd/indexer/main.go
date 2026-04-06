package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/queue"
	s3store "github.com/johnsuh/teraslack/server/internal/s3"
	"github.com/johnsuh/teraslack/server/internal/search"
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
	indexQueue := queue.NewManager(store, cfg.IndexQueueS3Key)
	producer := indexQueue.Producer()
	consumer := indexQueue.Consumer(cfg.IndexerWorkerID)
	for {
		if err := search.RunIndexerOnce(ctx, pool, producer, consumer, cfg.IndexerWorkerID); err != nil {
			log.Printf("indexer error: %v", err)
		}
		time.Sleep(5 * time.Second)
	}
}
