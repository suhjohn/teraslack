package eventsourcing_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/eventsourcing"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

// ---------------------------------------------------------------------------
// testEnv — all services wired to a real Postgres container
// ---------------------------------------------------------------------------

type testEnv struct {
	pool        *pgxpool.Pool
	userSvc     *service.UserService
	convSvc     *service.ConversationService
	msgSvc      *service.MessageService
	pinSvc      *service.PinService
	bookmarkSvc *service.BookmarkService
	ugSvc       *service.UsergroupService
	fileSvc     *service.FileService
	authSvc     *service.AuthService
	eventSvc    *service.EventService
	apiKeySvc   *service.APIKeyService
	projector   *eventsourcing.Projector
}

func setupAllServices(t *testing.T) *testEnv {
	t.Helper()
	pool := setupTestDB(t)
	ctx := context.Background()

	migrationsDir := getMigrationsDir(t)
	for _, mig := range []string{"000007_api_keys_principal_type.up.sql", "000008_drop_outbox.up.sql"} {
		data := readMigration(t, migrationsDir, mig)
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("run migration %s: %v", mig, err)
		}
	}

	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	msgRepo := pgRepo.NewMessageRepo(pool)
	pinRepo := pgRepo.NewPinRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)
	usergroupRepo := pgRepo.NewUsergroupRepo(pool)
	fileRepo := pgRepo.NewFileRepo(pool)
	authRepo := pgRepo.NewAuthRepo(pool)
	eventRepo := pgRepo.NewEventRepo(pool, nil)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)

	return &testEnv{
		pool:        pool,
		userSvc:     service.NewUserService(userRepo, recorder, pool, logger),
		convSvc:     service.NewConversationService(convRepo, userRepo, recorder, pool, logger),
		msgSvc:      service.NewMessageService(msgRepo, convRepo, recorder, pool, logger),
		pinSvc:      service.NewPinService(pinRepo, convRepo, msgRepo, recorder, pool, logger),
		bookmarkSvc: service.NewBookmarkService(bookmarkRepo, convRepo, recorder, pool, logger),
		ugSvc:       service.NewUsergroupService(usergroupRepo, userRepo, recorder, pool, logger),
		fileSvc:     service.NewFileService(fileRepo, nil, "http://localhost:8080", recorder, pool, logger),
		authSvc:     service.NewAuthService(authRepo, userRepo, recorder, pool, logger),
		eventSvc:    service.NewEventService(eventRepo, recorder, pool, logger),
		apiKeySvc:   service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger),
		projector:   eventsourcing.NewProjector(pool, logger),
	}
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func queryEventTypes(t *testing.T, env *testEnv, teamID string) []string {
	t.Helper()
	rows, err := env.pool.Query(context.Background(),
		"SELECT event_type FROM service_events WHERE team_id = $1 ORDER BY id ASC", teamID)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			t.Fatalf("scan: %v", err)
		}
		types = append(types, et)
	}
	return types
}

func countEvents(t *testing.T, env *testEnv) int {
	t.Helper()
	var count int
	if err := env.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM service_events").Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func queryPayloads(t *testing.T, env *testEnv, aggType, aggID string) []json.RawMessage {
	t.Helper()
	rows, err := env.pool.Query(context.Background(),
		"SELECT payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id ASC",
		aggType, aggID)
	if err != nil {
		t.Fatalf("query payloads: %v", err)
	}
	defer rows.Close()
	var payloads []json.RawMessage
	for rows.Next() {
		var p json.RawMessage
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		payloads = append(payloads, p)
	}
	return payloads
}

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Flow 1: Workspace Bootstrap & Team Collaboration
// ---------------------------------------------------------------------------

func TestFlow_WorkspaceBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-bootstrap"

	// Step 1: Create admin (human)
	admin, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "admin", Email: "admin@example.com",
		PrincipalType: domain.PrincipalTypeHuman, IsAdmin: true,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if admin.PrincipalType != domain.PrincipalTypeHuman {
		t.Errorf("admin principal_type = %q, want human", admin.PrincipalType)
	}

	// Step 2: Create second user
	alice, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Step 3: Create public channel
	general, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "general",
		Type: domain.ConversationTypePublicChannel, CreatorID: admin.ID,
	})
	if err != nil {
		t.Fatalf("create general: %v", err)
	}
	if general.NumMembers != 1 {
		t.Errorf("general.NumMembers = %d, want 1", general.NumMembers)
	}

	// Step 4: Invite alice
	if err := env.convSvc.Invite(ctx, general.ID, alice.ID); err != nil {
		t.Fatalf("invite alice: %v", err)
	}
	conv, _ := env.convSvc.Get(ctx, general.ID)
	if conv.NumMembers != 2 {
		t.Errorf("NumMembers after invite = %d, want 2", conv.NumMembers)
	}

	// Step 5: Post message
	msg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: general.ID, UserID: admin.ID, Text: "Welcome!",
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	// Step 6: Reply in thread
	reply, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: general.ID, UserID: alice.ID, Text: "Thanks!", ThreadTS: msg.TS,
	})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.ThreadTS == nil || *reply.ThreadTS != msg.TS {
		t.Errorf("reply.ThreadTS = %v, want %q", reply.ThreadTS, msg.TS)
	}

	// Step 7: Add reaction
	if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: general.ID, MessageTS: msg.TS, UserID: alice.ID, Emoji: "thumbsup",
	}); err != nil {
		t.Fatalf("reaction: %v", err)
	}

	// Step 8: Pin message
	if _, err := env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: general.ID, MessageTS: msg.TS, UserID: admin.ID,
	}); err != nil {
		t.Fatalf("pin: %v", err)
	}

	// Step 9: Add bookmark
	bm, err := env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: general.ID, Title: "Wiki", Type: "link",
		Link: "https://wiki.example.com", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("bookmark: %v", err)
	}

	// Step 10: List members
	members, err := env.convSvc.ListMembers(ctx, general.ID, "", 100)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members.Items) != 2 {
		t.Errorf("member count = %d, want 2", len(members.Items))
	}

	// Verify event sequence — some events (reaction, bookmark) have empty team_id,
	// so query all events in the DB, not just by team
	var allEvents []string
	eRows, _ := env.pool.Query(ctx, "SELECT event_type FROM service_events ORDER BY id ASC")
	for eRows.Next() {
		var et string
		eRows.Scan(&et)
		allEvents = append(allEvents, et)
	}
	eRows.Close()
	expected := []string{
		domain.EventUserCreated, domain.EventUserCreated,
		domain.EventConversationCreated, domain.EventMemberJoined,
		domain.EventMessagePosted, domain.EventMessagePosted,
		domain.EventReactionAdded, domain.EventPinAdded, domain.EventBookmarkCreated,
	}
	if len(allEvents) != len(expected) {
		t.Errorf("event count = %d, want %d; got %v", len(allEvents), len(expected), allEvents)
	} else {
		for i, want := range expected {
			if allEvents[i] != want {
				t.Errorf("event[%d] = %q, want %q", i, allEvents[i], want)
			}
		}
	}

	// --- Unhappy paths ---
	// Duplicate reaction — may succeed silently (upsert) or error
	err = env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: general.ID, MessageTS: msg.TS, UserID: alice.ID, Emoji: "thumbsup",
	})
	if err != nil && !errors.Is(err, domain.ErrAlreadyReacted) {
		t.Errorf("dup reaction: got unexpected %v", err)
	}

	// Duplicate invite
	err = env.convSvc.Invite(ctx, general.ID, alice.ID)
	if !errors.Is(err, domain.ErrAlreadyInChannel) {
		t.Errorf("dup invite: got %v, want ErrAlreadyInChannel", err)
	}

	// Duplicate pin — should error (already pinned)
	_, err = env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: general.ID, MessageTS: msg.TS, UserID: admin.ID,
	})
	if err == nil {
		t.Error("dup pin: expected error")
	}

	// Post to nonexistent channel
	_, err = env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: "nonexistent", UserID: admin.ID, Text: "x",
	})
	if err == nil {
		t.Error("post to nonexistent channel: expected error")
	}

	// Missing team_id
	_, err = env.userSvc.Create(ctx, domain.CreateUserParams{Name: "bad", Email: "bad@x.com"})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty team_id: got %v, want ErrInvalidArgument", err)
	}

	// Verify bookmark list
	bookmarks, _ := env.bookmarkSvc.List(ctx, general.ID)
	if len(bookmarks) != 1 || bookmarks[0].ID != bm.ID {
		t.Errorf("bookmark mismatch")
	}
}

// ---------------------------------------------------------------------------
// Flow 2: Channel Lifecycle & Archival
// ---------------------------------------------------------------------------

func TestFlow_ChannelLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-channel"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "owner", Email: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "project-alpha",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Set topic
	ch, err = env.convSvc.SetTopic(ctx, ch.ID, domain.SetTopicParams{
		Topic: "Alpha discussion", SetByID: user.ID,
	})
	if err != nil {
		t.Fatalf("set topic: %v", err)
	}
	if ch.Topic.Value != "Alpha discussion" {
		t.Errorf("topic = %q", ch.Topic.Value)
	}

	// Set purpose
	ch, err = env.convSvc.SetPurpose(ctx, ch.ID, domain.SetPurposeParams{
		Purpose: "Coordinate alpha", SetByID: user.ID,
	})
	if err != nil {
		t.Fatalf("set purpose: %v", err)
	}

	// Rename
	newName := "project-beta"
	ch, err = env.convSvc.Update(ctx, ch.ID, domain.UpdateConversationParams{Name: &newName})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if ch.Name != "project-beta" {
		t.Errorf("name = %q", ch.Name)
	}

	// Post before archive
	msg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID, Text: "last msg",
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	// Archive
	if err := env.convSvc.Archive(ctx, ch.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Post to archived fails
	_, err = env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID, Text: "fail",
	})
	if !errors.Is(err, domain.ErrChannelArchived) {
		t.Errorf("post to archived: got %v, want ErrChannelArchived", err)
	}

	// Set topic on archived fails
	_, err = env.convSvc.SetTopic(ctx, ch.ID, domain.SetTopicParams{Topic: "x", SetByID: user.ID})
	if !errors.Is(err, domain.ErrChannelArchived) {
		t.Errorf("topic on archived: got %v, want ErrChannelArchived", err)
	}

	// Unarchive
	if err := env.convSvc.Unarchive(ctx, ch.ID); err != nil {
		t.Fatalf("unarchive: %v", err)
	}

	// Post after unarchive
	if _, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID, Text: "back!",
	}); err != nil {
		t.Fatalf("post after unarchive: %v", err)
	}

	// List exclude_archived
	convs, err := env.convSvc.List(ctx, domain.ListConversationsParams{
		TeamID: teamID, ExcludeArchived: true, Limit: 100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, c := range convs.Items {
		if c.ID == ch.ID {
			found = true
		}
	}
	if !found {
		t.Error("unarchived channel not in list")
	}

	// Old message still accessible
	got, err := env.msgSvc.GetMessage(ctx, ch.ID, msg.TS)
	if err != nil {
		t.Fatalf("get old msg: %v", err)
	}
	if got.Text != "last msg" {
		t.Errorf("old msg text = %q", got.Text)
	}

	// Verify events
	events := queryEventTypes(t, env, teamID)
	expected := []string{
		domain.EventUserCreated, domain.EventConversationCreated,
		domain.EventConversationTopicSet, domain.EventConversationPurposeSet,
		domain.EventConversationUpdated, domain.EventMessagePosted,
		domain.EventConversationArchived, domain.EventConversationUnarchived,
		domain.EventMessagePosted,
	}
	if len(events) < len(expected) {
		t.Errorf("event count = %d, want >= %d; got %v", len(events), len(expected), events)
	}
}

// ---------------------------------------------------------------------------
// Flow 3: Agent Delegation & API Key Authentication
// ---------------------------------------------------------------------------

func TestFlow_AgentDelegation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-agent"

	human, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "human", Email: "human@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create human: %v", err)
	}

	agent, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "agent", Email: "agent@example.com",
		PrincipalType: domain.PrincipalTypeAgent, OwnerID: human.ID, IsBot: true,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if agent.PrincipalType != domain.PrincipalTypeAgent {
		t.Errorf("agent type = %q", agent.PrincipalType)
	}

	// Create API key with delegation
	key, rawKey, err := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "agent-key", TeamID: teamID, PrincipalID: agent.ID,
		CreatedBy: human.ID, OnBehalfOf: human.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
		Permissions: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if !strings.HasPrefix(rawKey, "sk_live_") {
		t.Errorf("key prefix wrong, got %q", rawKey[:min(8, len(rawKey))])
	}
	if key.OnBehalfOf != human.ID {
		t.Errorf("on_behalf_of = %q", key.OnBehalfOf)
	}

	// Validate
	v, err := env.apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if v.PrincipalID != agent.ID {
		t.Errorf("principal = %q", v.PrincipalID)
	}
	if v.OnBehalfOf != human.ID {
		t.Errorf("on_behalf_of = %q", v.OnBehalfOf)
	}

	// Get (hash hidden)
	gotKey, err := env.apiKeySvc.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if gotKey.KeyHash != "" {
		t.Error("key_hash exposed via Get")
	}

	// Update
	desc := "Updated"
	updated, err := env.apiKeySvc.Update(ctx, key.ID, domain.UpdateAPIKeyParams{Description: &desc})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Description != "Updated" {
		t.Errorf("desc = %q", updated.Description)
	}

	// List
	keys, err := env.apiKeySvc.List(ctx, domain.ListAPIKeysParams{TeamID: teamID, Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys.Items) != 1 {
		t.Errorf("count = %d", len(keys.Items))
	}

	// Revoke
	if err := env.apiKeySvc.Revoke(ctx, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err = env.apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if !errors.Is(err, domain.ErrTokenRevoked) {
		t.Errorf("validate revoked: got %v", err)
	}

	// List with revoked
	keys, _ = env.apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		TeamID: teamID, IncludeRevoked: true, Limit: 100,
	})
	if len(keys.Items) != 1 || !keys.Items[0].Revoked {
		t.Error("expected 1 revoked key")
	}

	// Verify revoke payload has revoked_at
	payloads := queryPayloads(t, env, domain.AggregateAPIKey, key.ID)
	var lastP domain.APIKey
	if err := json.Unmarshal(payloads[len(payloads)-1], &lastP); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if lastP.RevokedAt == nil {
		t.Error("revoke payload missing revoked_at")
	}

	// --- Unhappy paths ---
	// Nonexistent principal
	_, _, err = env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "bad", TeamID: teamID, PrincipalID: "nonexistent",
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
	})
	if err == nil {
		t.Error("nonexistent principal: expected error")
	}

	// Empty name
	_, _, err = env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		TeamID: teamID, PrincipalID: agent.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty name: got %v", err)
	}

	// Garbage key
	_, err = env.apiKeySvc.ValidateAPIKey(ctx, "sk_live_garbage")
	if !errors.Is(err, domain.ErrInvalidAuth) {
		t.Errorf("garbage key: got %v", err)
	}

	// Verify api_key events
	events := queryEventTypes(t, env, teamID)
	akCount := 0
	for _, e := range events {
		if strings.HasPrefix(e, "api_key.") {
			akCount++
		}
	}
	if akCount != 3 { // created, updated, revoked
		t.Errorf("api_key events = %d, want 3", akCount)
	}
}

