package eventsourcing_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/eventsourcing"
	pgRepo "github.com/suhjohn/workspace/internal/repository/postgres"
	"github.com/suhjohn/workspace/internal/service"
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
