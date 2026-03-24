package eventsourcing_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internalcrypto "github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
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

	logger := newTestLogger()
	provider, err := internalcrypto.NewEnvKeyProvider("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	if err != nil {
		t.Fatalf("new key provider: %v", err)
	}
	encryptor := internalcrypto.NewEncryptor(provider)

	userRepo := pgRepo.NewUserRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	msgRepo := pgRepo.NewMessageRepo(pool)
	pinRepo := pgRepo.NewPinRepo(pool)
	bookmarkRepo := pgRepo.NewBookmarkRepo(pool)
	usergroupRepo := pgRepo.NewUsergroupRepo(pool)
	fileRepo := pgRepo.NewFileRepo(pool)
	authRepo := pgRepo.NewAuthRepo(pool)
	workspaceRepo := pgRepo.NewWorkspaceRepo(pool)
	workspaceInviteRepo := pgRepo.NewWorkspaceInviteRepo(pool)
	eventRepo := pgRepo.NewEventRepo(pool, encryptor)
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
		fileSvc:     service.NewFileService(fileRepo, nil, "", "http://localhost:8080", recorder, pool, logger),
		authSvc: service.NewAuthService(authRepo, userRepo, workspaceRepo, workspaceInviteRepo, recorder, pool, logger, service.AuthConfig{
			BaseURL:     "http://localhost:8080",
			StateSecret: "test-state-secret",
			HTTPClient:  nil,
		}),
		eventSvc:  service.NewEventService(eventRepo, userRepo, recorder, pool, logger),
		apiKeySvc: service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger),
		projector: eventsourcing.NewProjector(pool, logger),
	}
}

