package main

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/queue"
	s3store "github.com/johnsuh/teraslack/server/internal/s3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	store, err := s3store.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}

	server := queue.NewBrokerServer(map[string]*queue.Manager{
		queue.QueueIndex:     queue.NewManager(store, cfg.IndexQueueS3Key),
		queue.QueueProjector: queue.NewManager(store, cfg.ProjectorQueueS3Key),
		queue.QueueWebhook:   queue.NewManager(store, cfg.WebhookQueueS3Key),
	})

	address := ":" + strconv.Itoa(cfg.Port)
	log.Printf("queue broker listening on %s", address)
	if err := http.ListenAndServe(address, server); err != nil {
		log.Fatal(err)
	}
}