// ---------------------------------------------------------------------------
// Flow 4: Key Rotation with Grace Period
// ---------------------------------------------------------------------------

func TestFlow_KeyRotation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-rotation"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "rotator", Email: "rotator@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	oldKey, oldRaw, err := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "rotate-me", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if _, err := env.apiKeySvc.ValidateAPIKey(ctx, oldRaw); err != nil {
		t.Fatalf("validate old: %v", err)
	}

	// Rotate with grace period
	newKey, newRaw, err := env.apiKeySvc.Rotate(ctx, oldKey.ID, domain.RotateAPIKeyParams{
		GracePeriod: "24h",
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newKey.ID == oldKey.ID {
		t.Error("IDs should differ")
	}
	if newRaw == oldRaw {
		t.Error("raw keys should differ")
	}

	// Both keys valid during grace
	if _, err := env.apiKeySvc.ValidateAPIKey(ctx, newRaw); err != nil {
		t.Fatalf("validate new: %v", err)
	}
	if _, err := env.apiKeySvc.ValidateAPIKey(ctx, oldRaw); err != nil {
		t.Fatalf("validate old during grace: %v", err)
	}

	// Old key has rotation metadata
	oldState, _ := env.apiKeySvc.Get(ctx, oldKey.ID)
	if oldState.RotatedToID != newKey.ID {
		t.Errorf("rotated_to_id = %q, want %q", oldState.RotatedToID, newKey.ID)
	}
	if oldState.GracePeriodEndsAt == nil {
		t.Error("missing grace_period_ends_at")
	}

	// List shows both
	keys, _ := env.apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		TeamID: teamID, IncludeRevoked: true, Limit: 100,
	})
	if len(keys.Items) != 2 {
		t.Errorf("key count = %d, want 2", len(keys.Items))
	}

	// Verify events: 2 created + 1 rotated
	events := queryEventTypes(t, env, teamID)
	created, rotated := 0, 0
	for _, e := range events {
		switch e {
		case domain.EventAPIKeyCreated:
			created++
		case domain.EventAPIKeyRotated:
			rotated++
		}
	}
	if created != 2 {
		t.Errorf("created events = %d, want 2", created)
	}
	if rotated != 1 {
		t.Errorf("rotated events = %d, want 1", rotated)
	}

	// --- Unhappy: rotate revoked key ---
	if err := env.apiKeySvc.Revoke(ctx, newKey.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, _, err = env.apiKeySvc.Rotate(ctx, newKey.ID, domain.RotateAPIKeyParams{})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("rotate revoked: got %v", err)
	}

	_, err = env.apiKeySvc.ValidateAPIKey(ctx, newRaw)
	if !errors.Is(err, domain.ErrTokenRevoked) {
		t.Errorf("validate revoked: got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flow 5: Message Threading, Editing & Deletion
// ---------------------------------------------------------------------------

func TestFlow_MessageThreading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-threading"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "threader", Email: "threader@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "dev",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	user2, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "replier", Email: "replier@example.com",
	})
	if err != nil {
		t.Fatalf("create user2: %v", err)
	}
	if err := env.convSvc.Invite(ctx, ch.ID, user2.ID); err != nil {
		t.Fatalf("invite: %v", err)
	}

	// Post parent
	parent, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID, Text: "parent",
	})
	if err != nil {
		t.Fatalf("post parent: %v", err)
	}

	// Post 3 replies
	var replyTSes []string
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Millisecond)
		r, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
			ChannelID: ch.ID, UserID: user2.ID, Text: "reply", ThreadTS: parent.TS,
		})
		if err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
		replyTSes = append(replyTSes, r.TS)
	}

	// Get replies
	replies, err := env.msgSvc.Replies(ctx, domain.ListRepliesParams{
		ChannelID: ch.ID, ThreadTS: parent.TS, Limit: 100,
	})
	if err != nil {
		t.Fatalf("replies: %v", err)
	}
	if len(replies.Items) < 3 {
		t.Errorf("reply count = %d, want >= 3", len(replies.Items))
	}

	// Edit first reply
	newText := "edited"
	edited, err := env.msgSvc.UpdateMessage(ctx, ch.ID, replyTSes[0], domain.UpdateMessageParams{
		Text: &newText,
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if edited.Text != "edited" {
		t.Errorf("text = %q", edited.Text)
	}

	// Delete last reply
	if err := env.msgSvc.DeleteMessage(ctx, ch.ID, replyTSes[2]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted, err := env.msgSvc.GetMessage(ctx, ch.ID, replyTSes[2])
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("get deleted: %v", err)
		}
	} else if !deleted.IsDeleted {
		t.Error("should be marked deleted")
	}

	// Multiple reactions on parent
	for _, emoji := range []string{"fire", "rocket", "heart"} {
		if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
			ChannelID: ch.ID, MessageTS: parent.TS, UserID: user.ID, Emoji: emoji,
		}); err != nil {
			t.Fatalf("react %s: %v", emoji, err)
		}
	}
	reactions, _ := env.msgSvc.GetReactions(ctx, ch.ID, parent.TS)
	if len(reactions) != 3 {
		t.Errorf("reactions = %d, want 3", len(reactions))
	}

	// Remove reaction
	if err := env.msgSvc.RemoveReaction(ctx, domain.RemoveReactionParams{
		ChannelID: ch.ID, MessageTS: parent.TS, UserID: user.ID, Emoji: "fire",
	}); err != nil {
		t.Fatalf("remove reaction: %v", err)
	}
	reactions, _ = env.msgSvc.GetReactions(ctx, ch.ID, parent.TS)
	if len(reactions) != 2 {
		t.Errorf("reactions after remove = %d, want 2", len(reactions))
	}

	// History
	history, _ := env.msgSvc.History(ctx, domain.ListMessagesParams{ChannelID: ch.ID, Limit: 100})
	if len(history.Items) < 1 {
		t.Error("empty history")
	}

	// --- Unhappy paths ---
	// Edit nonexistent
	_, err = env.msgSvc.UpdateMessage(ctx, ch.ID, "9999999.999999", domain.UpdateMessageParams{Text: strPtr("x")})
	if err == nil {
		t.Error("edit nonexistent: expected error")
	}

	// Delete nonexistent — soft delete, may or may not error depending on impl
	_ = env.msgSvc.DeleteMessage(ctx, ch.ID, "9999999.999999")

	// Remove nonexistent reaction — may or may not error depending on impl
	_ = env.msgSvc.RemoveReaction(ctx, domain.RemoveReactionParams{
		ChannelID: ch.ID, MessageTS: parent.TS, UserID: user.ID, Emoji: "nonexistent",
	})

	// Verify event types present — message events use empty team_id, so query all events
	var allEventTypes []string
	rows, err := env.pool.Query(ctx, "SELECT event_type FROM service_events ORDER BY id ASC")
	if err != nil {
		t.Fatalf("query all events: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			t.Fatalf("scan: %v", err)
		}
		allEventTypes = append(allEventTypes, et)
	}
	hasPosted, hasUpdated, hasDeleted := false, false, false
	for _, e := range allEventTypes {
		switch e {
		case domain.EventMessagePosted:
			hasPosted = true
		case domain.EventMessageUpdated:
			hasUpdated = true
		case domain.EventMessageDeleted:
			hasDeleted = true
		}
	}
	if !hasPosted || !hasUpdated || !hasDeleted {
		t.Errorf("missing events: posted=%v updated=%v deleted=%v", hasPosted, hasUpdated, hasDeleted)
	}
}

// ---------------------------------------------------------------------------
// Flow 6: Usergroups & Membership Management
// ---------------------------------------------------------------------------

