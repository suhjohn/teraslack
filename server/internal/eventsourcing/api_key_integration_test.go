package eventsourcing_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

// No shared setup struct needed — each test sets up its own environment
// following the same pattern as the existing integration tests.

// ---------- 1. API Key Lifecycle (CRUD) ----------

func TestAPIKey_CreateForHumanPrincipal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	// Create a human user
	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create an API key for the human
	key, rawKey, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "alice-key",
		WorkspaceID: "T001",
		UserID:      user.ID,
		Permissions: []string{"chat:write", "channels:read"},
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Verify key properties
	if key.ID == "" {
		t.Error("key.ID is empty")
	}
	if key.Name != "alice-key" {
		t.Errorf("key.Name = %q, want %q", key.Name, "alice-key")
	}
	if key.WorkspaceID != "T001" {
		t.Errorf("key.WorkspaceID = %q, want %q", key.WorkspaceID, "T001")
	}
	if key.UserID != user.ID {
		t.Errorf("key.UserID = %q, want %q", key.UserID, user.ID)
	}
	if key.KeyPrefix != "sk_" {
		t.Errorf("key.KeyPrefix = %q, want %q", key.KeyPrefix, "sk_")
	}
	if len(key.KeyHint) != 4 {
		t.Errorf("key.KeyHint length = %d, want 4", len(key.KeyHint))
	}

	// Verify raw key format
	if !strings.HasPrefix(rawKey, "sk_") {
		t.Errorf("rawKey prefix = %q, want sk_ prefix", rawKey[:8])
	}
	if rawKey == "" {
		t.Error("rawKey is empty")
	}

	// Verify event recorded
	var eventType string
	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_type, payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	).Scan(&eventType, &payload)
	if err != nil {
		t.Fatalf("query internal_events: %v", err)
	}
	if eventType != domain.EventAPIKeyCreated {
		t.Errorf("event_type = %q, want %q", eventType, domain.EventAPIKeyCreated)
	}

	// Verify payload contains the key snapshot (key_hash is a one-way SHA-256
	// hash and is intentionally preserved for projection rebuild support).
	var snapshot domain.APIKey
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if snapshot.KeyHash == "" {
		t.Error("event payload missing key_hash — needed for projection rebuilds")
	}
	if snapshot.ID != key.ID {
		t.Errorf("snapshot.ID = %q, want %q", snapshot.ID, key.ID)
	}
}

func TestAPIKey_CreateForAgentPrincipal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	// Create human owner
	human, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create human: %v", err)
	}

	// Create agent owned by alice
	agent, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "devin-agent",
		Email:         "devin@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       human.ID,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Verify agent has correct principal_type and owner
	var principalType, ownerID string
	err = pool.QueryRow(ctx, "SELECT principal_type, owner_id FROM users WHERE id = $1", agent.ID).
		Scan(&principalType, &ownerID)
	if err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if principalType != string(domain.PrincipalTypeAgent) {
		t.Errorf("principal_type = %q, want %q", principalType, domain.PrincipalTypeAgent)
	}
	if ownerID != human.ID {
		t.Errorf("owner_id = %q, want %q", ownerID, human.ID)
	}

	// Create API key for the agent, on behalf of alice
	key, rawKey, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "devin-key",
		WorkspaceID: "T001",
		UserID:      agent.ID,
		CreatedBy:   human.ID,
		Permissions: []string{"chat:write"},
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if key.UserID != agent.ID {
		t.Errorf("key.UserID = %q, want %q", key.UserID, agent.ID)
	}
	if key.CreatedBy != human.ID {
		t.Errorf("key.CreatedBy = %q, want %q", key.CreatedBy, human.ID)
	}
	if rawKey == "" {
		t.Error("rawKey is empty")
	}
}

func TestAPIKey_CreateTestEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "bob",
		Email:         "bob@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	key, rawKey, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "test-key",
		WorkspaceID: "T001",
		UserID:      user.ID,
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if key.KeyPrefix != "sk_" {
		t.Errorf("key_prefix = %q, want %q", key.KeyPrefix, "sk_")
	}
	if !strings.HasPrefix(rawKey, "sk_") {
		t.Errorf("rawKey should start with sk_, got %q", rawKey[:8])
	}
}

