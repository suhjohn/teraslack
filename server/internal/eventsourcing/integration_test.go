package eventsourcing_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/eventsourcing"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

// setupTestDB spins up a real Postgres container, runs all migrations, and
// returns a pgxpool.Pool connected to it.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "repository", "migrations")

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

	m, err := migrate.New("file://"+migrationsDir, connStr)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			t.Logf("close migration source: %v", srcErr)
		}
		if dbErr != nil {
			t.Logf("close migration db: %v", dbErr)
		}
	})
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("run migrations: %v", err)
	}

	return pool
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
		WorkspaceID:   "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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

	// Verify internal_events.
	var eventType string
	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_type, payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, user.ID,
	).Scan(&eventType, &payload)
	if err != nil {
		t.Fatalf("query internal_events: %v", err)
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
	if snapshot.WorkspaceID != "T001" {
		t.Errorf("snapshot.WorkspaceID = %q, want %q", snapshot.WorkspaceID, "T001")
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
		WorkspaceID:   "T001",
		Name:          "bob",
		Email:         "bob@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
		"SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
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
		"SELECT payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
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
			WorkspaceID:   "T001",
			Name:          fmt.Sprintf("user%d", i),
			Email:         fmt.Sprintf("user%d@example.com", i),
			PrincipalType: domain.PrincipalTypeHuman,
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
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1", domain.AggregateUser).Scan(&eventCount)
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

	workspaceRepo := pgRepo.NewWorkspaceRepo(pool)
	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)

	workspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name: "conversation-with-members",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	creator, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspace.ID,
		Name:          "creator",
		Email:         "creator@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	member, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspace.ID,
		Name:          "member",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	conv, err := convSvc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: workspace.ID,
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		Name:        "general",
		Type:        domain.ConversationTypePublicChannel,
		CreatorID:   creator.ID,
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
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1",
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

func TestReplayCorrectness_ConversationOwnerModelV2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	workspaceRepo := pgRepo.NewWorkspaceRepo(pool)
	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)

	workspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name: "owner-model",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	owner, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspace.ID,
		Name:          "owner",
		Email:         "owner@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	member, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspace.ID,
		Name:          "member",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	var workspaceMemberAccountID string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(account_id, '')
		FROM users
		WHERE id = $1
	`, member.ID).Scan(&workspaceMemberAccountID); err != nil {
		t.Fatalf("load member account id: %v", err)
	}
	if workspaceMemberAccountID == "" {
		t.Fatal("member account id is empty")
	}
	member.AccountID = workspaceMemberAccountID

	var membershipID string
	err = pool.QueryRow(ctx, `
		SELECT id
		FROM workspace_memberships
		WHERE workspace_id = $1 AND account_id = $2
	`, workspace.ID, workspaceMemberAccountID).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		membershipID = "WM_" + member.ID
		if _, err := pool.Exec(ctx, `
			INSERT INTO workspace_memberships (
				id, workspace_id, account_id, role, status, membership_kind, guest_scope,
				created_by_account_id, updated_by_account_id, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, NOW(), NOW())
		`, membershipID, workspace.ID, workspaceMemberAccountID, "member", "active", "full", "workspace_full", owner.AccountID); err != nil {
			t.Fatalf("insert workspace membership: %v", err)
		}
	} else if err != nil {
		t.Fatalf("load workspace membership: %v", err)
	}

	convID := "C_owner_model"
	createdAt := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	conv := domain.Conversation{
		ID:               convID,
		WorkspaceID:      workspace.ID,
		OwnerType:        domain.ConversationOwnerTypeWorkspace,
		OwnerWorkspaceID: workspace.ID,
		Name:             "general",
		Type:             domain.ConversationTypePublicChannel,
		CreatorID:        owner.ID,
		Topic:            domain.TopicPurpose{Value: "topic", Creator: owner.ID},
		Purpose:          domain.TopicPurpose{Value: "purpose", Creator: owner.ID},
		NumMembers:       1,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
	convWithMember := conv
	convWithMember.NumMembers = 2

	appendEvent := func(eventType string, aggregateType string, aggregateID string, workspaceID string, actorID string, payload any) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", eventType, err)
		}
		if _, err := eventStoreRepo.Append(ctx, domain.InternalEvent{
			EventType:     eventType,
			AggregateType: aggregateType,
			AggregateID:   aggregateID,
			WorkspaceID:   workspaceID,
			ActorID:       actorID,
			Payload:       body,
			CreatedAt:     createdAt,
		}); err != nil {
			t.Fatalf("append %s: %v", eventType, err)
		}
	}

	appendEvent(domain.EventConversationCreated, domain.AggregateConversation, convID, workspace.ID, owner.ID, conv)
	appendEvent(domain.EventMemberJoined, domain.AggregateConversation, convID, workspace.ID, owner.ID, struct {
		UserID       string               `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{
		UserID:       member.ID,
		Conversation: &convWithMember,
	})
	appendEvent(domain.EventConversationManagerAdded, domain.AggregateConversation, convID, workspace.ID, owner.ID, map[string]string{
		"conversation_id": convID,
		"user_id":         member.ID,
	})
	appendEvent(domain.EventConversationPostingPolicyUpdated, domain.AggregateConversation, convID, workspace.ID, owner.ID, domain.ConversationPostingPolicy{
		ConversationID:    convID,
		PolicyType:        domain.ConversationPostingPolicyCustom,
		AllowedAccountIDs: []string{member.AccountID},
		UpdatedBy:         owner.ID,
		UpdatedAt:         createdAt,
	})
	appendEvent(domain.EventMessagePosted, domain.AggregateMessage, "M_owner_model", workspace.ID, member.ID, domain.Message{
		TS:        "1700000000.000001",
		ChannelID: convID,
		UserID:    member.ID,
		Text:      "hello",
		Type:      "message",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})

	projector := eventsourcing.NewProjector(pool, logger)
	if err := projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild all: %v", err)
	}

	var memberAccountID string
	if err := pool.QueryRow(ctx, `
		SELECT account_id
		FROM conversation_members_v2
		WHERE conversation_id = $1
	`, convID).Scan(&memberAccountID); err != nil {
		t.Fatalf("query conversation_members_v2: %v", err)
	}
	if memberAccountID != member.AccountID {
		t.Fatalf("conversation_members_v2.account_id = %q, want %q", memberAccountID, member.AccountID)
	}

	var managerAccountID string
	if err := pool.QueryRow(ctx, `
		SELECT account_id
		FROM conversation_manager_assignments_v2
		WHERE conversation_id = $1
	`, convID).Scan(&managerAccountID); err != nil {
		t.Fatalf("query conversation_manager_assignments_v2: %v", err)
	}
	if managerAccountID != member.AccountID {
		t.Fatalf("conversation_manager_assignments_v2.account_id = %q, want %q", managerAccountID, member.AccountID)
	}

	var allowedAccountID string
	if err := pool.QueryRow(ctx, `
		SELECT account_id
		FROM conversation_posting_policy_allowed_accounts_v2
		WHERE conversation_id = $1
	`, convID).Scan(&allowedAccountID); err != nil {
		t.Fatalf("query conversation_posting_policy_allowed_accounts_v2: %v", err)
	}
	if allowedAccountID != member.AccountID {
		t.Fatalf("conversation_posting_policy_allowed_accounts_v2.account_id = %q, want %q", allowedAccountID, member.AccountID)
	}

	var authorAccountID, authorWorkspaceMembershipID string
	if err := pool.QueryRow(ctx, `
		SELECT author_account_id, author_workspace_membership_id
		FROM messages
		WHERE channel_id = $1 AND ts = $2
	`, convID, "1700000000.000001").Scan(&authorAccountID, &authorWorkspaceMembershipID); err != nil {
		t.Fatalf("query messages: %v", err)
	}
	if authorAccountID != member.AccountID {
		t.Fatalf("messages.author_account_id = %q, want %q", authorAccountID, member.AccountID)
	}
	if authorWorkspaceMembershipID != membershipID {
		t.Fatalf("messages.author_workspace_membership_id = %q, want %q", authorWorkspaceMembershipID, membershipID)
	}
}

