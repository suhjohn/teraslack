package eventsourcing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/eventsourcing"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

// setupTestDB spins up a real Postgres container, runs all 5 migrations, and
// returns a pgxpool.Pool connected to it.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "repository", "migrations")

	migrations := []string{
		"000001_init.up.sql",
		"000002_usergroups_pins_bookmarks_files_events_auth.up.sql",
		"000003_event_log.up.sql",
		"000004_token_hash_encrypted_secret.up.sql",
		"000005_service_events_outbox.up.sql",
		"000006_drop_token_unique.up.sql",
		"000007_api_keys_principal_type.up.sql",
		"000008_drop_outbox.up.sql",
	}

	migrationData := make([][]byte, len(migrations))
	for i, name := range migrations {
		data, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			t.Fatalf("read migration %d (%s): %v", i+1, name, err)
		}
		migrationData[i] = data
	}

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		tcpostgres.WithInitScripts(),
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

	for i, m := range migrationData {
		if _, err := pool.Exec(ctx, string(m)); err != nil {
			t.Fatalf("run migration %d: %v", i+1, err)
		}
	}

	return pool
}

func getMigrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "repository", "migrations")
}

func readMigration(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return data
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// ---------- Service-level event recording ----------

func TestServiceEventRecording_UserCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "alice",
		Email:  "alice@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Verify projection table.
	var projName string
	err = pool.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", user.ID).Scan(&projName)
	if err != nil {
		t.Fatalf("query projection: %v", err)
	}
	if projName != "alice" {
		t.Errorf("projection name = %q, want %q", projName, "alice")
	}

	// Verify service_events.
	var eventType string
	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_type, payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, user.ID,
	).Scan(&eventType, &payload)
	if err != nil {
		t.Fatalf("query service_events: %v", err)
	}
	if eventType != domain.EventUserCreated {
		t.Errorf("event_type = %q, want %q", eventType, domain.EventUserCreated)
	}

	var snapshot domain.User
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.ID == "" {
		t.Error("snapshot.ID is empty")
	}
	if snapshot.Name != "alice" {
		t.Errorf("snapshot.Name = %q, want %q", snapshot.Name, "alice")
	}
	if snapshot.TeamID != "T001" {
		t.Errorf("snapshot.TeamID = %q, want %q", snapshot.TeamID, "T001")
	}
}

func TestServiceEventRecording_UserUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "bob",
		Email:  "bob@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	newRealName := "Robert Smith"
	_, err = userSvc.Update(ctx, user.ID, domain.UpdateUserParams{
		RealName: &newRealName,
	})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}

	// Verify 2 events: created + updated.
	var count int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, user.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 2 {
		t.Errorf("event count = %d, want 2", count)
	}

	// Verify update event has full snapshot.
	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateUser, user.ID, domain.EventUserUpdated,
	).Scan(&payload)
	if err != nil {
		t.Fatalf("query update event: %v", err)
	}
	var snapshot domain.User
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.RealName != "Robert Smith" {
		t.Errorf("snapshot.RealName = %q, want %q", snapshot.RealName, "Robert Smith")
	}
}

// ---------- Replay Correctness ----------