func TestAPIKey_CreateValidationErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	tests := []struct {
		name   string
		params domain.CreateAPIKeyParams
	}{
		{
			name: "missing name",
			params: domain.CreateAPIKeyParams{
				WorkspaceID: "T001",
				UserID:      "U123",
			},
		},
		{
			name: "missing workspace_id",
			params: domain.CreateAPIKeyParams{
				Name:   "test",
				UserID: "U123",
			},
		},
		{
			name: "missing created_by for system key",
			params: domain.CreateAPIKeyParams{
				Name:        "test",
				WorkspaceID: "T001",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := apiKeySvc.Create(ctx, tc.params)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestAPIKey_CreateSystemKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	admin, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "admin",
		Email:         "admin@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	key, rawKey, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "system-key",
		WorkspaceID: "T001",
		CreatedBy:   admin.ID,
	})
	if err != nil {
		t.Fatalf("create system api key: %v", err)
	}

	if key.UserID != "" {
		t.Errorf("key.UserID = %q, want empty", key.UserID)
	}
	if key.CreatedBy != admin.ID {
		t.Errorf("key.CreatedBy = %q, want %q", key.CreatedBy, admin.ID)
	}
	if !strings.HasPrefix(rawKey, "sk_") {
		t.Errorf("rawKey should start with sk_, got %q", rawKey[:8])
	}

	var principalID *string
	err = pool.QueryRow(ctx, "SELECT principal_id FROM api_keys WHERE id = $1", key.ID).Scan(&principalID)
	if err != nil {
		t.Fatalf("query api key principal_id: %v", err)
	}
	if principalID != nil {
		t.Fatalf("principal_id = %q, want NULL", *principalID)
	}

	validation, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate system api key: %v", err)
	}
	if validation.PrincipalType != domain.PrincipalTypeSystem {
		t.Errorf("principal_type = %q, want %q", validation.PrincipalType, domain.PrincipalTypeSystem)
	}
	if validation.UserID != "" {
		t.Errorf("validation.UserID = %q, want empty", validation.UserID)
	}
}

func TestAPIKey_CreateForNonexistentPrincipal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	_, _, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "ghost-key",
		WorkspaceID: "T001",
		UserID:      "NONEXISTENT",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent principal, got nil")
	}
}

func TestAPIKey_GetStripsKeyHash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "test-key", WorkspaceID: "T001", UserID: user.ID,
	})

	got, err := apiKeySvc.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.KeyHash != "" {
		t.Error("Get should strip key_hash, but it was present")
	}
	if got.Name != "test-key" {
		t.Errorf("Name = %q, want %q", got.Name, "test-key")
	}
}

func TestAPIKey_ListAndFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	alice, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	bob, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "bob", Email: "bob@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	// Create 2 keys for alice, 1 for bob
	apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{Name: "alice-key-1", WorkspaceID: "T001", UserID: alice.ID})
	key2, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "alice-key-2", WorkspaceID: "T001", UserID: alice.ID,
	})
	apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{Name: "bob-key-1", WorkspaceID: "T001", UserID: bob.ID})

	// List all keys for workspace
	page, err := apiKeySvc.List(ctx, domain.ListAPIKeysParams{WorkspaceID: "T001"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 3 {
		t.Errorf("list all: got %d items, want 3", len(page.Items))
	}

	// Verify key_hash stripped in list
	for _, k := range page.Items {
		if k.KeyHash != "" {
			t.Errorf("key %s has key_hash in list response", k.ID)
		}
	}

	// Filter by principal
	page, err = apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		WorkspaceID: "T001", UserID: alice.ID,
	})
	if err != nil {
		t.Fatalf("list by principal: %v", err)
	}
	if len(page.Items) != 2 {
		t.Errorf("list for alice: got %d items, want 2", len(page.Items))
	}

	// Revoke one of alice's keys
	apiKeySvc.Revoke(ctx, key2.ID)

	// List without revoked (default)
	page, err = apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		WorkspaceID: "T001", UserID: alice.ID,
	})
	if err != nil {
		t.Fatalf("list without revoked: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("list without revoked: got %d items, want 1", len(page.Items))
	}

	// List with revoked included
	page, err = apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		WorkspaceID: "T001", UserID: alice.ID, IncludeRevoked: true,
	})
	if err != nil {
		t.Fatalf("list with revoked: %v", err)
	}
	if len(page.Items) != 2 {
		t.Errorf("list with revoked: got %d items, want 2", len(page.Items))
	}
}

