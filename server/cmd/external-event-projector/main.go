package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/eventsourcing"
	"github.com/johnsuh/teraslack/server/internal/queue"
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
	producer := broker.Producer(queue.QueueProjector)
	consumer := broker.Consumer(queue.QueueProjector, cfg.ProjectorWorkerID)
	for {
		if err := eventsourcing.ProjectExternalEventsOnce(ctx, pool, producer, consumer, cfg.ProjectorWorkerID); err != nil {
			log.Printf("projector error: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}