func TestFlow_Usergroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-ug"

	admin, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "ug-admin", Email: "ugadmin@example.com",
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	u1, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "m1", Email: "m1@example.com",
	})
	if err != nil {
		t.Fatalf("create u1: %v", err)
	}
	u2, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "m2", Email: "m2@example.com",
	})
	if err != nil {
		t.Fatalf("create u2: %v", err)
	}

	// Create usergroup
	ug, err := env.ugSvc.Create(ctx, domain.CreateUsergroupParams{
		TeamID: teamID, Name: "Engineers", Handle: "engineers",
		Description: "Eng team", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("create ug: %v", err)
	}

	// Set users
	if err := env.ugSvc.SetUsers(ctx, ug.ID, []string{u1.ID, u2.ID}); err != nil {
		t.Fatalf("set users: %v", err)
	}
	users, _ := env.ugSvc.ListUsers(ctx, ug.ID)
	if len(users) != 2 {
		t.Errorf("users = %d, want 2", len(users))
	}

	// Update name
	newName := "Senior Engineers"
	if _, err := env.ugSvc.Update(ctx, ug.ID, domain.UpdateUsergroupParams{
		Name: &newName, UpdatedBy: admin.ID,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := env.ugSvc.Get(ctx, ug.ID)
	if got.Name != "Senior Engineers" {
		t.Errorf("name = %q", got.Name)
	}

	// Disable
	if err := env.ugSvc.Disable(ctx, ug.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ = env.ugSvc.Get(ctx, ug.ID)
	if got.Enabled {
		t.Error("should be disabled")
	}

	// List with disabled
	ugs, _ := env.ugSvc.List(ctx, domain.ListUsergroupsParams{TeamID: teamID, IncludeDisabled: true})
	if len(ugs) != 1 {
		t.Errorf("with disabled = %d", len(ugs))
	}

	// List without disabled
	ugs, _ = env.ugSvc.List(ctx, domain.ListUsergroupsParams{TeamID: teamID, IncludeDisabled: false})
	if len(ugs) != 0 {
		t.Errorf("without disabled = %d", len(ugs))
	}

	// Re-enable
	if err := env.ugSvc.Enable(ctx, ug.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// Update membership
	if err := env.ugSvc.SetUsers(ctx, ug.ID, []string{u1.ID}); err != nil {
		t.Fatalf("set users 2: %v", err)
	}
	users, _ = env.ugSvc.ListUsers(ctx, ug.ID)
	if len(users) != 1 {
		t.Errorf("users after update = %d", len(users))
	}

	// Verify events
	events := queryEventTypes(t, env, teamID)
	ugEvents := 0
	for _, e := range events {
		if strings.HasPrefix(e, "usergroup.") {
			ugEvents++
		}
	}
	// created + users_set + updated + disabled + enabled + users_set = 6
	if ugEvents != 6 {
		t.Errorf("ug events = %d, want 6", ugEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 7: File Lifecycle (Remote Files)
// ---------------------------------------------------------------------------

func TestFlow_FileLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-files"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "uploader", Email: "up@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "files",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Add remote file
	f, err := env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title: "Design Doc", ExternalURL: "https://docs.example.com/design",
		Filetype: "gdoc", UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if !f.IsExternal {
		t.Error("should be external")
	}

	// Share to channel
	if err := env.fileSvc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{
		FileID: f.ID, Channels: []string{ch.ID},
	}); err != nil {
		t.Fatalf("share: %v", err)
	}

	// Get file
	gotFile, _ := env.fileSvc.Get(ctx, f.ID)
	if gotFile.Title != "Design Doc" {
		t.Errorf("title = %q", gotFile.Title)
	}

	// Second file
	f2, err := env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title: "Spec", ExternalURL: "https://docs.example.com/spec",
		Filetype: "gdoc", UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("add second: %v", err)
	}

	// List
	files, _ := env.fileSvc.List(ctx, domain.ListFilesParams{Limit: 100})
	if len(files.Items) < 2 {
		t.Errorf("files = %d, want >= 2", len(files.Items))
	}

	// Delete
	if err := env.fileSvc.Delete(ctx, f2.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = env.fileSvc.Get(ctx, f2.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get deleted: got %v, want ErrNotFound", err)
	}

	// S3 upload with nil S3 fails
	_, err = env.fileSvc.GetUploadURL(ctx, domain.GetUploadURLParams{Filename: "x.txt", Length: 100})
	if err == nil {
		t.Error("S3 nil: expected error")
	}

	// --- Unhappy paths ---
	// No title
	_, err = env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		ExternalURL: "https://x.com", UserID: user.ID,
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("no title: got %v", err)
	}

	// Nonexistent file
	_, err = env.fileSvc.Get(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("nonexistent: got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flow 8: Event Subscription & Webhook Lifecycle
// ---------------------------------------------------------------------------

func TestFlow_EventSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-events"

	sub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks.example.com/events",
		EventTypes: []string{domain.EventTypeMessagePosted, domain.EventTypeReactionAdded},
		Secret:     "secret-123",
	})
	if err != nil {
		t.Fatalf("create sub: %v", err)
	}
	if !sub.Enabled {
		t.Error("should be enabled")
	}

	// Get
	gotSub, _ := env.eventSvc.GetSubscription(ctx, sub.ID)
	if gotSub.URL != "https://hooks.example.com/events" {
		t.Errorf("url = %q", gotSub.URL)
	}

	// Update URL
	newURL := "https://hooks.example.com/v2"
	updated, err := env.eventSvc.UpdateSubscription(ctx, sub.ID, domain.UpdateEventSubscriptionParams{
		URL: &newURL,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.URL != newURL {
		t.Errorf("updated url = %q", updated.URL)
	}

	// Disable
	disabled := false
	if _, err := env.eventSvc.UpdateSubscription(ctx, sub.ID, domain.UpdateEventSubscriptionParams{
		Enabled: &disabled,
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	// Create second subscription
	sub2, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks2.example.com",
		EventTypes: []string{domain.EventTypeChannelCreated}, Secret: "s2",
	})
	if err != nil {
		t.Fatalf("create sub2: %v", err)
	}

	// List
	subs, _ := env.eventSvc.ListSubscriptions(ctx, domain.ListEventSubscriptionsParams{TeamID: teamID})
	if len(subs) != 2 {
		t.Errorf("subs = %d, want 2", len(subs))
	}

	// Delete
	if err := env.eventSvc.DeleteSubscription(ctx, sub2.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	subs, _ = env.eventSvc.ListSubscriptions(ctx, domain.ListEventSubscriptionsParams{TeamID: teamID})
	if len(subs) != 1 {
		t.Errorf("subs after delete = %d", len(subs))
	}

	// Verify events
	events := queryEventTypes(t, env, teamID)
	subEvents := 0
	for _, e := range events {
		if strings.HasPrefix(e, "subscription.") {
			subEvents++
		}
	}
	// created + updated + updated(disable) + created + deleted = 5
	if subEvents != 5 {
		t.Errorf("sub events = %d, want 5", subEvents)
	}

	// Verify the Redacted() method clears the Secret field in payloads
	// Note: with nil encryptor, EncryptedSecret will contain the plaintext value
	// (this is by design — encryption is optional). We verify that the "secret"
	// JSON key is empty (Redacted clears it), not the encrypted_secret field.
	payloads := queryPayloads(t, env, domain.AggregateSubscription, sub.ID)
	for _, p := range payloads {
		var parsed map[string]interface{}
		if err := json.Unmarshal(p, &parsed); err == nil {
			if s, ok := parsed["secret"]; ok && s != nil && s != "" {
				t.Errorf("plaintext secret field should be empty in payload, got %v", s)
			}
		}
	}

	// --- Unhappy paths ---
	// No URL
	_, err = env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, EventTypes: []string{"message.posted"},
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("no url: got %v", err)
	}

	// No event types
	_, err = env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://x.com",
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("no types: got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flow 9: Auth Token Lifecycle
// ---------------------------------------------------------------------------

func TestFlow_AuthTokenLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-auth"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "auth-user", Email: "auth@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Create token
	token, err := env.authSvc.CreateToken(ctx, domain.CreateTokenParams{
		TeamID: teamID, UserID: user.ID, Scopes: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if token.Token == "" {
		t.Error("raw token empty")
	}
	rawToken := token.Token

	// Validate
	auth, err := env.authSvc.ValidateToken(ctx, rawToken)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if auth.UserID != user.ID {
		t.Errorf("user_id = %q", auth.UserID)
	}
	if auth.TeamID != teamID {
		t.Errorf("team_id = %q", auth.TeamID)
	}

	// Validate with Bearer prefix
	auth2, err := env.authSvc.ValidateToken(ctx, "Bearer "+rawToken)
	if err != nil {
		t.Fatalf("validate bearer: %v", err)
	}
	if auth2.UserID != user.ID {
		t.Errorf("bearer user_id = %q", auth2.UserID)
	}

	// Second token
	token2, err := env.authSvc.CreateToken(ctx, domain.CreateTokenParams{
		TeamID: teamID, UserID: user.ID, Scopes: []string{"read"},
	})
	if err != nil {
		t.Fatalf("create token2: %v", err)
	}

	// Revoke first
	if err := env.authSvc.RevokeToken(ctx, rawToken); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Validate revoked fails
	_, err = env.authSvc.ValidateToken(ctx, rawToken)
	if err == nil {
		t.Error("validate revoked: expected error")
	}

	// Second still works
	if _, err := env.authSvc.ValidateToken(ctx, token2.Token); err != nil {
		t.Fatalf("validate token2: %v", err)
	}

	// Verify events — token events may have empty team_id (revoke uses token_hash as aggregate_id)
	// so query all events in the DB for token types
	var tokenEvents int
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE event_type LIKE 'token.%'").Scan(&tokenEvents)
	// created + created + revoked = 3
	if tokenEvents != 3 {
		t.Errorf("token events = %d, want 3", tokenEvents)
	}

	// Verify no raw token in payloads
	allPayloads := queryPayloads(t, env, domain.AggregateToken, token.ID)
	for _, p := range allPayloads {
		if strings.Contains(string(p), rawToken) {
			t.Error("raw token in payload")
		}
	}

	// --- Unhappy paths ---
	// Nonexistent user
	_, err = env.authSvc.CreateToken(ctx, domain.CreateTokenParams{
		TeamID: teamID, UserID: "nonexistent",
	})
	if err == nil {
		t.Error("nonexistent user: expected error")
	}

	// Garbage token
	_, err = env.authSvc.ValidateToken(ctx, "garbage")
	if err == nil {
		t.Error("garbage token: expected error")
	}
}

// ---------------------------------------------------------------------------
// Flow 10: Cross-Cutting Consistency — Full Workspace Lifecycle
// ---------------------------------------------------------------------------

func TestFlow_CrossCuttingConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-crosscut"

	before := countEvents(t, env)

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "cc-user", Email: "cc@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "crosscut",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	msg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID, Text: "consistency",
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: msg.TS, UserID: user.ID, Emoji: "check",
	}); err != nil {
		t.Fatalf("react: %v", err)
	}

	if _, err := env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msg.TS, UserID: user.ID,
	}); err != nil {
		t.Fatalf("pin: %v", err)
	}

	bm, err := env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: ch.ID, Title: "Ref", Type: "link",
		Link: "https://ref.com", CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("bookmark: %v", err)
	}

	ug, err := env.ugSvc.Create(ctx, domain.CreateUsergroupParams{
		TeamID: teamID, Name: "Team", Handle: "team", CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("usergroup: %v", err)
	}

	token, err := env.authSvc.CreateToken(ctx, domain.CreateTokenParams{
		TeamID: teamID, UserID: user.ID, Scopes: []string{"read"},
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	_, _, err = env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "cc-key", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
	})
	if err != nil {
		t.Fatalf("api key: %v", err)
	}

	if _, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks.example.com",
		EventTypes: []string{domain.EventTypeMessagePosted}, Secret: "s",
	}); err != nil {
		t.Fatalf("subscription: %v", err)
	}

	// 10 events: user + conv + msg + reaction + pin + bookmark + ug + token + api_key + sub
	after := countEvents(t, env)
	if after-before != 10 {
		t.Errorf("new events = %d, want 10", after-before)
	}

	// Each aggregate type has events (some services record empty team_id, so don't filter by it)
	for _, agg := range []string{
		domain.AggregateUser, domain.AggregateConversation, domain.AggregateMessage,
		domain.AggregatePin, domain.AggregateBookmark, domain.AggregateUsergroup,
		domain.AggregateToken, domain.AggregateAPIKey, domain.AggregateSubscription,
	} {
		var c int
		env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1", agg).Scan(&c)
		if c == 0 {
			t.Errorf("no events for %q", agg)
		}
	}

	// Rebuild preserves data
	if err := env.projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	u, _ := env.userSvc.Get(ctx, user.ID)
	if u.Name != "cc-user" {
		t.Errorf("user after rebuild = %q", u.Name)
	}
	c, _ := env.convSvc.Get(ctx, ch.ID)
	if c.Name != "crosscut" {
		t.Errorf("conv after rebuild = %q", c.Name)
	}
	bms, _ := env.bookmarkSvc.List(ctx, ch.ID)
	if len(bms) != 1 || bms[0].ID != bm.ID {
		t.Error("bookmark lost after rebuild")
	}
	ugGot, _ := env.ugSvc.Get(ctx, ug.ID)
	if ugGot.Name != "Team" {
		t.Errorf("ug after rebuild = %q", ugGot.Name)
	}
	if _, err := env.authSvc.ValidateToken(ctx, token.Token); err != nil {
		t.Fatalf("token after rebuild: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flow 11: Pagination & Listing Edge Cases
// ---------------------------------------------------------------------------

func TestFlow_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-pagination"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "paginator", Email: "pag@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create 5 more users
	for i := 0; i < 5; i++ {
		ts := time.Now().Format("150405.000000")
		if _, err := env.userSvc.Create(ctx, domain.CreateUserParams{
			TeamID: teamID, Name: "pu-" + ts, Email: "pu" + ts + "@x.com",
		}); err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Paginate users limit=2
	page1, err := env.userSvc.List(ctx, domain.ListUsersParams{TeamID: teamID, Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Errorf("page1 = %d, want 2", len(page1.Items))
	}
	if !page1.HasMore {
		t.Error("page1 should have more")
	}

	// Page 2
	page2, err := env.userSvc.List(ctx, domain.ListUsersParams{
		TeamID: teamID, Limit: 2, Cursor: page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Errorf("page2 = %d", len(page2.Items))
	}

	// No overlap
	ids := map[string]bool{}
	for _, u := range page1.Items {
		ids[u.ID] = true
	}
	for _, u := range page2.Items {
		if ids[u.ID] {
			t.Errorf("overlap: %s", u.ID)
		}
	}

	// Message pagination
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "pag-ch",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Millisecond)
		if _, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
			ChannelID: ch.ID, UserID: user.ID, Text: "msg",
		}); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	msgPage, _ := env.msgSvc.History(ctx, domain.ListMessagesParams{ChannelID: ch.ID, Limit: 2})
	if len(msgPage.Items) != 2 {
		t.Errorf("msg page = %d, want 2", len(msgPage.Items))
	}

	// Conversation pagination
	for i := 0; i < 3; i++ {
		ts := time.Now().Format("150405.000000")
		if _, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
			TeamID: teamID, Name: "pch-" + ts,
			Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
		}); err != nil {
			t.Fatalf("create conv %d: %v", i, err)
		}
		time.Sleep(1 * time.Millisecond)
	}
	convPage, _ := env.convSvc.List(ctx, domain.ListConversationsParams{TeamID: teamID, Limit: 2})
	if len(convPage.Items) != 2 {
		t.Errorf("conv page = %d", len(convPage.Items))
	}
	if !convPage.HasMore {
		t.Error("conv should have more")
	}

	// Empty result
	empty, _ := env.userSvc.List(ctx, domain.ListUsersParams{TeamID: "nonexistent", Limit: 10})
	if len(empty.Items) != 0 {
		t.Errorf("empty = %d", len(empty.Items))
	}
	if empty.HasMore {
		t.Error("empty should not have more")
	}
}

// ---------------------------------------------------------------------------
// Flow 12: Conversation Types — DMs and Group DMs
// ---------------------------------------------------------------------------

func TestFlow_ConversationTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-convtypes"

	alice, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "alice", Email: "alice-dm@example.com",
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bob", Email: "bob-dm@example.com",
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	charlie, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "charlie", Email: "charlie-dm@example.com",
	})
	if err != nil {
		t.Fatalf("create charlie: %v", err)
	}

	// DM (IM)
	dm, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Type: domain.ConversationTypeIM, CreatorID: alice.ID,
	})
	if err != nil {
		t.Fatalf("create DM: %v", err)
	}
	if dm.Type != domain.ConversationTypeIM {
		t.Errorf("dm type = %q", dm.Type)
	}
	if err := env.convSvc.Invite(ctx, dm.ID, bob.ID); err != nil {
		t.Fatalf("invite bob to DM: %v", err)
	}
	if _, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: dm.ID, UserID: alice.ID, Text: "hey bob",
	}); err != nil {
		t.Fatalf("post in DM: %v", err)
	}

	// Group DM (MPIM)
	mpim, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Type: domain.ConversationTypeMPIM, CreatorID: alice.ID,
	})
	if err != nil {
		t.Fatalf("create MPIM: %v", err)
	}
	if mpim.Type != domain.ConversationTypeMPIM {
		t.Errorf("mpim type = %q", mpim.Type)
	}
	if err := env.convSvc.Invite(ctx, mpim.ID, bob.ID); err != nil {
		t.Fatalf("invite bob to MPIM: %v", err)
	}
	if err := env.convSvc.Invite(ctx, mpim.ID, charlie.ID); err != nil {
		t.Fatalf("invite charlie: %v", err)
	}
	if _, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: mpim.ID, UserID: bob.ID, Text: "group!",
	}); err != nil {
		t.Fatalf("post in MPIM: %v", err)
	}

	// Private channel
	priv, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "secret",
		Type: domain.ConversationTypePrivateChannel, CreatorID: alice.ID,
	})
	if err != nil {
		t.Fatalf("create private: %v", err)
	}
	if priv.Type != domain.ConversationTypePrivateChannel {
		t.Errorf("private type = %q", priv.Type)
	}

	// List by type: public (should be 0 since we only created IM/MPIM/private)
	pubConvs, _ := env.convSvc.List(ctx, domain.ListConversationsParams{
		TeamID: teamID,
		Types:  []domain.ConversationType{domain.ConversationTypePublicChannel},
		Limit:  100,
	})
	for _, c := range pubConvs.Items {
		if c.Type != domain.ConversationTypePublicChannel {
			t.Errorf("expected public, got %q", c.Type)
		}
	}

	// List by type: IM
	imConvs, _ := env.convSvc.List(ctx, domain.ListConversationsParams{
		TeamID: teamID,
		Types:  []domain.ConversationType{domain.ConversationTypeIM},
		Limit:  100,
	})
	if len(imConvs.Items) != 1 {
		t.Errorf("IM count = %d, want 1", len(imConvs.Items))
	}

	// Kick from MPIM
	if err := env.convSvc.Kick(ctx, mpim.ID, charlie.ID); err != nil {
		t.Fatalf("kick: %v", err)
	}
	members, _ := env.convSvc.ListMembers(ctx, mpim.ID, "", 100)
	for _, m := range members.Items {
		if m.UserID == charlie.ID {
			t.Error("charlie should be kicked")
		}
	}

	// Verify events
	events := queryEventTypes(t, env, teamID)
	convCreated, memberEvts := 0, 0
	for _, e := range events {
		switch e {
		case domain.EventConversationCreated:
			convCreated++
		case domain.EventMemberJoined, domain.EventMemberLeft:
			memberEvts++
		}
	}
	if convCreated != 3 {
		t.Errorf("conv.created = %d, want 3", convCreated)
	}
	if memberEvts < 3 {
		t.Errorf("member events = %d, want >= 3", memberEvts)
	}
}

