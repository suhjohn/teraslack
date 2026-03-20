package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suhjohn/agentslack/internal/config"
	"github.com/suhjohn/agentslack/internal/handler"
	pgRepo "github.com/suhjohn/agentslack/internal/repository/postgres"
	"github.com/suhjohn/agentslack/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Run migrations
	logger.Info("running migrations")
	m, err := migrate.New("file://internal/repository/migrations", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	logger.Info("migrations complete")

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	logger.Info("connected to database")

	// Initialize repositories
	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	msgRepo := pgRepo.NewMessageRepo(pool)

	// Initialize services
	userSvc := service.NewUserService(userRepo)
	convSvc := service.NewConversationService(convRepo, userRepo)
	msgSvc := service.NewMessageService(msgRepo, convRepo)

	// Initialize handlers
	userHandler := handler.NewUserHandler(userSvc)
	convHandler := handler.NewConversationHandler(convSvc)
	msgHandler := handler.NewMessageHandler(msgSvc)

	// Set up router
	router := handler.Router(logger, userHandler, convHandler, msgHandler)

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "port", cfg.Port)
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", "signal", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
