package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/turbopuffer/turbopuffer-go/option"

	"github.com/suhjohn/teraslack/internal/config"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/handler"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	s3client "github.com/suhjohn/teraslack/internal/s3"
	"github.com/suhjohn/teraslack/internal/search"
	"github.com/suhjohn/teraslack/internal/service"
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
	migrationsURL, err := migrationSourceURL()
	if err != nil {
		return fmt.Errorf("resolve migration source: %w", err)
	}
	m, err := migrate.New(migrationsURL, cfg.MigrationDatabaseURL)
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
	ctx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
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

	if cfg.EncryptionKey == "" {
		return fmt.Errorf("ENCRYPTION_KEY is required")
	}
	keyProvider, keyErr := crypto.NewEnvKeyProvider(cfg.EncryptionKey, nil)
	if keyErr != nil {
		return fmt.Errorf("init encryption: %w", keyErr)
	}
	encryptor := crypto.NewEncryptor(keyProvider)
	logger.Info("encryption enabled", "key_id", keyProvider.CurrentKeyID())

	// Initialize repositories
	workspaceRepo := pgRepo.NewWorkspaceRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	accountRepo := pgRepo.NewAccountRepo(pool)
	workspaceMembershipRepo := pgRepo.NewWorkspaceMembershipRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	convAccessRepo := pgRepo.NewConversationAccessRepo(pool)
	externalMemberRepo := pgRepo.NewExternalMemberRepo(pool)
	msgRepo := pgRepo.NewMessageRepo(pool)
	conversationReadRepo := pgRepo.NewConversationReadRepo(pool)
	fileRepo := pgRepo.NewFileRepo(pool)
	eventRepo := pgRepo.NewEventRepo(pool, encryptor)
	roleRepo := pgRepo.NewRoleAssignmentRepo(pool)
	authRepo := pgRepo.NewAuthRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	externalEventRepo := pgRepo.NewExternalEventRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	auditRepo := pgRepo.NewAuthorizationAuditRepo(pool)
	workspaceInviteRepo := pgRepo.NewWorkspaceInviteRepo(pool)

	// Initialize EventRecorder
	recorder := service.NewEventRecorder(eventStoreRepo)

	// Initialize services
	workspaceSvc := service.NewWorkspaceService(workspaceRepo, userRepo, recorder, pool, logger)
	workspaceSvc.SetAuthorizationAuditRepository(auditRepo)
	workspaceSvc.SetExternalMemberRepository(externalMemberRepo)
	workspaceSvc.SetIdentityRepositories(accountRepo, workspaceMembershipRepo)
	eventSvc := service.NewEventService(eventRepo, userRepo, recorder, pool, logger)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo, workspaceMembershipRepo)
	userSvc.SetAuthorizationAuditRepository(auditRepo)
	roleSvc := service.NewRoleService(roleRepo, userRepo)
	roleSvc.SetRecorder(recorder, pool, logger)
	roleSvc.SetIdentityRepositories(workspaceMembershipRepo)
	roleSvc.SetAuthorizationAuditRepository(auditRepo)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)
	convSvc.SetIdentityRepositories(workspaceMembershipRepo)
	convSvc.SetExternalMemberRepository(externalMemberRepo)
	convAccessSvc := service.NewConversationAccessService(convAccessRepo, convRepo, userRepo, roleRepo, recorder, pool, logger)
	convAccessSvc.SetIdentityRepositories(workspaceMembershipRepo)
	convAccessSvc.SetAuthorizationAuditRepository(auditRepo)
	convSvc.SetAccessService(convAccessSvc)
	msgSvc := service.NewMessageService(msgRepo, convRepo, recorder, pool, logger)
	msgSvc.SetAccessService(convAccessSvc)
	msgSvc.SetExternalMemberRepository(externalMemberRepo)
	externalEventSvc := service.NewExternalEventService(externalEventRepo)
	externalEventSvc.SetExternalMemberRepository(externalMemberRepo)
	conversationReadSvc := service.NewConversationReadService(conversationReadRepo, convRepo)
	fileSvc := service.NewFileService(fileRepo, s3, cfg.S3KeyPrefix, cfg.BaseURL, recorder, pool, logger)
	fileSvc.SetExternalMemberRepository(externalMemberRepo)
	authSvc := service.NewAuthService(authRepo, userRepo, workspaceRepo, workspaceInviteRepo, recorder, pool, logger, service.AuthConfig{
		BaseURL:                 cfg.BaseURL,
		FrontendURL:             cfg.FrontendURL,
		StateSecret:             cfg.AuthStateSecret,
		GitHubOAuthClientID:     cfg.GitHubOAuthClientID,
		GitHubOAuthClientSecret: cfg.GitHubOAuthClientSecret,
		GoogleOAuthClientID:     cfg.GoogleOAuthClientID,
		GoogleOAuthClientSecret: cfg.GoogleOAuthClientSecret,
		ResendAPIKey:            cfg.ResendAPIKey,
		AuthEmailFrom:           cfg.AuthEmailFrom,
	})
	authSvc.SetIdentityRepositories(accountRepo, workspaceMembershipRepo)
	if cfg.ResendAPIKey == "" || cfg.AuthEmailFrom == "" {
		logger.Warn("email sign-in is disabled", "has_resend_api_key", cfg.ResendAPIKey != "", "has_auth_email_from", cfg.AuthEmailFrom != "")
	}
	authSvc.SetAuthorizationAuditRepository(auditRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)
	apiKeySvc.SetIdentityRepositories(workspaceMembershipRepo)
	apiKeySvc.SetExternalMemberRepository(externalMemberRepo)
	apiKeySvc.SetAuthorizationAuditRepository(auditRepo)
	externalMemberSvc := service.NewExternalMemberService(externalMemberRepo, accountRepo, convRepo, workspaceRepo)
	externalMemberSvc.SetConversationAccessService(convAccessSvc)
	// Initialize TurbopufferClient (optional — nil means search is disabled)
	var tpClient service.TurbopufferClient
	if cfg.TurbopufferAPIKey != "" {
		nsPrefix := "teraslack"
		if v := os.Getenv("TURBOPUFFER_NS_PREFIX"); v != "" {
			nsPrefix = v
		}
		tpClient = search.NewClient(
			cfg.TurbopufferAPIKey,
			nsPrefix,
			option.WithRegion(cfg.TurbopufferRegion),
		)
		logger.Info("turbopuffer client initialized", "ns_prefix", nsPrefix, "region", cfg.TurbopufferRegion)
	}
	searchSvc := service.NewSearchService(tpClient)
	searchSvc.SetExternalMemberRepository(externalMemberRepo)
	workspaceInviteSvc := service.NewWorkspaceInviteService(workspaceInviteRepo, userRepo, recorder, pool, cfg.FrontendURL)
	workspaceInviteSvc.SetAuthorizationAuditRepository(auditRepo)
	workspaceInviteSvc.SetIdentityRepositories(accountRepo, workspaceMembershipRepo)
	// Initialize handlers
	workspaceHandler := handler.NewWorkspaceHandler(workspaceSvc)
	workspaceInviteHandler := handler.NewWorkspaceInviteHandler(workspaceInviteSvc)
	userHandler := handler.NewUserHandler(userSvc, roleSvc)
	convHandler := handler.NewConversationHandler(convSvc, convAccessSvc)
	externalMemberHandler := handler.NewExternalMemberHandler(externalMemberSvc)
	msgHandler := handler.NewMessageHandler(msgSvc)
	fileHandler := handler.NewFileHandler(fileSvc)
	externalEventHandler := handler.NewExternalEventHandler(externalEventSvc)
	eventHandler := handler.NewEventHandler(eventSvc)
	authHandler := handler.NewAuthHandler(authSvc)
	apiKeyHandler := handler.NewAPIKeyHandler(apiKeySvc)
	searchHandler := handler.NewSearchHandler(searchSvc)
	conversationReadHandler := handler.NewConversationReadHandler(conversationReadSvc)

	// Set up router
	router := handler.Router(
		logger,
		cfg.FrontendURL,
		cfg.CORSAllowedOrigins,
		authSvc,
		apiKeySvc,
		workspaceHandler,
		workspaceInviteHandler,
		userHandler,
		convHandler,
		msgHandler,
		fileHandler,
		externalEventHandler,
		externalMemberHandler,
		eventHandler,
		authHandler,
		searchHandler,
		apiKeyHandler,
		conversationReadHandler,
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

	// Cancel root context before shutting down the server.
	cancelWorkers()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func migrationSourceURL() (string, error) {
	candidates := []string{"internal/repository/migrations"}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "internal", "repository", "migrations"),
			filepath.Join(exeDir, "..", "internal", "repository", "migrations"),
		)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absPath, err := filepath.Abs(candidate)
		if err != nil {
			return "", fmt.Errorf("abs path for %q: %w", candidate, err)
		}
		info, err := os.Stat(absPath)
		if err == nil && info.IsDir() {
			return "file://" + filepath.ToSlash(absPath), nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %q: %w", absPath, err)
		}
	}

	return "", fmt.Errorf("internal/repository/migrations not found from cwd or executable path")
}