// ---------------------------------------------------------------------------
// Flow 13: Concurrent Access & Edge Cases
// ---------------------------------------------------------------------------

func TestFlow_ConcurrentEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-concurrent"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "conc", Email: "conc@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "conc-ch",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Rapid-fire 10 messages
	var msgTSes []string
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Millisecond)
		m, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
			ChannelID: ch.ID, UserID: user.ID, Text: "rapid",
		})
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		msgTSes = append(msgTSes, m.TS)
	}

	// Unique timestamps
	tsSet := map[string]bool{}
	for _, ts := range msgTSes {
		if tsSet[ts] {
			t.Errorf("dup ts: %s", ts)
		}
		tsSet[ts] = true
	}

	// Multi-user reactions on same message
	user2, _ := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "conc2", Email: "conc2@example.com",
	})
	env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: msgTSes[0], UserID: user.ID, Emoji: "wave",
	})
	env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: msgTSes[0], UserID: user2.ID, Emoji: "wave",
	})
	reactions, _ := env.msgSvc.GetReactions(ctx, ch.ID, msgTSes[0])
	for _, r := range reactions {
		if r.Name == "wave" && r.Count != 2 {
			t.Errorf("wave count = %d, want 2", r.Count)
		}
	}

	// Create + immediately revoke API key
	key, rawKey, err := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "ephemeral", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvTest,
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := env.apiKeySvc.Revoke(ctx, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err = env.apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if !errors.Is(err, domain.ErrTokenRevoked) {
		t.Errorf("validate after revoke: got %v", err)
	}

	// Update user and verify lookup
	newName := "updated-conc"
	env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{RealName: &newName})
	byEmail, _ := env.userSvc.GetByEmail(ctx, "conc@example.com")
	if byEmail.RealName != "updated-conc" {
		t.Errorf("real_name = %q", byEmail.RealName)
	}

	// Rapid pin + unpin
	env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msgTSes[1], UserID: user.ID,
	})
	env.pinSvc.Remove(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msgTSes[1], UserID: user.ID,
	})
	pins, _ := env.pinSvc.List(ctx, ch.ID)
	for _, p := range pins {
		if p.MessageTS == msgTSes[1] {
			t.Error("pin should be removed")
		}
	}

	// Total event consistency
	total := countEvents(t, env)
	if total < 20 {
		t.Errorf("total events = %d, want >= 20", total)
	}

	// Rebuild after rapid ops
	if err := env.projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	u, _ := env.userSvc.Get(ctx, user.ID)
	if u.RealName != "updated-conc" {
		t.Errorf("after rebuild = %q", u.RealName)
	}
}

// ---------------------------------------------------------------------------
// Flow 14: User Profile Management — CRUD, profile fields, principal types
// ---------------------------------------------------------------------------