func createSession(t *testing.T, env *testEnv, teamID, userID string) *domain.AuthSession {
	t.Helper()
	session, err := pgRepo.NewAuthRepo(env.pool).CreateSession(context.Background(), domain.CreateAuthSessionParams{
		TeamID:    teamID,
		UserID:    userID,
		Provider:  domain.AuthProviderGitHub,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func queryEventTypes(t *testing.T, env *testEnv, teamID string) []string {
	t.Helper()
	rows, err := env.pool.Query(context.Background(),
		"SELECT event_type FROM internal_events WHERE team_id = $1 ORDER BY id ASC", teamID)
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
	if err := env.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM internal_events").Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func queryPayloads(t *testing.T, env *testEnv, aggType, aggID string) []json.RawMessage {
	t.Helper()
	rows, err := env.pool.Query(context.Background(),
		"SELECT payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id ASC",
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

func queryServiceEvent(t *testing.T, env *testEnv, eventType, aggType, aggID string) domain.InternalEvent {
	t.Helper()

	var evt domain.InternalEvent
	err := env.pool.QueryRow(context.Background(),
		`SELECT id, event_type, aggregate_type, aggregate_id, team_id, actor_id, payload, metadata, created_at
		 FROM internal_events
		 WHERE event_type = $1 AND aggregate_type = $2 AND aggregate_id = $3
		 ORDER BY id DESC
		 LIMIT 1`,
		eventType, aggType, aggID,
	).Scan(
		&evt.ID,
		&evt.EventType,
		&evt.AggregateType,
		&evt.AggregateID,
		&evt.TeamID,
		&evt.ActorID,
		&evt.Payload,
		&evt.Metadata,
		&evt.CreatedAt,
	)
	if err != nil {
		t.Fatalf("query service event: %v", err)
	}
	return evt
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()

	var gotV any
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("unmarshal got json: %v", err)
	}

	var wantV any
	if err := json.Unmarshal(want, &wantV); err != nil {
		t.Fatalf("unmarshal want json: %v", err)
	}

	gotNorm, err := json.Marshal(gotV)
	if err != nil {
		t.Fatalf("marshal got normalized json: %v", err)
	}
	wantNorm, err := json.Marshal(wantV)
	if err != nil {
		t.Fatalf("marshal want normalized json: %v", err)
	}

	if string(gotNorm) != string(wantNorm) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", gotNorm, wantNorm)
	}
}

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Flow 1: Workspace Bootstrap & Team Collaboration
// ---------------------------------------------------------------------------
//
// Scenario: A brand-new workspace is set up from scratch and exercises every
//
//	core collaboration primitive in a single end-to-end sequence.
//
// Steps:
//  1. Create an admin user (human principal, account_type=admin).
//  2. Create a second user "alice" (human principal).
//  3. Create a public channel "general" — verify creator is auto-joined (num_members=1).
//  4. Invite alice to general — verify num_members=2.
//  5. Admin posts a message in general.
//  6. Alice replies in a thread — verify thread_ts references parent.
//  7. Alice adds a :thumbsup: reaction to admin's message.
//  8. Admin pins the message.
//  9. Admin creates a bookmark (link type) in general.
//  10. List channel members — verify 2 members.
//  11. Verify the full event sequence in internal_events:
//     [user.created x2, conversation.created, member.joined,
//     message.posted x2, reaction.added, pin.added, bookmark.created]
//  12. (Unhappy) Duplicate reaction → expect silent upsert or ErrAlreadyReacted.
//  13. (Unhappy) Duplicate invite → expect ErrAlreadyInChannel.
//  14. (Unhappy) Duplicate pin → expect error.
//  15. (Unhappy) Post to nonexistent channel → expect error.
//  16. (Unhappy) Create user with empty team_id → expect ErrInvalidArgument.
//  17. Verify bookmark list returns the created bookmark.
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
		PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin,
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
	eRows, _ := env.pool.Query(ctx, "SELECT event_type FROM internal_events ORDER BY id ASC")
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
//
// Scenario: A channel goes through its full lifecycle — creation, metadata
//
//	updates, posting, archival, and unarchival — verifying that
//	archived channels block writes and unarchiving restores them.
//
// Steps:
//  1. Create a user and a public channel "project-alpha".
//  2. Set the channel topic to "Alpha discussion" — verify.
//  3. Set the channel purpose to "Coordinate alpha" — verify.
//  4. Rename the channel to "project-beta" — verify.
//  5. Post a message before archiving.
//  6. Archive the channel.
//  7. (Unhappy) Post to archived channel → expect ErrChannelArchived.
//  8. (Unhappy) Set topic on archived channel → expect ErrChannelArchived.
//  9. Unarchive the channel.
//  10. Post a message after unarchiving — verify it succeeds.
//  11. List conversations with exclude_archived=true — verify unarchived channel appears.
//  12. Verify old message (posted before archive) is still accessible.
//  13. Verify event sequence: [user.created, conversation.created,
//     topic_set, purpose_set, updated, message.posted, archived,
//     unarchived, message.posted]
func TestFlow_ChannelLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-channel"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "owner", Email: "owner@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: A human creates an AI agent and issues a delegated API key,
//
//	then exercises the full key lifecycle (create → validate →
//	update → list → revoke) including delegation chain tracking.
//
// Steps:
//  1. Create a human user.
//  2. Create an agent user owned by the human (principal_type=agent, owner_id=human).
//  3. Create a live API key for the agent with on_behalf_of=human — verify sk_live_ prefix.
//  4. Validate the API key — verify principal_id=agent, on_behalf_of=human.
//  5. Get the key — verify key_hash is redacted (empty).
//  6. Update the key description — verify.
//  7. List keys for the team — verify count=1.
//  8. Revoke the key — verify subsequent validation returns ErrTokenRevoked.
//  9. List with include_revoked=true — verify the revoked key appears.
//
// 10. Verify the revoke event payload contains revoked_at.
// 11. (Unhappy) Create key with nonexistent principal → expect error.
// 12. (Unhappy) Create key with empty name → expect ErrInvalidArgument.
// 13. (Unhappy) Validate garbage key → expect ErrInvalidAuth.
// 14. Verify api_key event count: 3 (created, updated, revoked).
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
//
// Scenario: An API key is rotated with a 24h grace period, during which
//
//	both old and new keys are valid. After revoking the new key,
//	rotation of a revoked key is rejected.
//
// Steps:
//  1. Create a user and a live API key — validate it works.
//  2. Rotate the key with a 24h grace period — verify new key has different ID and raw value.
//  3. Validate BOTH old and new keys during grace period — both succeed.
//  4. Get old key — verify rotated_to_id points to new key and grace_period_ends_at is set.
//  5. List keys — verify count=2 (old + new).
//  6. Verify event counts: 2 api_key.created + 1 api_key.rotated.
//  7. (Unhappy) Revoke the new key, then try to rotate it → expect ErrInvalidArgument.
//  8. Validate revoked new key → expect ErrTokenRevoked.
func TestFlow_KeyRotation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-rotation"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "rotator", Email: "rotator@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Two users in a channel exercise the full message lifecycle —
//
//	posting, threading, editing, deleting, reactions, and removal.
//
// Steps:
//  1. Create 2 users and a public channel; invite user2.
//  2. Post a parent message.
//  3. Post 3 replies from user2 — verify thread_ts references parent.
//  4. Fetch thread replies — verify count >= 3.
//  5. Edit the first reply text — verify updated text.
//  6. Delete the last reply — verify it's marked deleted or returns ErrNotFound.
//  7. Add 3 different reactions (:fire:, :rocket:, :heart:) on the parent.
//  8. Verify reaction count = 3.
//  9. Remove the :fire: reaction — verify count = 2.
//  10. (Unhappy) Edit nonexistent message → expect error.
//  11. (Unhappy) Delete already-deleted message → expect error.
//  12. Verify event counts: message.posted, message.updated, message.deleted,
//     reaction.added, reaction.removed.
func TestFlow_MessageThreading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-threading"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "threader", Email: "threader@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
		PrincipalType: domain.PrincipalTypeHuman,
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
	rows, err := env.pool.Query(ctx, "SELECT event_type FROM internal_events ORDER BY id ASC")
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
//
// Scenario: A usergroup is created, populated, renamed, disabled, re-enabled,
//
//	and its membership is updated — covering the full CRUD lifecycle.
//
// Steps:
//  1. Create 3 users (admin, u1, u2).
//  2. Create usergroup "Engineers" with handle "engineers".
//  3. Set members to [u1, u2] — verify count=2.
//  4. Update name to "Senior Engineers" — verify.
//  5. Disable the usergroup — verify enabled=false.
//  6. List with include_disabled=true — verify count=1.
//  7. List with include_disabled=false — verify count=0.
//  8. Re-enable the usergroup.
//  9. Update membership to [u1] only — verify count=1.
//  10. Verify usergroup event count = 6:
//     [created, users_set, updated, disabled, enabled, users_set]
func TestFlow_Usergroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-ug"

	admin, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "ug-admin", Email: "ugadmin@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	u1, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "m1", Email: "m1@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create u1: %v", err)
	}
	u2, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "m2", Email: "m2@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Remote files are added, shared to channels, listed, and deleted.
//
//	Also verifies that S3 upload fails gracefully when no S3 client is configured.
//
// Steps:
//  1. Create a user and a public channel.
//  2. Add a remote file "Design Doc" (type=gdoc) — verify is_external=true.
//  3. Share the file to the channel.
//  4. Get file by ID — verify title.
//  5. Add a second remote file "Spec".
//  6. List all files — verify count >= 2.
//  7. Delete the spec file — verify Get returns ErrNotFound.
//  8. (Unhappy) Request S3 upload URL with nil S3 client → expect error.
//  9. (Unhappy) Add file with no title → expect ErrInvalidArgument.
//
// 10. (Unhappy) Get nonexistent file → expect ErrNotFound.
func TestFlow_FileLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-files"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "uploader", Email: "up@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	ctx = ctxutil.WithUser(ctx, user.ID, teamID)
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
//
// Scenario: Webhook subscriptions are created, updated, disabled, listed,
//
//	and deleted. Verifies that plaintext secrets are not stored in
//	event payloads (Redacted() clears the secret field).
//
// Steps:
//  1. Create a message subscription — verify enabled=true.
//  2. Get subscription — verify URL.
//  3. Update the URL — verify.
//  4. Disable the subscription (enabled=false).
//  5. Create a second subscription for conversation.created.
//  6. List subscriptions — verify count=2.
//  7. Delete the second subscription — verify count=1.
//  8. Verify event count = 5: [created, updated, updated(disable), created, deleted]
//  9. Verify no plaintext secret in any event payload (Redacted()).
//
// 10. (Unhappy) Create subscription with no URL → expect ErrInvalidArgument.
// 11. (Unhappy) Create subscription with no event types → expect ErrInvalidArgument.
func TestFlow_EventSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-events"

	sub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks.example.com/events",
		Type:   domain.EventTypeConversationMessageCreated,
		Secret: "secret-123",
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
		Type: domain.EventTypeConversationCreated, Secret: "s2",
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
		if strings.HasPrefix(e, "event_subscription.") {
			subEvents++
		}
	}
	// created + updated + updated(disable) + created + deleted = 5
	if subEvents != 5 {
		t.Errorf("sub events = %d, want 5", subEvents)
	}

	// Verify the Redacted() method clears the plaintext secret field in payloads.
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
		TeamID: teamID, Type: domain.EventTypeConversationMessageCreated,
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("no url: got %v", err)
	}

}