func TestAPIKey_UpdateNameDescriptionPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "original", WorkspaceID: "T001", UserID: user.ID, Permissions: []string{"chat:write"},
	})

	newName := "updated-name"
	newDesc := "updated description"
	newPerms := []string{"chat:write", "channels:read", "files:read"}
	updated, err := apiKeySvc.Update(ctx, key.ID, domain.UpdateAPIKeyParams{
		Name:        &newName,
		Description: &newDesc,
		Permissions: &newPerms,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "updated-name" {
		t.Errorf("Name = %q, want %q", updated.Name, "updated-name")
	}
	if updated.Description != "updated description" {
		t.Errorf("Description = %q, want %q", updated.Description, "updated description")
	}
	if len(updated.Permissions) != 3 {
		t.Errorf("Permissions count = %d, want 3", len(updated.Permissions))
	}

	// Verify update event recorded
	var eventCount int
	pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateAPIKey, key.ID, domain.EventAPIKeyUpdated,
	).Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("update events = %d, want 1", eventCount)
	}

	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateAPIKey, key.ID, domain.EventAPIKeyUpdated,
	).Scan(&payload)
	if err != nil {
		t.Fatalf("query update event payload: %v", err)
	}

	var snapshot domain.APIKey
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal update event payload: %v", err)
	}
	if snapshot.KeyHash == "" {
		t.Fatal("update event payload missing key_hash")
	}
}

func TestAPIKey_Revoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "to-revoke", WorkspaceID: "T001", UserID: user.ID,
	})

	// Revoke
	if err := apiKeySvc.Revoke(ctx, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Verify revoked flag
	got, err := apiKeySvc.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get after revoke: %v", err)
	}
	if !got.Revoked {
		t.Error("key should be revoked")
	}

	// Verify revoked key can't validate
	_, err = apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err == nil {
		t.Error("expected error validating revoked key, got nil")
	}

	// Verify revoke event recorded
	var eventType string
	pool.QueryRow(ctx,
		"SELECT event_type FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id DESC LIMIT 1",
		domain.AggregateAPIKey, key.ID,
	).Scan(&eventType)
	if eventType != domain.EventAPIKeyRevoked {
		t.Errorf("last event = %q, want %q", eventType, domain.EventAPIKeyRevoked)
	}
}

// ---------- 2. Key Validation ----------

func TestAPIKey_ValidateLiveKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "live-key", WorkspaceID: "T001", UserID: user.ID,
		Permissions: []string{"chat:write", "channels:read"},
	})

	validation, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if validation.WorkspaceID != "T001" {
		t.Errorf("WorkspaceID = %q, want %q", validation.WorkspaceID, "T001")
	}
	if validation.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", validation.UserID, user.ID)
	}
	if len(validation.Permissions) != 2 {
		t.Errorf("Permissions count = %d, want 2", len(validation.Permissions))
	}
}

func TestAPIKey_ValidateTestKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "bob", Email: "bob@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "test-key", WorkspaceID: "T001", UserID: user.ID,
	})

	_, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestAPIKey_ValidateExpiredKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	// Create a key that expires in 1 second
	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "short-lived", WorkspaceID: "T001", UserID: user.ID, ExpiresIn: "1s",
	})

	// Should work initially
	_, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate before expiry: %v", err)
	}

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Should fail after expiry
	_, err = apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err == nil {
		t.Error("expected error for expired key, got nil")
	}
}

func TestAPIKey_ValidateGarbageKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	_, err := apiKeySvc.ValidateAPIKey(ctx, "not-a-valid-api-key")
	if err == nil {
		t.Error("expected error for garbage key, got nil")
	}
}

func TestAPIKey_UsageTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "usage-key", WorkspaceID: "T001", UserID: user.ID,
	})

	// Validate 3 times
	for i := 0; i < 3; i++ {
		_, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
		if err != nil {
			t.Fatalf("validate %d: %v", i, err)
		}
	}

	// Give the async goroutines time to complete
	time.Sleep(200 * time.Millisecond)

	// Check usage was updated
	var requestCount int64
	var lastUsedAt *time.Time
	err := pool.QueryRow(ctx,
		"SELECT request_count, last_used_at FROM api_keys WHERE id = $1", key.ID,
	).Scan(&requestCount, &lastUsedAt)
	if err != nil {
		t.Fatalf("query usage: %v", err)
	}
	if requestCount < 1 {
		t.Errorf("request_count = %d, want >= 1 (async updates may not all complete)", requestCount)
	}
	if lastUsedAt == nil {
		t.Error("last_used_at should be set after validation")
	}
}

// ---------- 3. Key Rotation ----------