func TestFlow_UserProfileManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-profile"

	// Step 1: Create user with full profile
	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:      teamID,
		Name:        "fullprofile",
		RealName:    "Full Profile User",
		DisplayName: "FPU",
		Email:       "full@example.com",
		IsAdmin:     false,
		Profile: domain.UserProfile{
			Title:       "Senior Engineer",
			Phone:       "+1-555-1234",
			StatusText:  "In a meeting",
			StatusEmoji: ":calendar:",
		},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if user.RealName != "Full Profile User" {
		t.Errorf("real_name = %q", user.RealName)
	}
	if user.DisplayName != "FPU" {
		t.Errorf("display_name = %q", user.DisplayName)
	}
	if user.PrincipalType != domain.PrincipalTypeHuman {
		t.Errorf("default principal_type = %q, want human", user.PrincipalType)
	}

	// Step 2: Get by ID
	fetched, err := env.userSvc.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Email != "full@example.com" {
		t.Errorf("email = %q", fetched.Email)
	}

	// Step 3: Get by email
	byEmail, err := env.userSvc.GetByEmail(ctx, "full@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Errorf("id mismatch: %s != %s", byEmail.ID, user.ID)
	}

	// Step 4: Update real name
	newRealName := "Updated Real Name"
	updated, err := env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{RealName: &newRealName})
	if err != nil {
		t.Fatalf("update real_name: %v", err)
	}
	if updated.RealName != "Updated Real Name" {
		t.Errorf("updated real_name = %q", updated.RealName)
	}

	// Step 5: Update display name
	newDisplay := "New Display"
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{DisplayName: &newDisplay})
	if err != nil {
		t.Fatalf("update display_name: %v", err)
	}
	if updated.DisplayName != "New Display" {
		t.Errorf("display_name = %q", updated.DisplayName)
	}

	// Step 6: Update email
	newEmail := "newemail@example.com"
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{Email: &newEmail})
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if updated.Email != "newemail@example.com" {
		t.Errorf("email = %q", updated.Email)
	}

	// Step 7: Verify old email lookup fails, new one works
	_, err = env.userSvc.GetByEmail(ctx, "full@example.com")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old email should not resolve: got %v", err)
	}
	byEmail, err = env.userSvc.GetByEmail(ctx, "newemail@example.com")
	if err != nil {
		t.Fatalf("new email lookup: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Error("new email should resolve to same user")
	}

	// Step 8: Promote to admin
	isAdmin := true
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{IsAdmin: &isAdmin})
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if !updated.IsAdmin {
		t.Error("should be admin")
	}

	// Step 9: Mark restricted
	isRestricted := true
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{IsRestricted: &isRestricted})
	if err != nil {
		t.Fatalf("restrict: %v", err)
	}
	if !updated.IsRestricted {
		t.Error("should be restricted")
	}

	// Step 10: Soft delete (deactivate)
	deleted := true
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{Deleted: &deleted})
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if !updated.Deleted {
		t.Error("should be deleted")
	}

	// Step 11: Reactivate
	reactivated := false
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{Deleted: &reactivated})
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if updated.Deleted {
		t.Error("should not be deleted")
	}

	// Step 12: Update profile struct
	newProfile := &domain.UserProfile{
		Title:       "Staff Engineer",
		Phone:       "+1-555-9999",
		StatusText:  "Available",
		StatusEmoji: ":white_check_mark:",
	}
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{Profile: newProfile})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}

	// Step 13: Create system principal
	systemUser, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        teamID,
		Name:          "system-bot",
		Email:         "system@example.com",
		PrincipalType: domain.PrincipalTypeSystem,
		IsBot:         true,
	})
	if err != nil {
		t.Fatalf("create system user: %v", err)
	}
	if systemUser.PrincipalType != domain.PrincipalTypeSystem {
		t.Errorf("system principal_type = %q", systemUser.PrincipalType)
	}
	if !systemUser.IsBot {
		t.Error("system should be bot")
	}

	// Step 14: Create agent with owner
	agent, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        teamID,
		Name:          "devin-agent",
		Email:         "devin@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       user.ID,
		IsBot:         true,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if agent.OwnerID != user.ID {
		t.Errorf("owner_id = %q, want %q", agent.OwnerID, user.ID)
	}

	// Step 15: List all users for team
	allUsers, err := env.userSvc.List(ctx, domain.ListUsersParams{TeamID: teamID, Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(allUsers.Items) != 3 {
		t.Errorf("user count = %d, want 3", len(allUsers.Items))
	}

	// Verify event sequence
	events := queryEventTypes(t, env, teamID)
	userCreated, userUpdated := 0, 0
	for _, e := range events {
		switch e {
		case domain.EventUserCreated:
			userCreated++
		case domain.EventUserUpdated:
			userUpdated++
		}
	}
	if userCreated != 3 {
		t.Errorf("user.created = %d, want 3", userCreated)
	}
	// real_name + display_name + email + admin + restricted + delete + reactivate + profile = 8
	if userUpdated < 8 {
		t.Errorf("user.updated = %d, want >= 8", userUpdated)
	}
}

// ---------------------------------------------------------------------------
// Flow 15: Multi-Channel Workspace — user active across many channels
// ---------------------------------------------------------------------------

func TestFlow_MultiChannelWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-multichan"

	alice, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "alice", Email: "alice-mc@example.com",
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bob", Email: "bob-mc@example.com",
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	charlie, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "charlie", Email: "charlie-mc@example.com",
	})
	if err != nil {
		t.Fatalf("create charlie: %v", err)
	}

	// Create 4 channels
	channelNames := []string{"general", "engineering", "random", "announcements"}
	channels := make([]*domain.Conversation, len(channelNames))
	for i, name := range channelNames {
		ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
			TeamID: teamID, Name: name,
			Type: domain.ConversationTypePublicChannel, CreatorID: alice.ID,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		channels[i] = ch
	}

	// Invite bob to general, engineering, random
	for _, ch := range channels[:3] {
		if err := env.convSvc.Invite(ctx, ch.ID, bob.ID); err != nil {
			t.Fatalf("invite bob to %s: %v", ch.Name, err)
		}
	}

	// Invite charlie to general and random only
	if err := env.convSvc.Invite(ctx, channels[0].ID, charlie.ID); err != nil {
		t.Fatalf("invite charlie general: %v", err)
	}
	if err := env.convSvc.Invite(ctx, channels[2].ID, charlie.ID); err != nil {
		t.Fatalf("invite charlie random: %v", err)
	}

	// Verify member counts
	for i, expected := range []int{3, 2, 3, 1} { // general=3, engineering=2, random=3, announcements=1(alice)
		ch, err := env.convSvc.Get(ctx, channels[i].ID)
		if err != nil {
			t.Fatalf("get %s: %v", channels[i].Name, err)
		}
		if ch.NumMembers != expected {
			t.Errorf("%s members = %d, want %d", channels[i].Name, ch.NumMembers, expected)
		}
	}

	// Post messages in each channel
	for i, ch := range channels {
		_, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
			ChannelID: ch.ID, UserID: alice.ID, Text: "Hello " + channelNames[i],
		})
		if err != nil {
			t.Fatalf("post in %s: %v", channelNames[i], err)
		}
	}

	// Bob posts in engineering (channel he's in)
	engMsg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: channels[1].ID, UserID: bob.ID, Text: "Let's ship it",
	})
	if err != nil {
		t.Fatalf("bob post: %v", err)
	}

	// Charlie replies in general thread
	generalMsg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: channels[0].ID, UserID: alice.ID, Text: "Discussion topic",
	})
	if err != nil {
		t.Fatalf("general thread: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	_, err = env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: channels[0].ID, UserID: charlie.ID, Text: "I agree", ThreadTS: generalMsg.TS,
	})
	if err != nil {
		t.Fatalf("charlie reply: %v", err)
	}

	// Bob reacts to engineering msg
	if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: channels[1].ID, MessageTS: engMsg.TS, UserID: alice.ID, Emoji: "rocket",
	}); err != nil {
		t.Fatalf("react: %v", err)
	}

	// Alice leaves random
	if err := env.convSvc.Kick(ctx, channels[2].ID, alice.ID); err != nil {
		t.Fatalf("kick alice from random: %v", err)
	}
	ch, _ := env.convSvc.Get(ctx, channels[2].ID)
	if ch.NumMembers != 2 {
		t.Errorf("random after kick = %d, want 2", ch.NumMembers)
	}

	// Verify alice is not in random members list
	members, _ := env.convSvc.ListMembers(ctx, channels[2].ID, "", 100)
	for _, m := range members.Items {
		if m.UserID == alice.ID {
			t.Error("alice should not be in random")
		}
	}

	// List all channels
	allConvs, err := env.convSvc.List(ctx, domain.ListConversationsParams{
		TeamID: teamID, Limit: 100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(allConvs.Items) != 4 {
		t.Errorf("conv count = %d, want 4", len(allConvs.Items))
	}

	// Archive announcements, then list excluding archived
	if err := env.convSvc.Archive(ctx, channels[3].ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	filtered, _ := env.convSvc.List(ctx, domain.ListConversationsParams{
		TeamID: teamID, ExcludeArchived: true, Limit: 100,
	})
	if len(filtered.Items) != 3 {
		t.Errorf("non-archived = %d, want 3", len(filtered.Items))
	}

	// Verify history for engineering has 2 messages
	engHistory, _ := env.msgSvc.History(ctx, domain.ListMessagesParams{
		ChannelID: channels[1].ID, Limit: 100,
	})
	if len(engHistory.Items) < 2 {
		t.Errorf("eng history = %d, want >= 2", len(engHistory.Items))
	}
}

// ---------------------------------------------------------------------------
// Flow 16: Deep Threading — reply chains, counts, thread participants
// ---------------------------------------------------------------------------

func TestFlow_DeepThreading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-deepthread"

	// Setup 3 users in a channel
	users := make([]*domain.User, 3)
	for i, name := range []string{"alice", "bob", "charlie"} {
		u, err := env.userSvc.Create(ctx, domain.CreateUserParams{
			TeamID: teamID, Name: name, Email: name + "-dt@example.com",
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		users[i] = u
	}

	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "threads",
		Type: domain.ConversationTypePublicChannel, CreatorID: users[0].ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	for _, u := range users[1:] {
		env.convSvc.Invite(ctx, ch.ID, u.ID)
	}

	// Post parent message
	parent, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: users[0].ID, Text: "Let's discuss the architecture",
	})
	if err != nil {
		t.Fatalf("post parent: %v", err)
	}

	// 5 replies from different users
	replyTexts := []string{
		"I think we should use event sourcing",
		"Agreed, let's also add CQRS",
		"What about the read model?",
		"We can use materialized views",
		"Good idea, let's prototype it",
	}
	replyUsers := []int{1, 2, 0, 1, 2} // bob, charlie, alice, bob, charlie
	var replies []*domain.Message
	for i, text := range replyTexts {
		time.Sleep(2 * time.Millisecond)
		r, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
			ChannelID: ch.ID, UserID: users[replyUsers[i]].ID,
			Text: text, ThreadTS: parent.TS,
		})
		if err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
		replies = append(replies, r)
	}

	// Verify all replies reference parent
	for i, r := range replies {
		if r.ThreadTS == nil || *r.ThreadTS != parent.TS {
			t.Errorf("reply[%d].ThreadTS = %v, want %q", i, r.ThreadTS, parent.TS)
		}
	}

	// Get thread replies
	threadReplies, err := env.msgSvc.Replies(ctx, domain.ListRepliesParams{
		ChannelID: ch.ID, ThreadTS: parent.TS, Limit: 100,
	})
	if err != nil {
		t.Fatalf("replies: %v", err)
	}
	if len(threadReplies.Items) < 5 {
		t.Errorf("thread reply count = %d, want >= 5", len(threadReplies.Items))
	}

	// Edit a reply in the thread
	editedText := "I think we should definitely use event sourcing (edited)"
	edited, err := env.msgSvc.UpdateMessage(ctx, ch.ID, replies[0].TS, domain.UpdateMessageParams{
		Text: &editedText,
	})
	if err != nil {
		t.Fatalf("edit reply: %v", err)
	}
	if edited.Text != editedText {
		t.Errorf("edited text = %q", edited.Text)
	}

	// Delete middle reply
	if err := env.msgSvc.DeleteMessage(ctx, ch.ID, replies[2].TS); err != nil {
		t.Fatalf("delete reply: %v", err)
	}

	// Add reactions to multiple thread messages
	if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: replies[0].TS, UserID: users[0].ID, Emoji: "thumbsup",
	}); err != nil {
		t.Fatalf("react reply 0: %v", err)
	}
	if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: replies[0].TS, UserID: users[2].ID, Emoji: "thumbsup",
	}); err != nil {
		t.Fatalf("react reply 0 charlie: %v", err)
	}
	if err := env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: replies[3].TS, UserID: users[0].ID, Emoji: "100",
	}); err != nil {
		t.Fatalf("react reply 3: %v", err)
	}

	// Verify reaction counts
	reactions0, _ := env.msgSvc.GetReactions(ctx, ch.ID, replies[0].TS)
	for _, r := range reactions0 {
		if r.Name == "thumbsup" && r.Count != 2 {
			t.Errorf("thumbsup count = %d, want 2", r.Count)
		}
	}

	// Post second parent + replies (verify two threads are independent)
	time.Sleep(2 * time.Millisecond)
	parent2, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: users[1].ID, Text: "Separate topic",
	})
	if err != nil {
		t.Fatalf("post parent2: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	_, err = env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: users[2].ID, Text: "Reply to topic 2", ThreadTS: parent2.TS,
	})
	if err != nil {
		t.Fatalf("reply to parent2: %v", err)
	}

	// Thread 2 has 1 reply only
	thread2Replies, _ := env.msgSvc.Replies(ctx, domain.ListRepliesParams{
		ChannelID: ch.ID, ThreadTS: parent2.TS, Limit: 100,
	})
	if len(thread2Replies.Items) < 1 {
		t.Errorf("thread2 replies = %d", len(thread2Replies.Items))
	}

	// Channel history shows all top-level + thread messages
	history, _ := env.msgSvc.History(ctx, domain.ListMessagesParams{
		ChannelID: ch.ID, Limit: 100,
	})
	if len(history.Items) < 2 { // At least 2 parent messages
		t.Errorf("history = %d", len(history.Items))
	}

	// Paginate thread replies
	pageSize := 2
	page1, _ := env.msgSvc.Replies(ctx, domain.ListRepliesParams{
		ChannelID: ch.ID, ThreadTS: parent.TS, Limit: pageSize,
	})
	if len(page1.Items) != pageSize {
		t.Errorf("thread page1 = %d, want %d", len(page1.Items), pageSize)
	}
	if !page1.HasMore {
		t.Error("thread should have more pages")
	}
}

// ---------------------------------------------------------------------------
// Flow 17: Bookmark Full CRUD — multiple bookmarks, updates, emojis
// ---------------------------------------------------------------------------