func TestReplayCorrectness_Users(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	var userIDs []string
	for i := 0; i < 5; i++ {
		u, err := userSvc.Create(ctx, domain.CreateUserParams{
			TeamID: "T001",
			Name:   fmt.Sprintf("user%d", i),
			Email:  fmt.Sprintf("user%d@example.com", i),
		})
		if err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		userIDs = append(userIDs, u.ID)
	}

	newName := "updated-user2"
	_, err := userSvc.Update(ctx, userIDs[2], domain.UpdateUserParams{RealName: &newName})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}

	var eventCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1", domain.AggregateUser).Scan(&eventCount)
	if eventCount != 6 {
		t.Fatalf("expected 6 events, got %d", eventCount)
	}

	type userRow struct {
		ID   string
		Name string
	}
	getUsers := func() []userRow {
		rows, err := pool.Query(ctx, "SELECT id, name FROM users ORDER BY id")
		if err != nil {
			t.Fatalf("query users: %v", err)
		}
		defer rows.Close()
		var users []userRow
		for rows.Next() {
			var u userRow
			if err := rows.Scan(&u.ID, &u.Name); err != nil {
				t.Fatalf("scan user: %v", err)
			}
			users = append(users, u)
		}
		return users
	}

	originalUsers := getUsers()

	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAggregate(ctx, domain.AggregateUser); err != nil {
		t.Fatalf("rebuild users: %v", err)
	}

	rebuiltUsers := getUsers()
	if len(rebuiltUsers) != len(originalUsers) {
		t.Fatalf("rebuilt users count = %d, want %d", len(rebuiltUsers), len(originalUsers))
	}

	origMap := map[string]string{}
	for _, u := range originalUsers {
		origMap[u.ID] = u.Name
	}
	for _, u := range rebuiltUsers {
		if origMap[u.ID] != u.Name {
			t.Errorf("rebuilt user %s: name = %q, want %q", u.ID, u.Name, origMap[u.ID])
		}
	}
}

func TestReplayCorrectness_ConversationWithMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)

	creator, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "creator",
		Email:  "creator@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	member, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "member",
		Email:  "member@example.com",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	conv, err := convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "general",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: creator.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	if err := convSvc.Invite(ctx, conv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	var convName string
	err = pool.QueryRow(ctx, "SELECT name FROM conversations WHERE id = $1", conv.ID).Scan(&convName)
	if err != nil {
		t.Fatalf("query conversation: %v", err)
	}
	if convName != "general" {
		t.Errorf("conv name = %q, want %q", convName, "general")
	}

	var convEventCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregateConversation).Scan(&convEventCount)
	if convEventCount < 1 {
		t.Errorf("expected at least 1 conversation event, got %d", convEventCount)
	}

	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild all: %v", err)
	}

	var rebuiltName string
	err = pool.QueryRow(ctx, "SELECT name FROM conversations WHERE id = $1", conv.ID).Scan(&rebuiltName)
	if err != nil {
		t.Fatalf("query rebuilt conversation: %v", err)
	}
	if rebuiltName != "general" {
		t.Errorf("rebuilt conv name = %q, want %q", rebuiltName, "general")
	}
}

func TestReplayCorrectness_BookmarkCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)
	bookmarkSvc := service.NewBookmarkService(bookmarkRepo, convRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "alice",
		Email:  "alice@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	conv, err := convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "bookmarks-test",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	bm, err := bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: conv.ID,
		Title:     "Go Docs",
		Type:      "link",
		Link:      "https://go.dev",
		CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bookmark: %v", err)
	}

	newTitle := "Updated Go Docs"
	_, err = bookmarkSvc.Update(ctx, bm.ID, domain.UpdateBookmarkParams{
		Title:     &newTitle,
		UpdatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("update bookmark: %v", err)
	}

	var bmEventCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregateBookmark).Scan(&bmEventCount)
	if bmEventCount != 2 {
		t.Errorf("expected 2 bookmark events, got %d", bmEventCount)
	}

	var payload json.RawMessage
	pool.QueryRow(ctx,
		"SELECT payload FROM service_events WHERE aggregate_type = $1 AND event_type = $2 ORDER BY id DESC LIMIT 1",
		domain.AggregateBookmark, domain.EventBookmarkUpdated,
	).Scan(&payload)
	var bmSnapshot domain.Bookmark
	if err := json.Unmarshal(payload, &bmSnapshot); err != nil {
		t.Fatalf("unmarshal bookmark snapshot: %v", err)
	}
	if bmSnapshot.Title != "Updated Go Docs" {
		t.Errorf("bookmark snapshot title = %q, want %q", bmSnapshot.Title, "Updated Go Docs")
	}

	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild all: %v", err)
	}

	var rebuiltTitle string
	err = pool.QueryRow(ctx, "SELECT title FROM bookmarks WHERE id = $1", bm.ID).Scan(&rebuiltTitle)
	if err != nil {
		t.Fatalf("query rebuilt bookmark: %v", err)
	}
	if rebuiltTitle != "Updated Go Docs" {
		t.Errorf("rebuilt bookmark title = %q, want %q", rebuiltTitle, "Updated Go Docs")
	}
}

