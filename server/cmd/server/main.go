package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/db"
	"github.com/johnsuh/teraslack/server/internal/handler"
)

func main() {
	var migrateOnly bool
	flag.BoolVar(&migrateOnly, "migrate-only", false, "run migrations and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	pool, err := migratePool(ctx, cfg, logger)
	defer pool.Close()
	if migrateOnly {
		return
	}

	pool.Close()
	pool, err = pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	httpHandler, err := handler.New(cfg, pool, logger)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpHandler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		logger.Info("shutting down server", "signal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}
}

func migratePool(ctx context.Context, cfg config.Config, logger *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.MigrationDatabaseURL)
	if err != nil {
		return nil, err
	}

	if err := db.Migrate(ctx, pool); err == nil {
		return pool, nil
	} else {
		pool.Close()
		if cfg.DatabaseURL == cfg.MigrationDatabaseURL || !isConnectionSlotExhaustion(err) {
			return nil, err
		}

		logger.Warn(
			"direct migration database exhausted connection slots; retrying migrations with pooled database url",
			"error", err.Error(),
		)
	}

	pool, err = pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func isConnectionSlotExhaustion(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()
	return strings.Contains(message, "SQLSTATE 53300") ||
		strings.Contains(message, "remaining connection slots")
}