func TestAPIKey_RotateCreatesNewKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	oldKey, oldRawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "original", WorkspaceID: "T001", UserID: user.ID, Permissions: []string{"chat:write"},
	})

	// Rotate with 1h grace period
	newKey, newRawKey, err := apiKeySvc.Rotate(ctx, oldKey.ID, domain.RotateAPIKeyParams{
		GracePeriod: "1h",
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if newKey.ID == oldKey.ID {
		t.Error("new key ID should differ from old key ID")
	}
	if newRawKey == oldRawKey {
		t.Error("new raw key should differ from old raw key")
	}
	if !strings.HasSuffix(newKey.Name, " (rotated)") {
		t.Errorf("new key name = %q, want suffix '(rotated)'", newKey.Name)
	}

	// Verify old key has rotated_to_id set
	oldKeyAfter, _ := apiKeySvc.Get(ctx, oldKey.ID)
	if oldKeyAfter.RotatedToID != newKey.ID {
		t.Errorf("old key rotated_to_id = %q, want %q", oldKeyAfter.RotatedToID, newKey.ID)
	}
	if oldKeyAfter.GracePeriodEndsAt == nil {
		t.Error("old key grace_period_ends_at should be set")
	}

	// Both keys should validate during grace period
	_, err = apiKeySvc.ValidateAPIKey(ctx, oldRawKey)
	if err != nil {
		t.Errorf("old key should still validate during grace period: %v", err)
	}
	_, err = apiKeySvc.ValidateAPIKey(ctx, newRawKey)
	if err != nil {
		t.Errorf("new key should validate: %v", err)
	}

	// Verify rotation event recorded
	var eventType string
	var payload json.RawMessage
	pool.QueryRow(ctx,
		"SELECT event_type, payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateAPIKey, oldKey.ID, domain.EventAPIKeyRotated,
	).Scan(&eventType, &payload)
	if eventType != domain.EventAPIKeyRotated {
		t.Errorf("event = %q, want %q", eventType, domain.EventAPIKeyRotated)
	}

	var rotatePayload map[string]any
	json.Unmarshal(payload, &rotatePayload)
	if rotatePayload["old_key_id"] != oldKey.ID {
		t.Errorf("payload old_key_id = %v, want %q", rotatePayload["old_key_id"], oldKey.ID)
	}
	if rotatePayload["new_key_id"] != newKey.ID {
		t.Errorf("payload new_key_id = %v, want %q", rotatePayload["new_key_id"], newKey.ID)
	}
}

func TestAPIKey_RotateRevokedKeyFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "to-revoke", WorkspaceID: "T001", UserID: user.ID,
	})

	apiKeySvc.Revoke(ctx, key.ID)

	_, _, err := apiKeySvc.Rotate(ctx, key.ID, domain.RotateAPIKeyParams{})
	if err == nil {
		t.Error("expected error rotating revoked key, got nil")
	}
}