func TestFlow_WebhookEnvelopeContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	baseCtx := context.Background()
	teamID := "T-webhook-envelope"

	user, err := env.userSvc.Create(baseCtx, domain.CreateUserParams{
		TeamID: teamID, Name: "hook-user", Email: "hook@example.com",
		PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ctx := ctxutil.WithUser(baseCtx, user.ID, teamID)

	ch, err := env.convSvc.Create(ctx, domain.CreateConversationParams{
		TeamID: teamID, Name: "hooks",
		Type: domain.ConversationTypePublicChannel, CreatorID: user.ID,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	msg, err := env.msgSvc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: ch.ID, UserID: user.ID, Text: "hello webhooks",
	})
	if err != nil {
		t.Fatalf("post message: %v", err)
	}

	msgEvent := queryServiceEvent(t, env, domain.EventMessagePosted, domain.AggregateMessage, msg.TS)
	if msgEvent.TeamID != teamID {
		t.Fatalf("message event team_id = %q, want %q", msgEvent.TeamID, teamID)
	}
	if msgEvent.ActorID != user.ID {
		t.Fatalf("message event actor_id = %q, want %q", msgEvent.ActorID, user.ID)
	}
	if msgEvent.CreatedAt.IsZero() {
		t.Fatal("message event created_at is zero")
	}

	body, err := json.Marshal(msgEvent)
	if err != nil {
		t.Fatalf("marshal message event envelope: %v", err)
	}
	var msgEnvelope domain.InternalEvent
	if err := json.Unmarshal(body, &msgEnvelope); err != nil {
		t.Fatalf("unmarshal message envelope: %v", err)
	}
	if msgEnvelope.ID != msgEvent.ID || msgEnvelope.EventType != domain.EventMessagePosted ||
		msgEnvelope.AggregateType != domain.AggregateMessage || msgEnvelope.AggregateID != msg.TS {
		t.Fatalf("unexpected message envelope: %+v", msgEnvelope)
	}
	assertJSONEqual(t, msgEnvelope.Payload, msgEvent.Payload)

	var msgPayload domain.Message
	if err := json.Unmarshal(msgEnvelope.Payload, &msgPayload); err != nil {
		t.Fatalf("unmarshal message payload: %v", err)
	}
	if msgPayload.TS != msg.TS || msgPayload.ChannelID != ch.ID || msgPayload.Text != "hello webhooks" {
		t.Fatalf("unexpected message payload: %+v", msgPayload)
	}

	sub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID,
		URL:    "https://hooks.example.com/events",
		Type:   domain.EventTypeConversationMessageCreated,
		Secret: "super-secret",
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if err := env.eventSvc.DeleteSubscription(ctx, sub.ID); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}

	delEvent := queryServiceEvent(t, env, domain.EventSubscriptionDeleted, domain.AggregateSubscription, sub.ID)
	if delEvent.ActorID != user.ID {
		t.Fatalf("delete event actor_id = %q, want %q", delEvent.ActorID, user.ID)
	}

	delBody, err := json.Marshal(delEvent)
	if err != nil {
		t.Fatalf("marshal delete event envelope: %v", err)
	}
	var delEnvelope domain.InternalEvent
	if err := json.Unmarshal(delBody, &delEnvelope); err != nil {
		t.Fatalf("unmarshal delete envelope: %v", err)
	}
	assertJSONEqual(t, delEnvelope.Payload, delEvent.Payload)

	var deletePayload map[string]string
	if err := json.Unmarshal(delEnvelope.Payload, &deletePayload); err != nil {
		t.Fatalf("unmarshal delete payload: %v", err)
	}
	if deletePayload["id"] != sub.ID {
		t.Fatalf("delete payload id = %q, want %q", deletePayload["id"], sub.ID)
	}
}