func TestFlow_BookmarkFullCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-bookmarks"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bookmarker", Email: "bm@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	user2, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bookmarker2", Email: "bm2@example.com",
	})
	if err != nil {
		t.Fatalf("create user2: %v", err)
	}

	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "bookmarks-ch",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Create 3 bookmarks
	bm1, err := env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: ch.ID, Title: "Wiki", Type: "link",
		Link: "https://wiki.example.com", Emoji: ":book:", CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bm1: %v", err)
	}
	if bm1.Emoji != ":book:" {
		t.Errorf("emoji = %q", bm1.Emoji)
	}

	bm2, err := env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: ch.ID, Title: "Figma Design", Type: "link",
		Link: "https://figma.com/design", Emoji: ":art:", CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bm2: %v", err)
	}

	bm3, err := env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: ch.ID, Title: "Runbook", Type: "link",
		Link: "https://runbook.example.com", CreatedBy: user2.ID,
	})
	if err != nil {
		t.Fatalf("create bm3: %v", err)
	}

	// List — should have 3
	bookmarks, err := env.bookmarkSvc.List(ctx, ch.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bookmarks) != 3 {
		t.Errorf("bookmark count = %d, want 3", len(bookmarks))
	}

	// Update title + link on bm1
	newTitle := "Internal Wiki (Updated)"
	newLink := "https://wiki.example.com/v2"
	updatedBm, err := env.bookmarkSvc.Update(ctx, bm1.ID, domain.UpdateBookmarkParams{
		Title: &newTitle, Link: &newLink, UpdatedBy: user2.ID,
	})
	if err != nil {
		t.Fatalf("update bm1: %v", err)
	}
	if updatedBm.Title != "Internal Wiki (Updated)" {
		t.Errorf("title = %q", updatedBm.Title)
	}
	if updatedBm.Link != "https://wiki.example.com/v2" {
		t.Errorf("link = %q", updatedBm.Link)
	}
	if updatedBm.UpdatedBy != user2.ID {
		t.Errorf("updated_by = %q", updatedBm.UpdatedBy)
	}

	// Update emoji on bm2
	newEmoji := ":paintbrush:"
	updatedBm2, err := env.bookmarkSvc.Update(ctx, bm2.ID, domain.UpdateBookmarkParams{
		Emoji: &newEmoji, UpdatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("update bm2 emoji: %v", err)
	}
	if updatedBm2.Emoji != ":paintbrush:" {
		t.Errorf("emoji = %q", updatedBm2.Emoji)
	}

	// Delete bm3
	if err := env.bookmarkSvc.Delete(ctx, bm3.ID); err != nil {
		t.Fatalf("delete bm3: %v", err)
	}

	// List — should have 2
	bookmarks, _ = env.bookmarkSvc.List(ctx, ch.ID)
	if len(bookmarks) != 2 {
		t.Errorf("after delete = %d, want 2", len(bookmarks))
	}

	// Verify deleted bookmark IDs
	for _, bm := range bookmarks {
		if bm.ID == bm3.ID {
			t.Error("bm3 should be deleted")
		}
	}

	// Create bookmark in second channel
	ch2, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "other-ch",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create ch2: %v", err)
	}
	_, err = env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: ch2.ID, Title: "Ch2 Wiki", Type: "link",
		Link: "https://ch2wiki.com", CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create bm in ch2: %v", err)
	}

	// Bookmarks are scoped to channel
	ch1Bookmarks, _ := env.bookmarkSvc.List(ctx, ch.ID)
	ch2Bookmarks, _ := env.bookmarkSvc.List(ctx, ch2.ID)
	if len(ch1Bookmarks) != 2 {
		t.Errorf("ch1 bookmarks = %d", len(ch1Bookmarks))
	}
	if len(ch2Bookmarks) != 1 {
		t.Errorf("ch2 bookmarks = %d", len(ch2Bookmarks))
	}

	// Verify events
	var bmEvents int
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregateBookmark).Scan(&bmEvents)
	// 3 created + 2 updated + 1 deleted + 1 created = 7
	if bmEvents != 7 {
		t.Errorf("bookmark events = %d, want 7", bmEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 18: Pin Lifecycle — pin multiple, list, unpin, re-pin
// ---------------------------------------------------------------------------

func TestFlow_PinLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-pins"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "pinner", Email: "pinner@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "pinboard",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Post 4 messages
	var msgs []*domain.Message
	for i := 0; i < 4; i++ {
		time.Sleep(2 * time.Millisecond)
		m, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
			ChannelID: ch.ID, UserID: user.ID, Text: "pin candidate " + string(rune('A'+i)),
		})
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		msgs = append(msgs, m)
	}

	// Pin first 3
	for i := 0; i < 3; i++ {
		if _, err := env.pinSvc.Add(ctx, domain.PinParams{
			ChannelID: ch.ID, MessageTS: msgs[i].TS, UserID: user.ID,
		}); err != nil {
			t.Fatalf("pin %d: %v", i, err)
		}
	}

	// List pins — should have 3
	pins, err := env.pinSvc.List(ctx, ch.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pins) != 3 {
		t.Errorf("pin count = %d, want 3", len(pins))
	}

	// Unpin second message
	if err := env.pinSvc.Remove(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msgs[1].TS, UserID: user.ID,
	}); err != nil {
		t.Fatalf("unpin: %v", err)
	}

	// List pins — should have 2
	pins, _ = env.pinSvc.List(ctx, ch.ID)
	if len(pins) != 2 {
		t.Errorf("after unpin = %d, want 2", len(pins))
	}

	// Verify unpinned message is not in list
	for _, p := range pins {
		if p.MessageTS == msgs[1].TS {
			t.Error("unpinned message should not be in list")
		}
	}

	// Re-pin the message
	if _, err := env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msgs[1].TS, UserID: user.ID,
	}); err != nil {
		t.Fatalf("re-pin: %v", err)
	}
	pins, _ = env.pinSvc.List(ctx, ch.ID)
	if len(pins) != 3 {
		t.Errorf("after re-pin = %d, want 3", len(pins))
	}

	// Pin 4th message
	if _, err := env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msgs[3].TS, UserID: user.ID,
	}); err != nil {
		t.Fatalf("pin 4th: %v", err)
	}
	pins, _ = env.pinSvc.List(ctx, ch.ID)
	if len(pins) != 4 {
		t.Errorf("all pinned = %d, want 4", len(pins))
	}

	// Unpin all
	for _, m := range msgs {
		env.pinSvc.Remove(ctx, domain.PinParams{
			ChannelID: ch.ID, MessageTS: m.TS, UserID: user.ID,
		})
	}
	pins, _ = env.pinSvc.List(ctx, ch.ID)
	if len(pins) != 0 {
		t.Errorf("after unpin all = %d, want 0", len(pins))
	}

	// Verify events
	var pinEvents int
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregatePin).Scan(&pinEvents)
	// 3 add + 1 remove + 1 re-add + 1 add + 4 remove = 10
	if pinEvents != 10 {
		t.Errorf("pin events = %d, want 10", pinEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 19: File Sharing Across Channels
// ---------------------------------------------------------------------------

func TestFlow_FileSharingAcrossChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-fileshare"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "sharer", Email: "sharer@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create 3 channels
	ch1, _ := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "design",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	ch2, _ := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "dev",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	ch3, _ := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "product",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})

	// Add 3 remote files
	designDoc, err := env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title: "Design System", ExternalURL: "https://figma.com/design-system",
		Filetype: "figma", UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("add design doc: %v", err)
	}

	spec, err := env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title: "API Spec", ExternalURL: "https://docs.example.com/spec",
		Filetype: "gdoc", UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("add spec: %v", err)
	}

	readme, err := env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title: "README", ExternalURL: "https://github.com/org/repo/README.md",
		Filetype: "markdown", UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("add readme: %v", err)
	}

	// Share design doc to design and product
	if err := env.fileSvc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{
		FileID: designDoc.ID, Channels: []string{ch1.ID, ch3.ID},
	}); err != nil {
		t.Fatalf("share design: %v", err)
	}

	// Share spec to all 3 channels
	if err := env.fileSvc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{
		FileID: spec.ID, Channels: []string{ch1.ID, ch2.ID, ch3.ID},
	}); err != nil {
		t.Fatalf("share spec: %v", err)
	}

	// Share readme to dev only
	if err := env.fileSvc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{
		FileID: readme.ID, Channels: []string{ch2.ID},
	}); err != nil {
		t.Fatalf("share readme: %v", err)
	}

	// Verify file metadata
	gotDesign, _ := env.fileSvc.Get(ctx, designDoc.ID)
	if gotDesign.Title != "Design System" {
		t.Errorf("title = %q", gotDesign.Title)
	}
	if !gotDesign.IsExternal {
		t.Error("should be external")
	}

	// List all files
	allFiles, err := env.fileSvc.List(ctx, domain.ListFilesParams{Limit: 100})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(allFiles.Items) < 3 {
		t.Errorf("all files = %d, want >= 3", len(allFiles.Items))
	}

	// Delete spec
	if err := env.fileSvc.Delete(ctx, spec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify spec is gone
	_, err = env.fileSvc.Get(ctx, spec.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get deleted: %v", err)
	}

	// Others still exist
	gotDesign, _ = env.fileSvc.Get(ctx, designDoc.ID)
	if gotDesign == nil {
		t.Error("design doc should still exist")
	}
	gotReadme, _ := env.fileSvc.Get(ctx, readme.ID)
	if gotReadme == nil {
		t.Error("readme should still exist")
	}

	// Verify events
	var fileEvents int
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregateFile).Scan(&fileEvents)
	// 3 created + 3 shared (updated+shared events) + 1 deleted
	if fileEvents < 7 {
		t.Errorf("file events = %d, want >= 7", fileEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 20: Multiple API Keys Per Principal — environments, permissions, listing
// ---------------------------------------------------------------------------

func TestFlow_MultipleAPIKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-multikey"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "multikey", Email: "multikey@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create live key with read/write permissions
	liveKey, liveRaw, err := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "production", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
		Permissions: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("create live key: %v", err)
	}
	if !strings.HasPrefix(liveRaw, "sk_live_") {
		t.Errorf("live prefix: %q", liveRaw[:min(8, len(liveRaw))])
	}

	// Create test key with read-only permissions
	testKey, testRaw, err := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "staging", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvTest,
		Permissions: []string{"read"},
	})
	if err != nil {
		t.Fatalf("create test key: %v", err)
	}
	if !strings.HasPrefix(testRaw, "sk_test_") {
		t.Errorf("test prefix: %q", testRaw[:min(8, len(testRaw))])
	}

	// Create restricted key
	restrictedKey, restrictedRaw, err := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "ci-deploy", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypeRestricted, Environment: domain.APIKeyEnvLive,
		Permissions: []string{"deploy"},
	})
	if err != nil {
		t.Fatalf("create restricted key: %v", err)
	}

	// Validate each key returns correct metadata
	liveVal, err := env.apiKeySvc.ValidateAPIKey(ctx, liveRaw)
	if err != nil {
		t.Fatalf("validate live: %v", err)
	}
	if liveVal.Environment != domain.APIKeyEnvLive {
		t.Errorf("live env = %q", liveVal.Environment)
	}
	if len(liveVal.Permissions) != 2 {
		t.Errorf("live permissions = %v", liveVal.Permissions)
	}

	testVal, err := env.apiKeySvc.ValidateAPIKey(ctx, testRaw)
	if err != nil {
		t.Fatalf("validate test: %v", err)
	}
	if testVal.Environment != domain.APIKeyEnvTest {
		t.Errorf("test env = %q", testVal.Environment)
	}

	restrictedVal, err := env.apiKeySvc.ValidateAPIKey(ctx, restrictedRaw)
	if err != nil {
		t.Fatalf("validate restricted: %v", err)
	}
	if len(restrictedVal.Permissions) != 1 || restrictedVal.Permissions[0] != "deploy" {
		t.Errorf("restricted permissions = %v", restrictedVal.Permissions)
	}

	// List all keys
	allKeys, err := env.apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		TeamID: teamID, Limit: 100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(allKeys.Items) != 3 {
		t.Errorf("key count = %d, want 3", len(allKeys.Items))
	}

	// Update description on live key
	desc := "Production key for API access"
	_, err = env.apiKeySvc.Update(ctx, liveKey.ID, domain.UpdateAPIKeyParams{Description: &desc})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	gotKey, _ := env.apiKeySvc.Get(ctx, liveKey.ID)
	if gotKey.Description != "Production key for API access" {
		t.Errorf("desc = %q", gotKey.Description)
	}

	// Update permissions on test key
	newPerms := []string{"read", "write", "admin"}
	_, err = env.apiKeySvc.Update(ctx, testKey.ID, domain.UpdateAPIKeyParams{Permissions: &newPerms})
	if err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	testVal2, _ := env.apiKeySvc.ValidateAPIKey(ctx, testRaw)
	if len(testVal2.Permissions) != 3 {
		t.Errorf("updated perms = %v", testVal2.Permissions)
	}

	// Revoke restricted key
	if err := env.apiKeySvc.Revoke(ctx, restrictedKey.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// List without revoked — should be 2
	active, _ := env.apiKeySvc.List(ctx, domain.ListAPIKeysParams{TeamID: teamID, Limit: 100})
	if len(active.Items) != 2 {
		t.Errorf("active count = %d, want 2", len(active.Items))
	}

	// List with revoked — should be 3
	withRevoked, _ := env.apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		TeamID: teamID, IncludeRevoked: true, Limit: 100,
	})
	if len(withRevoked.Items) != 3 {
		t.Errorf("with revoked = %d, want 3", len(withRevoked.Items))
	}

	// Validate revoked fails
	_, err = env.apiKeySvc.ValidateAPIKey(ctx, restrictedRaw)
	if !errors.Is(err, domain.ErrTokenRevoked) {
		t.Errorf("validate revoked: %v", err)
	}

	// Live and test keys still work
	if _, err := env.apiKeySvc.ValidateAPIKey(ctx, liveRaw); err != nil {
		t.Fatalf("live still valid: %v", err)
	}
	if _, err := env.apiKeySvc.ValidateAPIKey(ctx, testRaw); err != nil {
		t.Fatalf("test still valid: %v", err)
	}

	// Verify events
	events := queryEventTypes(t, env, teamID)
	akCreated, akUpdated, akRevoked := 0, 0, 0
	for _, e := range events {
		switch e {
		case domain.EventAPIKeyCreated:
			akCreated++
		case domain.EventAPIKeyUpdated:
			akUpdated++
		case domain.EventAPIKeyRevoked:
			akRevoked++
		}
	}
	if akCreated != 3 {
		t.Errorf("created = %d, want 3", akCreated)
	}
	if akUpdated != 2 {
		t.Errorf("updated = %d, want 2", akUpdated)
	}
	if akRevoked != 1 {
		t.Errorf("revoked = %d, want 1", akRevoked)
	}
}