func TestReplayCorrectness_GuestWorkspaceMembershipProjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	workspaceRepo := pgRepo.NewWorkspaceRepo(pool)
	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)
	convRepo := pgRepo.NewConversationRepo(pool)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)

	hostWorkspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name:   "host-workspace",
		Domain: fmt.Sprintf("host-%d.example.com", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("create host workspace: %v", err)
	}
	guestWorkspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name:   "guest-workspace",
		Domain: fmt.Sprintf("guest-%d.example.com", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("create guest workspace: %v", err)
	}

	owner, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   hostWorkspace.ID,
		Name:          "owner",
		Email:         "owner@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	guest, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   guestWorkspace.ID,
		Name:          "guest",
		Email:         "guest@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}

	conv, err := convSvc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: hostWorkspace.ID,
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		Name:        "shared",
		Type:        domain.ConversationTypePublicChannel,
		CreatorID:   owner.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if err := convRepo.AddMemberByAccount(ctx, conv.ID, guest.AccountID); err != nil {
		t.Fatalf("add guest member by account: %v", err)
	}

	var membershipID, membershipKind, guestScope string
	if err := pool.QueryRow(ctx, `
		SELECT id, membership_kind, guest_scope
		FROM workspace_memberships
		WHERE workspace_id = $1 AND account_id = $2
	`, hostWorkspace.ID, guest.AccountID).Scan(&membershipID, &membershipKind, &guestScope); err != nil {
		t.Fatalf("query workspace_memberships: %v", err)
	}
	if membershipKind != "guest" {
		t.Fatalf("workspace_memberships.membership_kind = %q, want guest", membershipKind)
	}
	if guestScope != "single_conversation" {
		t.Fatalf("workspace_memberships.guest_scope = %q, want single_conversation", guestScope)
	}

	var accessCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM workspace_membership_conversation_access
		WHERE workspace_membership_id = $1 AND conversation_id = $2
	`, membershipID, conv.ID).Scan(&accessCount); err != nil {
		t.Fatalf("query workspace_membership_conversation_access: %v", err)
	}
	if accessCount != 1 {
		t.Fatalf("workspace_membership_conversation_access count = %d, want 1", accessCount)
	}

	var profileCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM workspace_profiles
		WHERE workspace_id = $1 AND account_id = $2
	`, hostWorkspace.ID, guest.AccountID).Scan(&profileCount); err != nil {
		t.Fatalf("query workspace_profiles: %v", err)
	}
	if profileCount != 1 {
		t.Fatalf("workspace_profiles count = %d, want 1", profileCount)
	}

	var workspaceUserID string
	if err := pool.QueryRow(ctx, `
		SELECT id
		FROM users
		WHERE workspace_id = $1 AND account_id = $2
	`, hostWorkspace.ID, guest.AccountID).Scan(&workspaceUserID); err != nil {
		t.Fatalf("query guest workspace user: %v", err)
	}
	if workspaceUserID == "" {
		t.Fatal("guest workspace user ID is empty")
	}
}

