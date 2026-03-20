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

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/eventsourcing"
	pgRepo "github.com/suhjohn/workspace/internal/repository/postgres"
)

// setupTestDB spins up a real Postgres container, runs migrations, and returns
// a pgxpool.Pool connected to it. The container is terminated when the test
// finishes.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	// Resolve the migration directory relative to this file.
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "repository", "migrations")

	// Read migration files.
	migration1, err := os.ReadFile(filepath.Join(migrationsDir, "000001_init.up.sql"))
	if err != nil {
		t.Fatalf("read migration 1: %v", err)
	}
	migration2, err := os.ReadFile(filepath.Join(migrationsDir, "000002_usergroups_pins_bookmarks_files_events_auth.up.sql"))
	if err != nil {
		t.Fatalf("read migration 2: %v", err)
	}
	migration3, err := os.ReadFile(filepath.Join(migrationsDir, "000003_event_log.up.sql"))
	if err != nil {
		t.Fatalf("read migration 3: %v", err)
	}

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		tcpostgres.WithInitScripts(), // no init scripts, we'll run migrations manually
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

	// Run migrations.
	for i, m := range [][]byte{migration1, migration2, migration3} {
		if _, err := pool.Exec(ctx, string(m)); err != nil {
			t.Fatalf("run migration %d: %v", i+1, err)
		}
	}

	return pool
}

// ---------- Test: Transactional Atomicity ----------
// Verifies that creating a user writes BOTH the projection row AND the event_log
// entry in the same transaction — you never get one without the other.

func TestTransactionalAtomicity_UserCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	userRepo := pgRepo.NewUserRepo(pool)

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "alice",
		Email:  "alice@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 1. Verify projection table has the user.
	var projName string
	err = pool.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", user.ID).Scan(&projName)
	if err != nil {
		t.Fatalf("query projection: %v", err)
	}
	if projName != "alice" {
		t.Errorf("projection name = %q, want %q", projName, "alice")
	}

	// 2. Verify event_log has the event.
	var eventType string
	var eventData json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_type, event_data FROM event_log WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, user.ID,
	).Scan(&eventType, &eventData)
	if err != nil {
		t.Fatalf("query event_log: %v", err)
	}
	if eventType != domain.EventUserCreated {
		t.Errorf("event_type = %q, want %q", eventType, domain.EventUserCreated)
	}

	// 3. Verify the event_data contains a full snapshot (not partial params).
	var snapshot domain.User
	if err := json.Unmarshal(eventData, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.ID == "" {
		t.Error("snapshot.ID is empty — event_data should contain full entity")
	}
	if snapshot.Name != "alice" {
		t.Errorf("snapshot.Name = %q, want %q", snapshot.Name, "alice")
	}
	if snapshot.TeamID != "T001" {
		t.Errorf("snapshot.TeamID = %q, want %q", snapshot.TeamID, "T001")
	}
}

func TestTransactionalAtomicity_UserUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	userRepo := pgRepo.NewUserRepo(pool)

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "bob",
		Email:  "bob@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	newRealName := "Robert Smith"
	updated, err := userRepo.Update(ctx, user.ID, domain.UpdateUserParams{
		RealName: &newRealName,
	})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}

	// Verify projection has the updated name.
	var projName string
	err = pool.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", updated.ID).Scan(&projName)
	if err != nil {
		t.Fatalf("query projection: %v", err)
	}
	if projName != "bob" {
		t.Errorf("projection name = %q, want %q", projName, "bob")
	}

	// Verify there are 2 events: created + updated.
	var count int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM event_log WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, user.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 2 {
		t.Errorf("event count = %d, want 2", count)
	}

	// Verify the update event has full snapshot with updated name.
	var eventData json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_data FROM event_log WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateUser, user.ID, domain.EventUserUpdated,
	).Scan(&eventData)
	if err != nil {
		t.Fatalf("query update event: %v", err)
	}
	var snapshot domain.User
	if err := json.Unmarshal(eventData, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.RealName != "Robert Smith" {
		t.Errorf("snapshot.RealName = %q, want %q", snapshot.RealName, "Robert Smith")
	}
}

// ---------- Test: Replay Correctness ----------
// Verifies that the Projector can rebuild projection tables from events alone.

