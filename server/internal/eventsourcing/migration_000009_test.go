package eventsourcing_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupMigrationHarness(t *testing.T) (*pgxpool.Pool, *migrate.Migrate) {
	t.Helper()

	ctx := context.Background()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "repository", "migrations")

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	migrator, err := migrate.New("file://"+migrationsDir, connStr)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() {
		srcErr, dbErr := migrator.Close()
		if srcErr != nil {
			t.Logf("close migration source: %v", srcErr)
		}
		if dbErr != nil {
			t.Logf("close migration db: %v", dbErr)
		}
	})

	return pool, migrator
}

func TestMigration000009DropsPreviousExternalAccessTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, migrator := setupMigrationHarness(t)
	ctx := context.Background()

	if err := migrator.Steps(8); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("apply migrations through 000008: %v", err)
	}

	var accessTableBefore string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(to_regclass('public.external_principal_access')::text, '')").Scan(&accessTableBefore); err != nil {
		t.Fatalf("query external_principal_access before 000009: %v", err)
	}
	if accessTableBefore == "" {
		t.Fatal("expected external_principal_access to exist before 000009")
	}

	var assignmentTableBefore string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(to_regclass('public.external_principal_conversation_assignments')::text, '')").Scan(&assignmentTableBefore); err != nil {
		t.Fatalf("query external_principal_conversation_assignments before 000009: %v", err)
	}
	if assignmentTableBefore == "" {
		t.Fatal("expected external_principal_conversation_assignments to exist before 000009")
	}

	if err := migrator.Steps(1); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("apply migration 000009: %v", err)
	}

	var accessTableAfter string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(to_regclass('public.external_principal_access')::text, '')").Scan(&accessTableAfter); err != nil {
		t.Fatalf("query external_principal_access after 000009: %v", err)
	}
	if accessTableAfter != "" {
		t.Fatalf("expected external_principal_access to be dropped, got %q", accessTableAfter)
	}

	var assignmentTableAfter string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(to_regclass('public.external_principal_conversation_assignments')::text, '')").Scan(&assignmentTableAfter); err != nil {
		t.Fatalf("query external_principal_conversation_assignments after 000009: %v", err)
	}
	if assignmentTableAfter != "" {
		t.Fatalf("expected external_principal_conversation_assignments to be dropped, got %q", assignmentTableAfter)
	}

	var externalMembersTable string
	if err := pool.QueryRow(ctx, "SELECT COALESCE(to_regclass('public.external_members')::text, '')").Scan(&externalMembersTable); err != nil {
		t.Fatalf("query external_members after 000009: %v", err)
	}
	if externalMembersTable == "" {
		t.Fatal("expected external_members to remain after 000009")
	}
}
