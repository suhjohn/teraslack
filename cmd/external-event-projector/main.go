// cmd/external-event-projector is a standalone process that tails
// internal_events and maintains the external_events public event stream.
// It runs separately from the API server so horizontally scaled API replicas
// do not all compete to project the same events.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/external-event-projector
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "external-event-projector: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	projector := service.NewExternalEventProjector(
		pool,
		pgRepo.NewEventStoreRepo(pool),
		pgRepo.NewExternalEventRepo(pool),
		pgRepo.NewProjectorCheckpointRepo(pool),
		logger,
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	logger.Info("external event projector starting")
	done := make(chan struct{})
	go func() {
		projector.Start(ctx)
		close(done)
	}()

	select {
	case <-ctx.Done():
		<-done
		return nil
	case <-done:
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("projector stopped unexpectedly")
	}
}