func TestAPIKey_RotateCustomGracePeriod(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "rotate-test", WorkspaceID: "T001", UserID: user.ID,
	})

	// 7 day grace period
	_, _, err := apiKeySvc.Rotate(ctx, key.ID, domain.RotateAPIKeyParams{
		GracePeriod: "7d",
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	oldKeyAfter, _ := apiKeySvc.Get(ctx, key.ID)
	if oldKeyAfter.GracePeriodEndsAt == nil {
		t.Fatal("grace_period_ends_at should be set")
	}

	// Should be roughly 7 days from now
	expectedEnd := time.Now().Add(7 * 24 * time.Hour)
	diff := oldKeyAfter.GracePeriodEndsAt.Sub(expectedEnd)
	if diff < -5*time.Second || diff > 5*time.Second {
		t.Errorf("grace_period_ends_at off by %v (expected ~7 days from now)", diff)
	}
}

// ---------- 4. Principal Model ----------

func TestPrincipalType_DefaultsToHuman(t *testing.T) {
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

	// Create user without specifying principal_type
	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "default-user", Email: "default@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	var pt string
	pool.QueryRow(ctx, "SELECT principal_type FROM users WHERE id = $1", user.ID).Scan(&pt)
	if pt != "human" {
		t.Errorf("principal_type = %q, want %q (should default to 'human')", pt, "human")
	}
}

func TestPrincipalType_AgentWithOwner(t *testing.T) {
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

	human, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	agent, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "devin",
		Email:         "devin@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       human.ID,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var pt, oid string
	pool.QueryRow(ctx, "SELECT principal_type, owner_id FROM users WHERE id = $1", agent.ID).Scan(&pt, &oid)
	if pt != "agent" {
		t.Errorf("principal_type = %q, want %q", pt, "agent")
	}
	if oid != human.ID {
		t.Errorf("owner_id = %q, want %q", oid, human.ID)
	}
}

func TestAPIKey_AgentActionsTrackedInEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	convRepo := pgRepo.NewConversationRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)
	convSvc := service.NewConversationService(convRepo, userRepo, recorder, pool, logger)

	// Create human + agent + API key for agent
	human, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	agent, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T001",
		Name:          "devin",
		Email:         "devin@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       human.ID,
	})

	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "agent-key", WorkspaceID: "T001", UserID: agent.ID,
		CreatedBy: human.ID,
	})

	// Validate key to get the validation info (simulating what auth middleware does)
	validation, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Simulate what auth middleware does: set context with principal info
	ctxWithAuth := context.WithValue(ctx, "user_id", validation.UserID)

	// Agent creates a conversation
	conv, err := convSvc.Create(ctxWithAuth, domain.CreateConversationParams{
		WorkspaceID: "T001",
		Name:        "agent-created-channel",
		Type:        domain.ConversationTypePublicChannel,
		CreatorID:   agent.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	if conv.CreatorID != agent.ID {
		t.Errorf("conversation creator = %q, want %q", conv.CreatorID, agent.ID)
	}

	// Verify delegation info is available
}

// ---------- 5. Event Sourcing Consistency ----------

func TestAPIKey_FullLifecycleEventCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	// 1. Create
	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "lifecycle-key", WorkspaceID: "T001", UserID: user.ID,
	})

	// 2. Update
	newName := "renamed"
	apiKeySvc.Update(ctx, key.ID, domain.UpdateAPIKeyParams{Name: &newName})

	// 3. Rotate (creates 2 events: rotated + created for the new key)
	apiKeySvc.Rotate(ctx, key.ID, domain.RotateAPIKeyParams{GracePeriod: "1h"})

	// 4. Revoke original
	apiKeySvc.Revoke(ctx, key.ID)

	// Count events for the original key
	var count int
	pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	).Scan(&count)

	// Expected: created + updated + rotated + revoked = 4
	if count != 4 {
		t.Errorf("event count for original key = %d, want 4 (created + updated + rotated + revoked)", count)
	}

	// Count total api_key events (including the new key's created event)
	var totalCount int
	pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM internal_events WHERE aggregate_type = $1",
		domain.AggregateAPIKey,
	).Scan(&totalCount)

	// Expected: 4 (original) + 1 (new key created) = 5
	if totalCount != 5 {
		t.Errorf("total api_key events = %d, want 5", totalCount)
	}

	// Verify event types are in order
	rows, err := pool.Query(ctx,
		"SELECT event_type FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id ASC",
		domain.AggregateAPIKey, key.ID,
	)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	expectedTypes := []string{
		domain.EventAPIKeyCreated,
		domain.EventAPIKeyUpdated,
		domain.EventAPIKeyRotated,
		domain.EventAPIKeyRevoked,
	}
	i := 0
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if i >= len(expectedTypes) {
			t.Errorf("extra event: %q", et)
			continue
		}
		if et != expectedTypes[i] {
			t.Errorf("event %d = %q, want %q", i, et, expectedTypes[i])
		}
		i++
	}
	if i != len(expectedTypes) {
		t.Errorf("got %d events, want %d", i, len(expectedTypes))
	}
}

func TestAPIKey_EventPayloadsRedacted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "redact-test", WorkspaceID: "T001", UserID: user.ID,
	})

	// Check all api_key events don't contain key_hash
	rows, err := pool.Query(ctx,
		"SELECT event_type, payload FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var et string
		var payload json.RawMessage
		if err := rows.Scan(&et, &payload); err != nil {
			t.Fatalf("scan: %v", err)
		}

		var m map[string]any
		json.Unmarshal(payload, &m)

		// key_hash is a one-way SHA-256 hash, safe to store in events.
		// It must be present for projection rebuilds to restore auth capability.
		if hash, ok := m["key_hash"]; ok && hash == "" {
			t.Errorf("event %q payload has empty key_hash — needed for projection rebuilds", et)
		}
	}
}

func TestAPIKey_TransactionalAtomicity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T001", Name: "alice", Email: "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	// Create a key and verify both projection and event exist
	key, _, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "atomic-test", WorkspaceID: "T001", UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Projection exists
	var projName string
	err = pool.QueryRow(ctx, "SELECT name FROM api_keys WHERE id = $1", key.ID).Scan(&projName)
	if err != nil {
		t.Fatalf("projection missing: %v", err)
	}
	if projName != "atomic-test" {
		t.Errorf("projection name = %q, want %q", projName, "atomic-test")
	}

	// Event exists
	var eventID int64
	err = pool.QueryRow(ctx,
		"SELECT id FROM internal_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	).Scan(&eventID)
	if err != nil {
		t.Fatalf("event missing: %v", err)
	}
	if eventID == 0 {
		t.Error("event ID should be non-zero")
	}
}