func TestReplayCorrectness_GuestWorkspaceMembershipIsSingleConversationUntilExplicitlyAdded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	workspaceRepo := pgRepo.NewWorkspaceRepo(pool)
	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)
	convRepo := pgRepo.NewConversationRepo(pool)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)

	hostWorkspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name:   "host-restriction",
		Domain: fmt.Sprintf("host-restriction-%d.example.com", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("create host workspace: %v", err)
	}
	guestWorkspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name:   "guest-restriction",
		Domain: fmt.Sprintf("guest-restriction-%d.example.com", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("create guest workspace: %v", err)
	}

	owner, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   hostWorkspace.ID,
		Name:          "owner",
		Email:         "owner-restriction@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	guest, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   guestWorkspace.ID,
		Name:          "guest",
		Email:         "guest-restriction@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}

	convOne, err := convSvc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: hostWorkspace.ID,
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		Name:        "shared-one",
		Type:        domain.ConversationTypePublicChannel,
		CreatorID:   owner.ID,
	})
	if err != nil {
		t.Fatalf("create first conversation: %v", err)
	}
	convTwo, err := convSvc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: hostWorkspace.ID,
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		Name:        "shared-two",
		Type:        domain.ConversationTypePublicChannel,
		CreatorID:   owner.ID,
	})
	if err != nil {
		t.Fatalf("create second conversation: %v", err)
	}

	if err := convRepo.AddMemberByAccount(ctx, convOne.ID, guest.AccountID); err != nil {
		t.Fatalf("add guest to first conversation: %v", err)
	}

	allowed, err := convRepo.IsAccountMember(ctx, convOne.ID, guest.AccountID)
	if err != nil {
		t.Fatalf("check first conversation membership: %v", err)
	}
	if !allowed {
		t.Fatal("expected guest to be allowed in first conversation")
	}

	allowed, err = convRepo.IsAccountMember(ctx, convTwo.ID, guest.AccountID)
	if err != nil {
		t.Fatalf("check second conversation membership before add: %v", err)
	}
	if allowed {
		t.Fatal("expected guest to be restricted from second conversation before explicit add")
	}

	if err := convRepo.AddMemberByAccount(ctx, convTwo.ID, guest.AccountID); err != nil {
		t.Fatalf("add guest to second conversation: %v", err)
	}

	allowed, err = convRepo.IsAccountMember(ctx, convTwo.ID, guest.AccountID)
	if err != nil {
		t.Fatalf("check second conversation membership after add: %v", err)
	}
	if !allowed {
		t.Fatal("expected guest to be allowed in second conversation after explicit add")
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
		WorkspaceID:   "T001",
		Name:          "snapshot-test",
		Email:         "snap@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		RealName:      "Snapshot User",
		DisplayName:   "snapuser",
		IsBot:         true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
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
	if snapshot.WorkspaceID != "T001" {
		t.Errorf("WorkspaceID = %q, want %q", snapshot.WorkspaceID, "T001")
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
		WorkspaceID:   "T001",
		Name:          "counter",
		Email:         "counter@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
		"SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateUser, u.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 3 {
		t.Errorf("event count = %d, want 3 (1 created + 2 updated)", count)
	}

	rows, err := pool.Query(ctx,
		"SELECT id FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id ASC",
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