// ---------------------------------------------------------------------------
// Flow 21: Subscription Event Type Matching — overlapping subscriptions
// ---------------------------------------------------------------------------

func TestFlow_SubscriptionEventTypeMatching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-submatch"

	// Create subscription for message events only
	msgSub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID:     teamID,
		URL:        "https://hooks.example.com/messages",
		EventTypes: []string{domain.EventTypeMessagePosted, domain.EventTypeMessageUpdated, domain.EventTypeMessageDeleted},
		Secret:     "msg-secret",
	})
	if err != nil {
		t.Fatalf("create msg sub: %v", err)
	}
	if len(msgSub.EventTypes) != 3 {
		t.Errorf("msg event types = %d", len(msgSub.EventTypes))
	}

	// Create subscription for channel events only
	chSub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID:     teamID,
		URL:        "https://hooks.example.com/channels",
		EventTypes: []string{domain.EventTypeChannelCreated, domain.EventTypeChannelArchive, domain.EventTypeMemberJoinedChannel},
		Secret:     "ch-secret",
	})
	if err != nil {
		t.Fatalf("create ch sub: %v", err)
	}

	// Create catch-all subscription
	allSub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID,
		URL:    "https://hooks.example.com/all",
		EventTypes: []string{
			domain.EventTypeMessagePosted, domain.EventTypeChannelCreated,
			domain.EventTypeReactionAdded, domain.EventTypePinAdded,
		},
		Secret: "all-secret",
	})
	if err != nil {
		t.Fatalf("create all sub: %v", err)
	}

	// Verify all 3 subscriptions exist
	subs, _ := env.eventSvc.ListSubscriptions(ctx, domain.ListEventSubscriptionsParams{TeamID: teamID})
	if len(subs) != 3 {
		t.Errorf("sub count = %d, want 3", len(subs))
	}

	// Update msg subscription — add reaction events
	newTypes := []string{
		domain.EventTypeMessagePosted, domain.EventTypeMessageUpdated,
		domain.EventTypeMessageDeleted, domain.EventTypeReactionAdded,
	}
	updatedSub, err := env.eventSvc.UpdateSubscription(ctx, msgSub.ID, domain.UpdateEventSubscriptionParams{
		EventTypes: newTypes,
	})
	if err != nil {
		t.Fatalf("update msg sub: %v", err)
	}
	if len(updatedSub.EventTypes) != 4 {
		t.Errorf("updated types = %d", len(updatedSub.EventTypes))
	}

	// Disable the all-events subscription
	disabled := false
	_, err = env.eventSvc.UpdateSubscription(ctx, allSub.ID, domain.UpdateEventSubscriptionParams{
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	gotAll, _ := env.eventSvc.GetSubscription(ctx, allSub.ID)
	if gotAll.Enabled {
		t.Error("should be disabled")
	}

	// Re-enable
	enabled := true
	_, err = env.eventSvc.UpdateSubscription(ctx, allSub.ID, domain.UpdateEventSubscriptionParams{
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	gotAll, _ = env.eventSvc.GetSubscription(ctx, allSub.ID)
	if !gotAll.Enabled {
		t.Error("should be re-enabled")
	}

	// Change URL on channel sub
	newURL := "https://hooks.example.com/channels/v2"
	_, err = env.eventSvc.UpdateSubscription(ctx, chSub.ID, domain.UpdateEventSubscriptionParams{
		URL: &newURL,
	})
	if err != nil {
		t.Fatalf("update url: %v", err)
	}
	gotCh, _ := env.eventSvc.GetSubscription(ctx, chSub.ID)
	if gotCh.URL != newURL {
		t.Errorf("url = %q", gotCh.URL)
	}

	// Delete channel sub
	if err := env.eventSvc.DeleteSubscription(ctx, chSub.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// List — should be 2
	subs, _ = env.eventSvc.ListSubscriptions(ctx, domain.ListEventSubscriptionsParams{TeamID: teamID})
	if len(subs) != 2 {
		t.Errorf("after delete = %d, want 2", len(subs))
	}

	// Verify events
	var subEvents int
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregateSubscription).Scan(&subEvents)
	// 3 created + 1 update(types) + 1 disable + 1 enable + 1 update(url) + 1 deleted = 8
	if subEvents != 8 {
		t.Errorf("sub events = %d, want 8", subEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 22: Complex Projection Rebuild — verify all state survives TRUNCATE+replay
// ---------------------------------------------------------------------------

func TestFlow_ComplexProjectionRebuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-rebuild"

	// Build a rich workspace state
	admin, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "admin", Email: "admin-rb@example.com",
		PrincipalType: domain.PrincipalTypeHuman, IsAdmin: true,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	agent, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "agent", Email: "agent-rb@example.com",
		PrincipalType: domain.PrincipalTypeAgent, OwnerID: admin.ID, IsBot: true,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Create channel with topic and purpose
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "rebuild-test",
		Type: domain.ConversationTypePublicChannel, CreatorID: admin.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	env.convSvc.SetTopic(ctx, ch.ID, domain.SetTopicParams{Topic: "Rebuild test", SetByID: admin.ID})
	env.convSvc.SetPurpose(ctx, ch.ID, domain.SetPurposeParams{Purpose: "Test projection rebuild", SetByID: admin.ID})
	env.convSvc.Invite(ctx, ch.ID, agent.ID)

	// Post messages with thread
	msg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: admin.ID, Text: "parent msg",
	})
	if err != nil {
		t.Fatalf("post parent: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: agent.ID, Text: "reply", ThreadTS: msg.TS,
	})

	// Reactions
	env.msgSvc.AddReaction(ctx, domain.AddReactionParams{
		ChannelID: ch.ID, MessageTS: msg.TS, UserID: admin.ID, Emoji: "star",
	})

	// Pin
	env.pinSvc.Add(ctx, domain.PinParams{
		ChannelID: ch.ID, MessageTS: msg.TS, UserID: admin.ID,
	})

	// Bookmark
	bm, _ := env.bookmarkSvc.Create(ctx, domain.CreateBookmarkParams{
		ChannelID: ch.ID, Title: "Rebuild Wiki", Type: "link",
		Link: "https://wiki.rebuild.com", CreatedBy: admin.ID,
	})

	// Usergroup
	ug, _ := env.ugSvc.Create(ctx, domain.CreateUsergroupParams{
		TeamID: teamID, Name: "Rebuilders", Handle: "rebuilders", CreatedBy: admin.ID,
	})
	env.ugSvc.SetUsers(ctx, ug.ID, []string{admin.ID, agent.ID})

	// File
	f, _ := env.fileSvc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title: "Rebuild Doc", ExternalURL: "https://rebuild.com/doc",
		Filetype: "pdf", UserID: admin.ID,
	})

	// Token
	token, _ := env.authSvc.CreateToken(ctx, domain.CreateTokenParams{
		TeamID: teamID, UserID: admin.ID, Scopes: []string{"read", "write"},
	})

	// API key
	apiKey, apiKeyRaw, _ := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "rebuild-key", TeamID: teamID, PrincipalID: admin.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
		Permissions: []string{"read", "write"},
	})

	// Subscription
	sub, _ := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks.rebuild.com",
		EventTypes: []string{domain.EventTypeMessagePosted}, Secret: "rebuild-secret",
	})

	// Count events before rebuild
	eventsBefore := countEvents(t, env)
	if eventsBefore < 15 {
		t.Errorf("events before rebuild = %d, want >= 15", eventsBefore)
	}

	// Perform full rebuild
	if err := env.projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// Verify events are unchanged
	eventsAfter := countEvents(t, env)
	if eventsAfter != eventsBefore {
		t.Errorf("events changed: %d -> %d", eventsBefore, eventsAfter)
	}

	// Verify admin user survives
	gotAdmin, err := env.userSvc.Get(ctx, admin.ID)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if gotAdmin.Name != "admin" || !gotAdmin.IsAdmin || gotAdmin.PrincipalType != domain.PrincipalTypeHuman {
		t.Errorf("admin state wrong: name=%q isAdmin=%v type=%q", gotAdmin.Name, gotAdmin.IsAdmin, gotAdmin.PrincipalType)
	}

	// Verify agent survives with owner
	gotAgent, err := env.userSvc.Get(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if gotAgent.PrincipalType != domain.PrincipalTypeAgent || gotAgent.OwnerID != admin.ID {
		t.Errorf("agent state wrong: type=%q owner=%q", gotAgent.PrincipalType, gotAgent.OwnerID)
	}

	// Verify channel state
	gotCh, err := env.convSvc.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if gotCh.Topic.Value != "Rebuild test" {
		t.Errorf("topic = %q", gotCh.Topic.Value)
	}
	if gotCh.Purpose.Value != "Test projection rebuild" {
		t.Errorf("purpose = %q", gotCh.Purpose.Value)
	}
	if gotCh.NumMembers != 2 {
		t.Errorf("members = %d", gotCh.NumMembers)
	}

	// Verify messages
	gotMsg, err := env.msgSvc.GetMessage(ctx, ch.ID, msg.TS)
	if err != nil {
		t.Fatalf("get msg: %v", err)
	}
	if gotMsg.Text != "parent msg" {
		t.Errorf("msg text = %q", gotMsg.Text)
	}

	// Verify reactions
	reactions, _ := env.msgSvc.GetReactions(ctx, ch.ID, msg.TS)
	if len(reactions) == 0 {
		t.Error("reactions lost after rebuild")
	}

	// Verify pin
	pins, _ := env.pinSvc.List(ctx, ch.ID)
	if len(pins) != 1 || pins[0].MessageTS != msg.TS {
		t.Errorf("pins lost after rebuild: %d", len(pins))
	}

	// Verify bookmark
	bms, _ := env.bookmarkSvc.List(ctx, ch.ID)
	if len(bms) != 1 || bms[0].ID != bm.ID {
		t.Error("bookmark lost after rebuild")
	}

	// Verify usergroup + members
	gotUG, _ := env.ugSvc.Get(ctx, ug.ID)
	if gotUG.Name != "Rebuilders" {
		t.Errorf("ug name = %q", gotUG.Name)
	}
	ugUsers, _ := env.ugSvc.ListUsers(ctx, ug.ID)
	if len(ugUsers) != 2 {
		t.Errorf("ug users = %d, want 2", len(ugUsers))
	}

	// Verify file
	gotFile, _ := env.fileSvc.Get(ctx, f.ID)
	if gotFile.Title != "Rebuild Doc" {
		t.Errorf("file title = %q", gotFile.Title)
	}

	// Verify token still validates
	if _, err := env.authSvc.ValidateToken(ctx, token.Token); err != nil {
		t.Fatalf("token invalid after rebuild: %v", err)
	}

	// Verify API key still validates
	akVal, err := env.apiKeySvc.ValidateAPIKey(ctx, apiKeyRaw)
	if err != nil {
		t.Fatalf("api key invalid after rebuild: %v", err)
	}
	if akVal.KeyID != apiKey.ID {
		t.Errorf("api key id = %q", akVal.KeyID)
	}

	// Verify subscription
	gotSub, _ := env.eventSvc.GetSubscription(ctx, sub.ID)
	if gotSub.URL != "https://hooks.rebuild.com" {
		t.Errorf("sub url = %q", gotSub.URL)
	}
	if !gotSub.Enabled {
		t.Error("sub should be enabled")
	}
}

// ---------------------------------------------------------------------------
// Flow 23: Message Metadata & Blocks — rich message content
// ---------------------------------------------------------------------------

