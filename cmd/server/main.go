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

	"github.com/suhjohn/workspace/internal/config"
	"github.com/suhjohn/workspace/internal/handler"
	pgRepo "github.com/suhjohn/workspace/internal/repository/postgres"
	s3client "github.com/suhjohn/workspace/internal/s3"
	"github.com/suhjohn/workspace/internal/service"
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
		srcErr, dbErr := m.Close()
		_ = srcErr
		_ = dbErr
		return fmt.Errorf("run migrations: %w", err)
	}
	srcErr, dbErr := m.Close()
	if srcErr != nil {
		return fmt.Errorf("close migration source: %w", srcErr)
	}
	if dbErr != nil {
		return fmt.Errorf("close migration db: %w", dbErr)
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

	// Initialize S3 client (optional - file uploads work only if configured)
	var s3 *s3client.Client
	if cfg.S3Bucket != "" {
		s3, err = s3client.NewClient(ctx, s3client.Config{
			Bucket:    cfg.S3Bucket,
			Region:    cfg.S3Region,
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
		})
		if err != nil {
			logger.Warn("s3 client init failed, file uploads disabled", "error", err)
			s3 = nil
		} else {
			logger.Info("s3 client initialized", "bucket", cfg.S3Bucket)
		}
	}

	// Initialize repositories
	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	msgRepo := pgRepo.NewMessageRepo(pool)
	ugRepo := pgRepo.NewUsergroupRepo(pool)
	pinRepo := pgRepo.NewPinRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)
	fileRepo := pgRepo.NewFileRepo(pool)
	eventRepo := pgRepo.NewEventRepo(pool)
	authRepo := pgRepo.NewAuthRepo(pool)

	// Initialize services
	// EventService must be created first as it serves as the EventPublisher for all other services
	eventSvc := service.NewEventService(eventRepo, logger)
	userSvc := service.NewUserService(userRepo, eventSvc, logger)
	convSvc := service.NewConversationService(convRepo, userRepo, eventSvc, logger)
	msgSvc := service.NewMessageService(msgRepo, convRepo, eventSvc, logger)
	ugSvc := service.NewUsergroupService(ugRepo, userRepo, eventSvc, logger)
	pinSvc := service.NewPinService(pinRepo, convRepo, msgRepo, eventSvc, logger)
	bookmarkSvc := service.NewBookmarkService(bookmarkRepo, convRepo, eventSvc, logger)
	fileSvc := service.NewFileService(fileRepo, s3, cfg.BaseURL, eventSvc, logger)
	authSvc := service.NewAuthService(authRepo, userRepo, eventSvc, logger)
	searchSvc := service.NewSearchService(msgRepo, fileRepo, nil, nil) // ClickHouse/Turbopuffer optional

	// Initialize handlers
	userHandler := handler.NewUserHandler(userSvc)
	convHandler := handler.NewConversationHandler(convSvc)
	msgHandler := handler.NewMessageHandler(msgSvc)
	ugHandler := handler.NewUsergroupHandler(ugSvc)
	pinHandler := handler.NewPinHandler(pinSvc)
	bookmarkHandler := handler.NewBookmarkHandler(bookmarkSvc)
	fileHandler := handler.NewFileHandler(fileSvc)
	eventHandler := handler.NewEventHandler(eventSvc)
	authHandler := handler.NewAuthHandler(authSvc)
	searchHandler := handler.NewSearchHandler(searchSvc)

	// Set up router
	router := handler.Router(
		logger,
		authSvc,
		userHandler,
		convHandler,
		msgHandler,
		ugHandler,
		pinHandler,
		bookmarkHandler,
		fileHandler,
		eventHandler,
		authHandler,
		searchHandler,
	)

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