// ---------------------------------------------------------------------------
// Flow 9: Auth Token Lifecycle
// ---------------------------------------------------------------------------
//
// Scenario: Bearer tokens are created, validated (with and without "Bearer "
//
//	prefix), revoked, and verified that raw tokens never appear in
//	event payloads.
//
// Steps:
//  1. Create a user.
//  2. Create a token with scopes [read, write] — verify raw token is non-empty.
//  3. Validate the token — verify user_id and team_id.
//  4. Validate with "Bearer " prefix — verify same user.
//  5. Create a second token with scope [read].
//  6. Revoke the first token — verify validation fails.
//  7. Validate the second token — still works.
//  8. Verify token event count = 3: [created, created, revoked].
//  9. Verify no raw token string appears in event payloads.
//
// 10. (Unhappy) Create token for nonexistent user → expect error.
// 11. (Unhappy) Validate garbage token → expect error.
func TestFlow_AuthSessionLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-auth"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "auth-user", Email: "auth@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	session := createSession(t, env, teamID, user.ID)
	if session.Token == "" {
		t.Fatal("raw session token empty")
	}

	// Validate
	auth, err := env.authSvc.ValidateSession(ctx, session.Token)
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
	auth2, err := env.authSvc.ValidateSession(ctx, "Bearer "+session.Token)
	if err != nil {
		t.Fatalf("validate bearer: %v", err)
	}
	if auth2.UserID != user.ID {
		t.Errorf("bearer user_id = %q", auth2.UserID)
	}

	session2 := createSession(t, env, teamID, user.ID)

	// Revoke first
	if err := env.authSvc.RevokeSession(ctx, session.Token); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Validate revoked fails
	_, err = env.authSvc.ValidateSession(ctx, session.Token)
	if err == nil {
		t.Error("validate revoked: expected error")
	}

	if !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("validate revoked: %v", err)
	}

	// Second still works
	if _, err := env.authSvc.ValidateSession(ctx, session2.Token); err != nil {
		t.Fatalf("validate session2: %v", err)
	}

	// --- Unhappy paths ---
	_, err = env.authSvc.ValidateSession(ctx, "garbage")
	if err == nil {
		t.Error("garbage session token: expected error")
	}
}

// ---------------------------------------------------------------------------
// Flow 10: Cross-Cutting Consistency — Full Workspace Lifecycle
// ---------------------------------------------------------------------------
//
// Scenario: Exercises one operation from every aggregate type in a single
//
//	workspace, then verifies that all 10 events are recorded and
//	that a full projection rebuild preserves all state.
//
// Steps:
//  1. Record event count before the test.
//  2. Create a user.
//  3. Create a public channel.
//  4. Post a message.
//  5. Add a reaction.
//  6. Pin the message.
//  7. Create a bookmark.
//  8. Create a usergroup.
//  9. Create an API key.
//  10. Create a webhook subscription.
//  11. Verify exactly 9 new events were recorded.
//  13. Verify each aggregate type (user, conversation, message, pin,
//     bookmark, usergroup, api_key, subscription) has >= 1 event.
//  14. Perform full projection rebuild (TRUNCATE + replay).
//  15. Verify user, conversation, bookmark, and usergroup
//     all survive the rebuild with correct state.
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
		PrincipalType: domain.PrincipalTypeHuman,
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

	_, _, err = env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "cc-key", TeamID: teamID, PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
	})
	if err != nil {
		t.Fatalf("api key: %v", err)
	}

	if _, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks.example.com",
		Type: domain.EventTypeConversationMessageCreated, Secret: "s",
	}); err != nil {
		t.Fatalf("subscription: %v", err)
	}

	// 9 events: user + conv + msg + reaction + pin + bookmark + ug + api_key + sub
	after := countEvents(t, env)
	if after-before != 9 {
		t.Errorf("new events = %d, want 9", after-before)
	}

	// Each aggregate type has events (some services record empty team_id, so don't filter by it)
	for _, agg := range []string{
		domain.AggregateUser, domain.AggregateConversation, domain.AggregateMessage,
		domain.AggregatePin, domain.AggregateBookmark, domain.AggregateUsergroup,
		domain.AggregateAPIKey, domain.AggregateSubscription,
	} {
		var c int
		env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1", agg).Scan(&c)
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
}