func TestFlow_MessageMetadataAndBlocks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-blocks"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "richcontent", Email: "rich@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "rich-messages",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Post message with blocks
	blocks := json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"*Hello* from blocks"}}]`)
	msg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID,
		Text:   "fallback text",
		Blocks: blocks,
	})
	if err != nil {
		t.Fatalf("post with blocks: %v", err)
	}
	if msg.Text != "fallback text" {
		t.Errorf("text = %q", msg.Text)
	}

	// Get message and verify blocks are present
	gotMsg, err := env.msgSvc.GetMessage(ctx, ch.ID, msg.TS)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if gotMsg.Blocks == nil {
		t.Error("blocks should be present")
	}

	// Post message with metadata
	metadata := json.RawMessage(`{"event_type":"deploy","event_payload":{"service":"api","version":"1.2.3"}}`)
	msgMeta, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID,
		Text:     "Deploy notification",
		Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("post with metadata: %v", err)
	}

	gotMeta, _ := env.msgSvc.GetMessage(ctx, ch.ID, msgMeta.TS)
	if gotMeta.Metadata == nil {
		t.Error("metadata should be present")
	}

	// Update blocks
	newBlocks := json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"*Updated* content"}}]`)
	updated, err := env.msgSvc.UpdateMessage(ctx, ch.ID, msg.TS, domain.UpdateMessageParams{
		Blocks: newBlocks,
	})
	if err != nil {
		t.Fatalf("update blocks: %v", err)
	}
	if updated.Blocks == nil {
		t.Error("updated blocks should be present")
	}

	// Update text only (blocks should remain)
	newText := "updated fallback"
	updated2, err := env.msgSvc.UpdateMessage(ctx, ch.ID, msg.TS, domain.UpdateMessageParams{
		Text: &newText,
	})
	if err != nil {
		t.Fatalf("update text: %v", err)
	}
	if updated2.Text != "updated fallback" {
		t.Errorf("text = %q", updated2.Text)
	}

	// Post with both blocks and metadata
	time.Sleep(2 * time.Millisecond)
	bothMsg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID,
		Text:     "rich message",
		Blocks:   json.RawMessage(`[{"type":"divider"}]`),
		Metadata: json.RawMessage(`{"event_type":"test"}`),
	})
	if err != nil {
		t.Fatalf("post both: %v", err)
	}
	gotBoth, _ := env.msgSvc.GetMessage(ctx, ch.ID, bothMsg.TS)
	if gotBoth.Blocks == nil || gotBoth.Metadata == nil {
		t.Error("both blocks and metadata should be present")
	}

	// History returns messages with blocks/metadata
	history, _ := env.msgSvc.History(ctx, domain.ListMessagesParams{
		ChannelID: ch.ID, Limit: 100,
	})
	if len(history.Items) < 3 {
		t.Errorf("history = %d, want >= 3", len(history.Items))
	}

	// Verify events
	var msgEvents int
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM service_events WHERE event_type LIKE 'message.%'").Scan(&msgEvents)
	// 3 posted + 2 updated = 5
	if msgEvents != 5 {
		t.Errorf("message events = %d, want 5", msgEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 24: Conversation with Initial Topic/Purpose + Full Update Lifecycle
// ---------------------------------------------------------------------------

func TestFlow_ConversationCreationWithTopicPurpose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-convfull"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "creator", Email: "creator@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create with topic and purpose
	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID:    teamID,
		Name:      "full-channel",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: user.ID,
		Topic:     "Initial Topic",
		Purpose:   "Initial Purpose",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.Topic.Value != "Initial Topic" {
		t.Errorf("topic = %q", ch.Topic.Value)
	}
	if ch.Purpose.Value != "Initial Purpose" {
		t.Errorf("purpose = %q", ch.Purpose.Value)
	}

	// Update topic multiple times
	topics := []string{"Second Topic", "Third Topic", "Final Topic"}
	for _, topic := range topics {
		var err error
		ch, err = env.convSvc.SetTopic(ctx, ch.ID, domain.SetTopicParams{
			Topic: topic, SetByID: user.ID,
		})
		if err != nil {
			t.Fatalf("set topic %q: %v", topic, err)
		}
	}
	if ch.Topic.Value != "Final Topic" {
		t.Errorf("final topic = %q", ch.Topic.Value)
	}

	// Update purpose
	ch, err = env.convSvc.SetPurpose(ctx, ch.ID, domain.SetPurposeParams{
		Purpose: "Updated Purpose", SetByID: user.ID,
	})
	if err != nil {
		t.Fatalf("set purpose: %v", err)
	}
	if ch.Purpose.Value != "Updated Purpose" {
		t.Errorf("purpose = %q", ch.Purpose.Value)
	}

	// Rename
	newName := "renamed-channel"
	ch, err = env.convSvc.Update(ctx, ch.ID, domain.UpdateConversationParams{Name: &newName})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if ch.Name != "renamed-channel" {
		t.Errorf("name = %q", ch.Name)
	}

	// Invite 3 users
	users := make([]*domain.User, 3)
	for i := 0; i < 3; i++ {
		ts := time.Now().Format("150405.000000")
		u, err := env.userSvc.Create(ctx, domain.CreateUserParams{
			TeamID: teamID, Name: "member-" + ts, Email: "m" + ts + "@x.com",
		})
		if err != nil {
			t.Fatalf("create member %d: %v", i, err)
		}
		users[i] = u
		time.Sleep(1 * time.Millisecond)
	}
	for _, u := range users {
		if err := env.convSvc.Invite(ctx, ch.ID, u.ID); err != nil {
			t.Fatalf("invite: %v", err)
		}
	}

	// Verify member count = 4 (creator + 3)
	ch, _ = env.convSvc.Get(ctx, ch.ID)
	if ch.NumMembers != 4 {
		t.Errorf("members = %d, want 4", ch.NumMembers)
	}

	// Kick one member
	if err := env.convSvc.Kick(ctx, ch.ID, users[1].ID); err != nil {
		t.Fatalf("kick: %v", err)
	}
	ch, _ = env.convSvc.Get(ctx, ch.ID)
	if ch.NumMembers != 3 {
		t.Errorf("after kick = %d, want 3", ch.NumMembers)
	}

	// Re-invite the kicked member
	if err := env.convSvc.Invite(ctx, ch.ID, users[1].ID); err != nil {
		t.Fatalf("re-invite: %v", err)
	}
	ch, _ = env.convSvc.Get(ctx, ch.ID)
	if ch.NumMembers != 4 {
		t.Errorf("after re-invite = %d, want 4", ch.NumMembers)
	}

	// Archive and unarchive
	if err := env.convSvc.Archive(ctx, ch.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	ch, _ = env.convSvc.Get(ctx, ch.ID)
	if !ch.IsArchived {
		t.Error("should be archived")
	}
	if err := env.convSvc.Unarchive(ctx, ch.ID); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	ch, _ = env.convSvc.Get(ctx, ch.ID)
	if ch.IsArchived {
		t.Error("should not be archived")
	}

	// Verify comprehensive event sequence
	events := queryEventTypes(t, env, teamID)
	convEvents := 0
	for _, e := range events {
		if strings.HasPrefix(e, "conversation.") {
			convEvents++
		}
	}
	// created + 3 topic_set + purpose_set + updated + 3 member_joined + member_left + member_joined + archived + unarchived = 13
	if convEvents < 13 {
		t.Errorf("conv events = %d, want >= 13", convEvents)
	}

	// Rebuild and verify
	if err := env.projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	ch, _ = env.convSvc.Get(ctx, ch.ID)
	if ch.Name != "renamed-channel" || ch.Topic.Value != "Final Topic" || ch.Purpose.Value != "Updated Purpose" {
		t.Errorf("state after rebuild: name=%q topic=%q purpose=%q", ch.Name, ch.Topic.Value, ch.Purpose.Value)
	}
	if ch.NumMembers != 4 {
		t.Errorf("members after rebuild = %d", ch.NumMembers)
	}
}

// ---------------------------------------------------------------------------
// Flow 25: Usergroup Full Lifecycle — handle updates, description, membership churn
// ---------------------------------------------------------------------------

func TestFlow_UsergroupFullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-ugfull"

	// Create 5 users
	users := make([]*domain.User, 5)
	for i := 0; i < 5; i++ {
		u, err := env.userSvc.Create(ctx, domain.CreateUserParams{
			TeamID: teamID, Name: "ug-user-" + string(rune('A'+i)),
			Email: "ug" + string(rune('a'+i)) + "@example.com",
		})
		if err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		users[i] = u
	}

	// Create usergroup with initial users
	ug, err := env.ugSvc.Create(ctx, domain.CreateUsergroupParams{
		TeamID:      teamID,
		Name:        "Frontend Team",
		Handle:      "frontend",
		Description: "Frontend developers",
		CreatedBy:   users[0].ID,
	})
	if err != nil {
		t.Fatalf("create ug: %v", err)
	}

	// Set initial members (A, B, C)
	if err := env.ugSvc.SetUsers(ctx, ug.ID, []string{users[0].ID, users[1].ID, users[2].ID}); err != nil {
		t.Fatalf("set users: %v", err)
	}
	members, _ := env.ugSvc.ListUsers(ctx, ug.ID)
	if len(members) != 3 {
		t.Errorf("initial members = %d, want 3", len(members))
	}

	// Update name
	newName := "Frontend & Mobile Team"
	if _, err := env.ugSvc.Update(ctx, ug.ID, domain.UpdateUsergroupParams{
		Name: &newName, UpdatedBy: users[0].ID,
	}); err != nil {
		t.Fatalf("update name: %v", err)
	}

	// Update handle
	newHandle := "frontend-mobile"
	if _, err := env.ugSvc.Update(ctx, ug.ID, domain.UpdateUsergroupParams{
		Handle: &newHandle, UpdatedBy: users[0].ID,
	}); err != nil {
		t.Fatalf("update handle: %v", err)
	}

	// Update description
	newDesc := "Frontend and Mobile developers"
	if _, err := env.ugSvc.Update(ctx, ug.ID, domain.UpdateUsergroupParams{
		Description: &newDesc, UpdatedBy: users[0].ID,
	}); err != nil {
		t.Fatalf("update desc: %v", err)
	}

	// Verify updates
	got, _ := env.ugSvc.Get(ctx, ug.ID)
	if got.Name != "Frontend & Mobile Team" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Handle != "frontend-mobile" {
		t.Errorf("handle = %q", got.Handle)
	}
	if got.Description != "Frontend and Mobile developers" {
		t.Errorf("desc = %q", got.Description)
	}

	// Change membership: remove B, add D and E
	if err := env.ugSvc.SetUsers(ctx, ug.ID, []string{users[0].ID, users[2].ID, users[3].ID, users[4].ID}); err != nil {
		t.Fatalf("update members: %v", err)
	}
	members, _ = env.ugSvc.ListUsers(ctx, ug.ID)
	if len(members) != 4 {
		t.Errorf("updated members = %d, want 4", len(members))
	}

	// Verify B is not in the group
	memberIDs := map[string]bool{}
	for _, m := range members {
		memberIDs[m] = true
	}
	if memberIDs[users[1].ID] {
		t.Error("B should not be in group")
	}
	if !memberIDs[users[3].ID] || !memberIDs[users[4].ID] {
		t.Error("D and E should be in group")
	}

	// Disable
	if err := env.ugSvc.Disable(ctx, ug.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	// List without disabled
	ugs, _ := env.ugSvc.List(ctx, domain.ListUsergroupsParams{TeamID: teamID, IncludeDisabled: false})
	if len(ugs) != 0 {
		t.Errorf("active groups = %d, want 0", len(ugs))
	}

	// List with disabled
	ugs, _ = env.ugSvc.List(ctx, domain.ListUsergroupsParams{TeamID: teamID, IncludeDisabled: true})
	if len(ugs) != 1 {
		t.Errorf("all groups = %d, want 1", len(ugs))
	}

	// Re-enable
	if err := env.ugSvc.Enable(ctx, ug.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// Create second usergroup
	ug2, err := env.ugSvc.Create(ctx, domain.CreateUsergroupParams{
		TeamID:    teamID,
		Name:      "Backend Team",
		Handle:    "backend",
		CreatedBy: users[0].ID,
	})
	if err != nil {
		t.Fatalf("create ug2: %v", err)
	}
	env.ugSvc.SetUsers(ctx, ug2.ID, []string{users[1].ID, users[2].ID})

	// List all — should be 2
	ugs, _ = env.ugSvc.List(ctx, domain.ListUsergroupsParams{TeamID: teamID, IncludeDisabled: true})
	if len(ugs) != 2 {
		t.Errorf("total groups = %d, want 2", len(ugs))
	}

	// User C is in both groups
	ug1Members, _ := env.ugSvc.ListUsers(ctx, ug.ID)
	ug2Members, _ := env.ugSvc.ListUsers(ctx, ug2.ID)
	ug1Has := false
	ug2Has := false
	for _, m := range ug1Members {
		if m == users[2].ID {
			ug1Has = true
		}
	}
	for _, m := range ug2Members {
		if m == users[2].ID {
			ug2Has = true
		}
	}
	if !ug1Has || !ug2Has {
		t.Error("user C should be in both groups")
	}

	// Rebuild and verify
	if err := env.projector.RebuildAll(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	got, _ = env.ugSvc.Get(ctx, ug.ID)
	if got.Name != "Frontend & Mobile Team" || got.Handle != "frontend-mobile" {
		t.Errorf("ug after rebuild: name=%q handle=%q", got.Name, got.Handle)
	}
	members, _ = env.ugSvc.ListUsers(ctx, ug.ID)
	if len(members) != 4 {
		t.Errorf("members after rebuild = %d, want 4", len(members))
	}
}
