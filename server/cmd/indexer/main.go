package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/queue"
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
	broker := queue.NewBrokerClient(cfg.QueueBrokerURL)
	if !broker.Configured() {
		log.Fatal("QUEUE_BROKER_URL is required")
	}
	producer := broker.Producer(queue.QueueIndex)
	consumer := broker.Consumer(queue.QueueIndex, cfg.SearchIndexerID)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	runtime := search.NewRuntime(cfg, pool, logger)

	for {
		if err := search.IndexOnce(ctx, pool, producer, consumer, cfg.SearchIndexerID, runtime); err != nil {
			log.Printf("search indexer error: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}