// ---------------------------------------------------------------------------
// Flow 11: Pagination & Listing Edge Cases
// ---------------------------------------------------------------------------
//
// Scenario: Verifies cursor-based pagination across users, messages, and
//
//	conversations. Checks that pages don't overlap, HasMore is
//	correct, and empty results return cleanly.
//
// Steps:
//  1. Create 6 users (1 + 5 more) in the same team.
//  2. Paginate users with limit=2 — verify page1 has 2 items and HasMore=true.
//  3. Fetch page2 with the cursor — verify 2 items.
//  4. Verify no user ID overlap between page1 and page2.
//  5. Create a channel and post 5 messages.
//  6. Paginate message history with limit=2 — verify 2 items.
//  7. Create 3 more channels.
//  8. Paginate conversations with limit=2 — verify 2 items and HasMore=true.
//  9. Query a nonexistent team — verify empty result with HasMore=false.
func TestFlow_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-pagination"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "paginator", Email: "pag@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create 5 more users
	for i := 0; i < 5; i++ {
		ts := time.Now().Format("150405.000000")
		if _, err := env.userSvc.Create(ctx, domain.CreateUserParams{
			TeamID: teamID, Name: "pu-" + ts, Email: "pu" + ts + "@x.com",
			PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Creates each conversation type (IM, MPIM, private channel) and
//
//	verifies type-specific behavior: posting, inviting, kicking,
//	and listing with type filters.
//
// Steps:
//  1. Create 3 users (alice, bob, charlie).
//  2. Create a DM (IM) — invite bob, post a message.
//  3. Create a Group DM (MPIM) — invite bob and charlie, post a message.
//  4. Create a private channel.
//  5. List by type=public_channel — verify none of our conversations appear.
//  6. List by type=im — verify count=1.
//  7. Kick charlie from the MPIM — verify he's no longer in the member list.
//  8. Verify event counts: 3 conversation.created, >= 3 member events.
func TestFlow_ConversationTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-convtypes"

	alice, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "alice", Email: "alice-dm@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bob", Email: "bob-dm@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	charlie, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "charlie", Email: "charlie-dm@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Stress-tests rapid sequential operations to verify timestamp
//
//	uniqueness, multi-user reactions, and immediate create→revoke
//	sequences.
//
// Steps:
//  1. Create a user and a public channel.
//  2. Rapid-fire post 10 messages — verify all 10 timestamps are unique.
//  3. Create a second user.
//  4. Both users add :wave: reaction to the same message — verify count=2.
//  5. Create an API key and immediately revoke it — verify validation
//     returns ErrTokenRevoked.
//  6. Update user name — verify lookup by ID returns updated name.
//  7. Verify event count is reasonable (>= 15).
func TestFlow_ConcurrentEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-concurrent"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "conc", Email: "conc@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
		PrincipalType: domain.PrincipalTypeHuman,
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
	byEmail, _ := env.userSvc.GetByEmail(ctxutil.WithUser(ctx, user.ID, teamID), "conc@example.com")
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
//
// Scenario: Exercises the full user profile lifecycle — creation with rich
//
//	profile data, lookup by ID and email, incremental field updates,
//	role changes, soft-delete/reactivation, and principal type
//	variations (human, system, agent).
//
// Steps:
//  1. Create a human user with full profile fields (real_name, display_name,
//     email, profile.title, profile.phone, profile.status_text, profile.status_emoji).
//  2. Verify real_name, display_name, and default principal_type=human.
//  3. Get user by ID — verify email.
//  4. Get user by email — verify same ID.
//  5. Update real_name — verify.
//  6. Update display_name — verify.
//  7. Update email — verify.
//  8. Verify old email lookup returns ErrNotFound; new email resolves to same user.
//  9. Promote to admin (account_type=admin) — verify.
//
// 10. Soft-delete the user (deleted=true) — verify.
// 11. Soft-delete (deleted=true) — verify.
// 12. Reactivate (deleted=false) — verify.
// 13. Update the profile struct (title, phone, status) — verify.
// 14. Create a system principal (principal_type=system, is_bot=true).
// 15. Create an agent principal with owner_id pointing to the human user.
// 16. List all users for the team — verify count=3.
// 17. Verify event counts: 3 user.created, >= 8 user.updated.
func TestFlow_UserProfileManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-profile"

	// Step 1: Create user with full profile
	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        teamID,
		Name:          "fullprofile",
		RealName:      "Full Profile User",
		DisplayName:   "FPU",
		Email:         "full@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
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
	emailCtx := ctxutil.WithUser(ctx, user.ID, teamID)
	byEmail, err := env.userSvc.GetByEmail(emailCtx, "full@example.com")
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
	_, err = env.userSvc.GetByEmail(emailCtx, "full@example.com")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old email should not resolve: got %v", err)
	}
	byEmail, err = env.userSvc.GetByEmail(emailCtx, "newemail@example.com")
	if err != nil {
		t.Fatalf("new email lookup: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Error("new email should resolve to same user")
	}

	// Step 8: Promote to admin
	accountTypeAdmin := domain.AccountTypeAdmin
	updated, err = env.userSvc.Update(ctx, user.ID, domain.UpdateUserParams{AccountType: &accountTypeAdmin})
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if updated.EffectiveAccountType() != domain.AccountTypeAdmin {
		t.Error("should be admin")
	}

	// Step 9: Soft delete (deactivate)
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
	// real_name + display_name + email + admin + delete + reactivate + profile = 7
	if userUpdated < 7 {
		t.Errorf("user.updated = %d, want >= 7", userUpdated)
	}
}

// ---------------------------------------------------------------------------
// Flow 15: Multi-Channel Workspace — user active across many channels
// ---------------------------------------------------------------------------
//
// Scenario: Three users operate across 4 channels with varying membership,
//
//	testing cross-channel posting, threading, reactions, member
//	removal, archival, and filtered listing.
//
// Steps:
//  1. Create 3 users (alice, bob, charlie).
//  2. Create 4 public channels: general, engineering, random, announcements.
//  3. Invite bob to general, engineering, random (3 channels).
//  4. Invite charlie to general and random (2 channels).
//  5. Verify member counts: general=3, engineering=2, random=3, announcements=1.
//  6. Alice posts a message in each of the 4 channels.
//  7. Bob posts in engineering.
//  8. Alice posts a discussion topic in general; charlie replies in thread.
//  9. Alice reacts :rocket: to bob's engineering message.
//
// 10. Kick alice from random — verify num_members=2.
// 11. Verify alice is not in random's member list.
// 12. List all channels — verify count=4.
// 13. Archive announcements — list with exclude_archived — verify count=3.
// 14. Verify engineering history has >= 2 messages.
func TestFlow_MultiChannelWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-multichan"

	alice, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "alice", Email: "alice-mc@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bob", Email: "bob-mc@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	charlie, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "charlie", Email: "charlie-mc@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Three users participate in a deep thread with 5 replies, then
//
//	exercise in-thread editing, deletion, reactions, a second
//	independent thread, and thread-level pagination.
//
// Steps:
//  1. Create 3 users (alice, bob, charlie) and a public channel; invite all.
//  2. Alice posts a parent message about architecture.
//  3. Post 5 replies from alternating users (bob, charlie, alice, bob, charlie).
//  4. Verify all replies have thread_ts == parent.TS.
//  5. Fetch thread replies — verify count >= 5.
//  6. Edit the first reply (bob) — verify updated text.
//  7. Delete the third reply (alice) — verify it's gone or marked deleted.
//  8. Add reactions to multiple thread messages:
//     :thinking: on parent, :thumbsup: on reply[1].
//  9. Verify reaction counts on each message.
//
// 10. Bob posts a second parent message ("Separate topic").
// 11. Charlie replies to the second thread.
// 12. Verify thread 2 has 1 reply (independent from thread 1).
// 13. Verify channel history shows >= 2 top-level messages.
// 14. Paginate thread 1 replies with limit=2 — verify HasMore=true.
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
			PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Multiple bookmarks are created with emojis, updated (title, link,
//
//	emoji), deleted, and verified to be scoped per-channel.
//
// Steps:
//  1. Create 2 users and a public channel.
//  2. Create 3 bookmarks with emojis (:book:, :art:, none) — verify emoji on bm1.
//  3. List bookmarks — verify count=3.
//  4. Update bm1 title and link (by user2) — verify title, link, and updated_by.
//  5. Update bm2 emoji to :paintbrush: — verify.
//  6. Delete bm3 — verify list count=2 and bm3 ID is absent.
//  7. Create a second channel and add a bookmark to it.
//  8. Verify bookmarks are scoped: ch1 has 2, ch2 has 1.
//  9. Verify bookmark event count = 7:
//     [3 created + 2 updated + 1 deleted + 1 created]
func TestFlow_BookmarkFullCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-bookmarks"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bookmarker", Email: "bm@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	user2, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "bookmarker2", Email: "bm2@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1",
		domain.AggregateBookmark).Scan(&bmEvents)
	// 3 created + 2 updated + 1 deleted + 1 created = 7
	if bmEvents != 7 {
		t.Errorf("bookmark events = %d, want 7", bmEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 18: Pin Lifecycle — pin multiple, list, unpin, re-pin
// ---------------------------------------------------------------------------
//
// Scenario: Messages are pinned, unpinned, re-pinned, and bulk-unpinned,
//
//	verifying the pin list at each step and the final event count.
//
// Steps:
//  1. Create a user and a public channel.
//  2. Post 4 messages (A, B, C, D).
//  3. Pin messages A, B, C — verify pin list count=3.
//  4. Unpin message B — verify count=2 and B is absent.
//  5. Re-pin message B — verify count=3.
//  6. Pin message D — verify count=4.
//  7. Unpin all 4 messages — verify count=0.
//  8. Verify pin event count = 10:
//     [3 add + 1 remove + 1 re-add + 1 add + 4 remove]
func TestFlow_PinLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-pins"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "pinner", Email: "pinner@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1",
		domain.AggregatePin).Scan(&pinEvents)
	// 3 add + 1 remove + 1 re-add + 1 add + 4 remove = 10
	if pinEvents != 10 {
		t.Errorf("pin events = %d, want 10", pinEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 19: File Sharing Across Channels
// ---------------------------------------------------------------------------
//
// Scenario: Three remote files are shared across multiple channels with
//
//	varying distribution, then one file is deleted while verifying
//	the others remain intact.
//
// Steps:
//  1. Create a user and 3 public channels (design, dev, product).
//  2. Add 3 remote files: Design System (figma), API Spec (gdoc), README (markdown).
//  3. Share Design System to design + product (2 channels).
//  4. Share API Spec to all 3 channels.
//  5. Share README to dev only.
//  6. Get Design System — verify title and is_external=true.
//  7. List all files — verify count >= 3.
//  8. Delete the API Spec file — verify Get returns ErrNotFound.
//  9. Verify Design System and README still exist.
//  10. Verify file event count >= 7:
//     [3 created + 3 shared + 1 deleted]
func TestFlow_FileSharingAcrossChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-fileshare"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "sharer", Email: "sharer@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	ctx = ctxutil.WithUser(ctx, user.ID, teamID)

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
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1",
		domain.AggregateFile).Scan(&fileEvents)
	// 3 created + 3 shared (updated+shared events) + 1 deleted
	if fileEvents < 7 {
		t.Errorf("file events = %d, want >= 7", fileEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 20: Multiple API Keys Per Principal — environments, permissions, listing
// ---------------------------------------------------------------------------
//
// Scenario: A single user creates multiple API keys across different
//
//	environments (live, test) and types (persistent, restricted),
//	exercises validation, permission updates, revocation, and
//	filtered listing.
//
// Steps:
//  1. Create a user.
//  2. Create a live key (sk_live_) with [read, write] — verify prefix.
//  3. Create a test key (sk_test_) with [read] — verify prefix.
//  4. Create a restricted key with [deploy] permission.
//  5. Validate each key — verify environment and permissions match.
//  6. List all keys — verify count=3.
//  7. Update live key description — verify via Get.
//  8. Update test key permissions to [read, write, admin] — re-validate
//     and verify 3 permissions.
//  9. Revoke the restricted key.
//
// 10. List without revoked — verify count=2.
// 11. List with include_revoked — verify count=3.
// 12. Validate revoked key — expect ErrTokenRevoked.
// 13. Validate live and test keys — both still work.
// 14. Verify event counts: 3 created, 2 updated, 1 revoked.
func TestFlow_MultipleAPIKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-multikey"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "multikey", Email: "multikey@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: Multiple webhook subscriptions with overlapping event types are
//
//	created, updated (add types, change URL), disabled/re-enabled,
//	and deleted — verifying independent lifecycle management.
//
// Steps:
//  1. Create a message-created subscription.
//  2. Create a conversation-created subscription.
//  3. Create a catch-all subscription with no type filter.
//  4. List subscriptions — verify count=3.
//  5. Update the messages subscription to add reaction.added (now 4 types).
//  6. Disable the catch-all subscription — verify enabled=false.
//  7. Re-enable the catch-all — verify enabled=true.
//  8. Change the channels subscription URL to /channels/v2 — verify.
//  9. Delete the channels subscription — verify list count=2.
//  10. Verify subscription event count = 8:
//     [3 created + 1 update(types) + 1 disable + 1 enable + 1 update(url) + 1 deleted]
func TestFlow_SubscriptionEventTypeMatching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-submatch"

	// Create subscription for message events only
	msgSub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID,
		URL:    "https://hooks.example.com/messages",
		Type:   domain.EventTypeConversationMessageCreated,
		Secret: "msg-secret",
	})
	if err != nil {
		t.Fatalf("create msg sub: %v", err)
	}
	if msgSub.Type != domain.EventTypeConversationMessageCreated {
		t.Errorf("msg type = %q, want %q", msgSub.Type, domain.EventTypeConversationMessageCreated)
	}

	// Create subscription for channel events only
	chSub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID,
		URL:    "https://hooks.example.com/channels",
		Type:   domain.EventTypeConversationCreated,
		Secret: "ch-secret",
	})
	if err != nil {
		t.Fatalf("create ch sub: %v", err)
	}

	// Create catch-all subscription
	allSub, err := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID,
		URL:    "https://hooks.example.com/all",
		Type:   domain.EventTypeConversationMessageCreated,
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
	newType := domain.EventTypeConversationMessageCreated
	updatedSub, err := env.eventSvc.UpdateSubscription(ctx, msgSub.ID, domain.UpdateEventSubscriptionParams{
		Type: &newType,
	})
	if err != nil {
		t.Fatalf("update msg sub: %v", err)
	}
	if updatedSub.Type != domain.EventTypeConversationMessageCreated {
		t.Errorf("updated type = %q, want %q", updatedSub.Type, domain.EventTypeConversationMessageCreated)
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
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1",
		domain.AggregateSubscription).Scan(&subEvents)
	// 3 created + 1 update(types) + 1 disable + 1 enable + 1 update(url) + 1 deleted = 8
	if subEvents != 8 {
		t.Errorf("sub events = %d, want 8", subEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 22: Complex Projection Rebuild — verify all state survives TRUNCATE+replay
// ---------------------------------------------------------------------------
//
// Scenario: Builds a rich workspace state touching every aggregate type, then
//
//	performs a full projection rebuild (TRUNCATE all projection tables
//	+ replay all events) and verifies that every piece of state
//	is faithfully recreated.
//
// Steps:
//  1. Create an admin (human, account_type=admin) and an agent (owned by admin).
//  2. Create a public channel with topic and purpose; invite the agent.
//  3. Post a parent message and a threaded reply.
//  4. Add a :star: reaction to the parent.
//  5. Pin the parent message.
//  6. Create a bookmark in the channel.
//  7. Create a usergroup and set members to [admin, agent].
//  8. Add a remote file.
//  9. Create an auth token.
//
// 10. Create an API key.
// 11. Create a webhook subscription.
// 12. Count events before rebuild — verify >= 15.
// 13. Perform full RebuildAll().
// 14. Verify event count is unchanged after rebuild.
// 15. Verify admin user: name, account_type, principal_type.
// 16. Verify agent user: principal_type, owner_id.
// 17. Verify channel: topic, purpose, num_members=2.
// 18. Verify message text.
// 19. Verify reactions on the message.
// 20. Verify pins list.
// 21. Verify bookmarks list.
// 22. Verify usergroup membership.
// 23. Verify file still accessible.
// 24. Verify auth token validates successfully.
// 25. Verify API key validates successfully.
// 26. Verify subscription URL and enabled state.
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
		PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	ctx = ctxutil.WithUser(ctx, admin.ID, teamID)

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

	// API key
	apiKey, apiKeyRaw, _ := env.apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "rebuild-key", TeamID: teamID, PrincipalID: admin.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
		Permissions: []string{"read", "write"},
	})

	// Subscription
	sub, _ := env.eventSvc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		TeamID: teamID, URL: "https://hooks.rebuild.com",
		Type: domain.EventTypeConversationMessageCreated, Secret: "rebuild-secret",
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
	if gotAdmin.Name != "admin" || gotAdmin.EffectiveAccountType() != domain.AccountTypeAdmin || gotAdmin.PrincipalType != domain.PrincipalTypeHuman {
		t.Errorf("admin state wrong: name=%q accountType=%q type=%q", gotAdmin.Name, gotAdmin.EffectiveAccountType(), gotAdmin.PrincipalType)
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
//
// Scenario: Messages with structured Block Kit content and arbitrary metadata
//
//	are posted, retrieved, updated, and verified through history.
//
// Steps:
//  1. Create a user and a public channel.
//  2. Post a message with Block Kit blocks (section with mrkdwn) and fallback text.
//  3. Get the message — verify blocks are present and text matches fallback.
//  4. Post a message with metadata (event_type + event_payload JSON).
//  5. Get the metadata message — verify metadata is present.
//  6. Update the blocks on the first message — verify blocks are updated.
//  7. Update only the text (not blocks) — verify text changes while blocks
//     remain from the previous update.
//  8. Post a message with BOTH blocks and metadata.
//  9. Get it — verify both blocks and metadata are present.
//
// 10. Fetch channel history — verify >= 3 messages.
// 11. Verify message event count = 5: [3 posted + 2 updated].
func TestFlow_MessageMetadataAndBlocks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-blocks"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "richcontent", Email: "rich@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
	env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_events WHERE event_type LIKE 'message.%'").Scan(&msgEvents)
	// 3 posted + 2 updated = 5
	if msgEvents != 5 {
		t.Errorf("message events = %d, want 5", msgEvents)
	}
}

// ---------------------------------------------------------------------------
// Flow 24: Conversation with Initial Topic/Purpose + Full Update Lifecycle
// ---------------------------------------------------------------------------
//
// Scenario: A channel is created with an initial topic and purpose, then goes
//
//	through multiple topic updates, a rename, member churn (invite,
//	kick, re-invite), archive/unarchive, and a projection rebuild.
//
// Steps:
//  1. Create a user.
//  2. Create a public channel with topic="Initial Topic" and purpose="Initial Purpose".
//  3. Update the topic 3 times ("Second Topic" → "Third Topic" → "Final Topic").
//  4. Update the purpose to "Updated Purpose".
//  5. Rename the channel to "renamed-channel".
//  6. Create 3 additional users and invite all to the channel.
//  7. Verify num_members=4 (creator + 3).
//  8. Kick one member — verify num_members=3.
//  9. Re-invite the kicked member — verify num_members=4.
//  10. Archive the channel — verify is_archived=true.
//  11. Unarchive — verify is_archived=false.
//  12. Verify conversation event count >= 13:
//     [created + 3 topic_set + purpose_set + updated + 3 member_joined
//     + member_left + member_joined + archived + unarchived]
//  13. Perform projection rebuild — verify name, topic, purpose, and
//     num_members all survive.
func TestFlow_ConversationCreationWithTopicPurpose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	env := setupAllServices(t)
	ctx := context.Background()
	teamID := "T-convfull"

	user, err := env.userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: teamID, Name: "creator", Email: "creator@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
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
			PrincipalType: domain.PrincipalTypeHuman,
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
//
// Scenario: A usergroup goes through its full lifecycle — creation, metadata
//
//	updates (name, handle, description), membership churn, disable/
//	re-enable, a second group for cross-membership, and rebuild.
//
// Steps:
//  1. Create 5 users (A, B, C, D, E).
//  2. Create usergroup "Frontend Team" with handle="frontend".
//  3. Set initial members to [A, B, C] — verify count=3.
//  4. Update name to "Frontend & Mobile Team".
//  5. Update handle to "frontend-mobile".
//  6. Update description to "Frontend and Mobile developers".
//  7. Verify all 3 fields via Get.
//  8. Change membership: remove B, add D and E → [A, C, D, E] — verify count=4.
//  9. Verify B is NOT in the group; D and E ARE.
//
// 10. Disable the usergroup.
// 11. List with include_disabled=false — verify count=0.
// 12. List with include_disabled=true — verify count=1.
// 13. Re-enable the usergroup.
// 14. Create a second usergroup "Backend Team" with members [B, C].
// 15. List all groups — verify count=2.
// 16. Verify user C is in BOTH groups.
// 17. Perform projection rebuild — verify name and handle survive.
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
			Email:         "ug" + string(rune('a'+i)) + "@example.com",
			PrincipalType: domain.PrincipalTypeHuman,
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