// ---------- Payload full snapshots ----------

func TestPayloadIsFullSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:      "T001",
		Name:        "snapshot-test",
		Email:       "snap@example.com",
		RealName:    "Snapshot User",
		DisplayName: "snapuser",
		IsBot:       true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateUser, user.ID, domain.EventUserCreated,
	).Scan(&payload)
	if err != nil {
		t.Fatalf("query event: %v", err)
	}

	var snapshot domain.User
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if snapshot.ID != user.ID {
		t.Errorf("ID = %q, want %q", snapshot.ID, user.ID)
	}
	if snapshot.TeamID != "T001" {
		t.Errorf("TeamID = %q, want %q", snapshot.TeamID, "T001")
	}
	if snapshot.Name != "snapshot-test" {
		t.Errorf("Name = %q, want %q", snapshot.Name, "snapshot-test")
	}
	if snapshot.Email != "snap@example.com" {
		t.Errorf("Email = %q, want %q", snapshot.Email, "snap@example.com")
	}
	if snapshot.RealName != "Snapshot User" {
		t.Errorf("RealName = %q, want %q", snapshot.RealName, "Snapshot User")
	}
	if snapshot.DisplayName != "snapuser" {
		t.Errorf("DisplayName = %q, want %q", snapshot.DisplayName, "snapuser")
	}
	if !snapshot.IsBot {
		t.Error("IsBot should be true")
	}
	if snapshot.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// ---------- Delete events ----------

func TestDeleteEventIsRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)
	bookmarkSvc := service.NewBookmarkService(bookmarkRepo, convRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "deleter",
		Email:  "deleter@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	conv, err := convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "delete-test",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	bm, err := bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: conv.ID,
		Title:     "To Be Deleted",
		Type:      "link",
		Link:      "https://example.com/delete",
		CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bookmark: %v", err)
	}

	if err := bookmarkSvc.Delete(ctx, bm.ID); err != nil {
		t.Fatalf("delete bookmark: %v", err)
	}

	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT payload FROM service_events WHERE aggregate_type = $1 AND event_type = $2 AND aggregate_id = $3",
		domain.AggregateBookmark, domain.EventBookmarkDeleted, bm.ID,
	).Scan(&payload)
	if err != nil {
		t.Fatalf("query delete event: %v", err)
	}

	var deletePayload map[string]string
	if err := json.Unmarshal(payload, &deletePayload); err != nil {
		t.Fatalf("unmarshal delete payload: %v", err)
	}
	if deletePayload["bookmark_id"] != bm.ID {
		t.Errorf("delete payload bookmark_id = %q, want %q", deletePayload["bookmark_id"], bm.ID)
	}
}

// ---------- Event count consistency ----------

func TestEventCountConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	u, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "counter",
		Email:  "counter@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	n1 := "updated1"
	_, err = userSvc.Update(ctx, u.ID, domain.UpdateUserParams{RealName: &n1})
	if err != nil {
		t.Fatalf("update 1: %v", err)
	}

	n2 := "updated2"
	_, err = userSvc.Update(ctx, u.ID, domain.UpdateUserParams{RealName: &n2})
	if err != nil {
		t.Fatalf("update 2: %v", err)
	}

	var count int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, u.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 3 {
		t.Errorf("event count = %d, want 3 (1 created + 2 updated)", count)
	}

	rows, err := pool.Query(ctx,
		"SELECT id FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id ASC",
		domain.AggregateUser, u.ID,
	)
	if err != nil {
		t.Fatalf("query IDs: %v", err)
	}
	defer rows.Close()

	var prevID int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		if id <= prevID {
			t.Errorf("id %d not strictly greater than %d", id, prevID)
		}
		prevID = id
	}
}