func TestReplayCorrectness_Users(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	userRepo := pgRepo.NewUserRepo(pool)

	// Create several users.
	var userIDs []string
	for i := 0; i < 5; i++ {
		u, err := userRepo.Create(ctx, domain.CreateUserParams{
			TeamID: "T001",
			Name:   fmt.Sprintf("user%d", i),
			Email:  fmt.Sprintf("user%d@example.com", i),
		})
		if err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		userIDs = append(userIDs, u.ID)
	}

	// Update the third user.
	newName := "updated-user2"
	_, err := userRepo.Update(ctx, userIDs[2], domain.UpdateUserParams{RealName: &newName})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}

	// Verify we have 6 events total (5 created + 1 updated).
	var eventCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM event_log WHERE aggregate_type = $1", domain.AggregateUser).Scan(&eventCount)
	if eventCount != 6 {
		t.Fatalf("expected 6 events, got %d", eventCount)
	}

	// Record the projection state before rebuild.
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

	// Now rebuild the users projection from the event log.
	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAggregate(ctx, domain.AggregateUser); err != nil {
		t.Fatalf("rebuild users: %v", err)
	}

	// Verify the rebuilt projection matches the original.
	rebuiltUsers := getUsers()

	if len(rebuiltUsers) != len(originalUsers) {
		t.Fatalf("rebuilt users count = %d, want %d", len(rebuiltUsers), len(originalUsers))
	}

	// Build maps for comparison.
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
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)

	// Create a user (needed for FK constraint).
	creator, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "creator",
		Email:  "creator@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	member, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "member",
		Email:  "member@example.com",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	// Create a conversation.
	conv, err := convRepo.Create(ctx, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "general",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: creator.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	// Add a member.
	if err := convRepo.AddMember(ctx, conv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Verify projection state.
	var convName string
	err = pool.QueryRow(ctx, "SELECT name FROM conversations WHERE id = $1", conv.ID).Scan(&convName)
	if err != nil {
		t.Fatalf("query conversation: %v", err)
	}
	if convName != "general" {
		t.Errorf("conv name = %q, want %q", convName, "general")
	}

	// Verify events exist.
	var convEventCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM event_log WHERE aggregate_type = $1",
		domain.AggregateConversation).Scan(&convEventCount)
	if convEventCount < 1 {
		t.Errorf("expected at least 1 conversation event, got %d", convEventCount)
	}

	// Rebuild all projections (users must be rebuilt before conversations due to FK).
	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild all: %v", err)
	}

	// Verify the rebuilt conversation still exists with correct name.
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
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)

	// Set up prerequisites.
	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "alice",
		Email:  "alice@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	conv, err := convRepo.Create(ctx, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "bookmarks-test",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	// Create a bookmark.
	bm, err := bookmarkRepo.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: conv.ID,
		Title:     "Go Docs",
		Type:      "link",
		Link:      "https://go.dev",
		CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bookmark: %v", err)
	}

	// Update the bookmark.
	newTitle := "Updated Go Docs"
	_, err = bookmarkRepo.Update(ctx, bm.ID, domain.UpdateBookmarkParams{
		Title:     &newTitle,
		UpdatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("update bookmark: %v", err)
	}

	// Verify 2 bookmark events exist (created + updated).
	var bmEventCount int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM event_log WHERE aggregate_type = $1",
		domain.AggregateBookmark).Scan(&bmEventCount)
	if bmEventCount != 2 {
		t.Errorf("expected 2 bookmark events, got %d", bmEventCount)
	}

	// Verify the update event has the updated title in its snapshot.
	var eventData json.RawMessage
	pool.QueryRow(ctx,
		"SELECT event_data FROM event_log WHERE aggregate_type = $1 AND event_type = $2 ORDER BY sequence_id DESC LIMIT 1",
		domain.AggregateBookmark, domain.EventBookmarkUpdated,
	).Scan(&eventData)
	var bmSnapshot domain.Bookmark
	if err := json.Unmarshal(eventData, &bmSnapshot); err != nil {
		t.Fatalf("unmarshal bookmark snapshot: %v", err)
	}
	if bmSnapshot.Title != "Updated Go Docs" {
		t.Errorf("bookmark snapshot title = %q, want %q", bmSnapshot.Title, "Updated Go Docs")
	}

	// Rebuild all projections (respects FK ordering).
	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild all: %v", err)
	}

	// Verify rebuilt bookmark has the updated title.
	var rebuiltTitle string
	err = pool.QueryRow(ctx, "SELECT title FROM bookmarks WHERE id = $1", bm.ID).Scan(&rebuiltTitle)
	if err != nil {
		t.Fatalf("query rebuilt bookmark: %v", err)
	}
	if rebuiltTitle != "Updated Go Docs" {
		t.Errorf("rebuilt bookmark title = %q, want %q", rebuiltTitle, "Updated Go Docs")
	}
}

// ---------- Test: Event log stores full snapshots ----------

func TestEventDataIsFullSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	userRepo := pgRepo.NewUserRepo(pool)

	// Create a user with specific fields.
	user, err := userRepo.Create(ctx, domain.CreateUserParams{
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

	// Read the event_data and verify it contains ALL fields.
	var eventData json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_data FROM event_log WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateUser, user.ID, domain.EventUserCreated,
	).Scan(&eventData)
	if err != nil {
		t.Fatalf("query event: %v", err)
	}

	var snapshot domain.User
	if err := json.Unmarshal(eventData, &snapshot); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify ALL fields are present in the snapshot.
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

// ---------- Test: Delete events store pre-deletion snapshot ----------

func TestDeleteEventStoresSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)

	// Set up prerequisites.
	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "deleter",
		Email:  "deleter@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	conv, err := convRepo.Create(ctx, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "delete-test",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	bm, err := bookmarkRepo.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: conv.ID,
		Title:     "To Be Deleted",
		Type:      "link",
		Link:      "https://example.com/delete",
		CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bookmark: %v", err)
	}

	// Delete the bookmark.
	if err := bookmarkRepo.Delete(ctx, bm.ID); err != nil {
		t.Fatalf("delete bookmark: %v", err)
	}

	// Verify the delete event contains the pre-deletion snapshot.
	var eventData json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_data FROM event_log WHERE aggregate_type = $1 AND event_type = $2 AND aggregate_id = $3",
		domain.AggregateBookmark, domain.EventBookmarkDeleted, bm.ID,
	).Scan(&eventData)
	if err != nil {
		t.Fatalf("query delete event: %v", err)
	}

	var snapshot domain.Bookmark
	if err := json.Unmarshal(eventData, &snapshot); err != nil {
		t.Fatalf("unmarshal delete snapshot: %v", err)
	}
	if snapshot.ID != bm.ID {
		t.Errorf("delete snapshot ID = %q, want %q", snapshot.ID, bm.ID)
	}
	if snapshot.Title != "To Be Deleted" {
		t.Errorf("delete snapshot Title = %q, want %q", snapshot.Title, "To Be Deleted")
	}
}

// ---------- Test: Event count consistency ----------

func TestEventCountConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	userRepo := pgRepo.NewUserRepo(pool)

	// Perform a known sequence of operations.
	u, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "counter",
		Email:  "counter@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	n1 := "updated1"
	_, err = userRepo.Update(ctx, u.ID, domain.UpdateUserParams{RealName: &n1})
	if err != nil {
		t.Fatalf("update 1: %v", err)
	}

	n2 := "updated2"
	_, err = userRepo.Update(ctx, u.ID, domain.UpdateUserParams{RealName: &n2})
	if err != nil {
		t.Fatalf("update 2: %v", err)
	}

	// We should have exactly 3 events: 1 created + 2 updated.
	var count int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM event_log WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, u.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 3 {
		t.Errorf("event count = %d, want 3 (1 created + 2 updated)", count)
	}

	// Verify sequence IDs are strictly increasing.
	rows, err := pool.Query(ctx,
		"SELECT sequence_id FROM event_log WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY sequence_id ASC",
		domain.AggregateUser, u.ID,
	)
	if err != nil {
		t.Fatalf("query sequence IDs: %v", err)
	}
	defer rows.Close()

	var prevSeq int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan seq: %v", err)
		}
		if seq <= prevSeq {
			t.Errorf("sequence_id %d not strictly greater than %d", seq, prevSeq)
		}
		prevSeq = seq
	}
}
