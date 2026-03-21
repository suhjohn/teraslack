package eventsourcing_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/suhjohn/workspace/internal/domain"
	pgRepo "github.com/suhjohn/workspace/internal/repository/postgres"
	"github.com/suhjohn/workspace/internal/service"
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

	// Run migration 000007
	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	// Create a human user
	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T001",
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
		TeamID:      "T001",
		PrincipalID: user.ID,
		Type:        domain.APIKeyTypePersistent,
		Environment: domain.APIKeyEnvLive,
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
	if key.TeamID != "T001" {
		t.Errorf("key.TeamID = %q, want %q", key.TeamID, "T001")
	}
	if key.PrincipalID != user.ID {
		t.Errorf("key.PrincipalID = %q, want %q", key.PrincipalID, user.ID)
	}
	if key.Type != domain.APIKeyTypePersistent {
		t.Errorf("key.Type = %q, want %q", key.Type, domain.APIKeyTypePersistent)
	}
	if key.Environment != domain.APIKeyEnvLive {
		t.Errorf("key.Environment = %q, want %q", key.Environment, domain.APIKeyEnvLive)
	}
	if key.KeyPrefix != "sk_live_" {
		t.Errorf("key.KeyPrefix = %q, want %q", key.KeyPrefix, "sk_live_")
	}
	if len(key.KeyHint) != 4 {
		t.Errorf("key.KeyHint length = %d, want 4", len(key.KeyHint))
	}

	// Verify raw key format
	if !strings.HasPrefix(rawKey, "sk_live_") {
		t.Errorf("rawKey prefix = %q, want sk_live_ prefix", rawKey[:8])
	}
	if rawKey == "" {
		t.Error("rawKey is empty")
	}

	// Verify event recorded
	var eventType string
	var payload json.RawMessage
	err = pool.QueryRow(ctx,
		"SELECT event_type, payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	).Scan(&eventType, &payload)
	if err != nil {
		t.Fatalf("query service_events: %v", err)
	}
	if eventType != domain.EventAPIKeyCreated {
		t.Errorf("event_type = %q, want %q", eventType, domain.EventAPIKeyCreated)
	}

	// Verify payload is redacted (no key_hash)
	var snapshot domain.APIKey
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if snapshot.KeyHash != "" {
		t.Error("event payload contains key_hash — should be redacted")
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	// Create human owner
	human, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create human: %v", err)
	}

	// Create agent owned by alice
	agent, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T001",
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
		TeamID:      "T001",
		PrincipalID: agent.ID,
		CreatedBy:   human.ID,
		OnBehalfOf:  human.ID,
		Type:        domain.APIKeyTypePersistent,
		Environment: domain.APIKeyEnvLive,
		Permissions: []string{"chat:write"},
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if key.PrincipalID != agent.ID {
		t.Errorf("key.PrincipalID = %q, want %q", key.PrincipalID, agent.ID)
	}
	if key.CreatedBy != human.ID {
		t.Errorf("key.CreatedBy = %q, want %q", key.CreatedBy, human.ID)
	}
	if key.OnBehalfOf != human.ID {
		t.Errorf("key.OnBehalfOf = %q, want %q", key.OnBehalfOf, human.ID)
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001",
		Name:   "bob",
		Email:  "bob@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	key, rawKey, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "test-key",
		TeamID:      "T001",
		PrincipalID: user.ID,
		Type:        domain.APIKeyTypePersistent,
		Environment: domain.APIKeyEnvTest,
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if key.Environment != domain.APIKeyEnvTest {
		t.Errorf("environment = %q, want %q", key.Environment, domain.APIKeyEnvTest)
	}
	if key.KeyPrefix != "sk_test_" {
		t.Errorf("key_prefix = %q, want %q", key.KeyPrefix, "sk_test_")
	}
	if !strings.HasPrefix(rawKey, "sk_test_") {
		t.Errorf("rawKey should start with sk_test_, got %q", rawKey[:8])
	}
}

func TestAPIKey_CreateValidationErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

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
				TeamID:      "T001",
				PrincipalID: "U123",
			},
		},
		{
			name: "missing team_id",
			params: domain.CreateAPIKeyParams{
				Name:        "test",
				PrincipalID: "U123",
			},
		},
		{
			name: "missing principal_id",
			params: domain.CreateAPIKeyParams{
				Name:   "test",
				TeamID: "T001",
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

func TestAPIKey_CreateForNonexistentPrincipal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	_, _, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "ghost-key",
		TeamID:      "T001",
		PrincipalID: "NONEXISTENT",
		Environment: domain.APIKeyEnvLive,
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "test-key", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	alice, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})
	bob, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "bob", Email: "bob@example.com",
	})

	// Create 2 keys for alice, 1 for bob
	apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "alice-key-1", TeamID: "T001", PrincipalID: alice.ID, Environment: domain.APIKeyEnvLive,
	})
	key2, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "alice-key-2", TeamID: "T001", PrincipalID: alice.ID, Environment: domain.APIKeyEnvLive,
	})
	apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "bob-key-1", TeamID: "T001", PrincipalID: bob.ID, Environment: domain.APIKeyEnvLive,
	})

	// List all keys for team
	page, err := apiKeySvc.List(ctx, domain.ListAPIKeysParams{TeamID: "T001"})
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
		TeamID: "T001", PrincipalID: alice.ID,
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
		TeamID: "T001", PrincipalID: alice.ID,
	})
	if err != nil {
		t.Fatalf("list without revoked: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("list without revoked: got %d items, want 1", len(page.Items))
	}

	// List with revoked included
	page, err = apiKeySvc.List(ctx, domain.ListAPIKeysParams{
		TeamID: "T001", PrincipalID: alice.ID, IncludeRevoked: true,
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "original", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive, Permissions: []string{"chat:write"},
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
		"SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
		domain.AggregateAPIKey, key.ID, domain.EventAPIKeyUpdated,
	).Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("update events = %d, want 1", eventCount)
	}
}

func TestAPIKey_Revoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "to-revoke", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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
		"SELECT event_type FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id DESC LIMIT 1",
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "live-key", TeamID: "T001", PrincipalID: user.ID,
		Type: domain.APIKeyTypePersistent, Environment: domain.APIKeyEnvLive,
		Permissions: []string{"chat:write", "channels:read"},
	})

	validation, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if validation.TeamID != "T001" {
		t.Errorf("TeamID = %q, want %q", validation.TeamID, "T001")
	}
	if validation.PrincipalID != user.ID {
		t.Errorf("PrincipalID = %q, want %q", validation.PrincipalID, user.ID)
	}
	if validation.Environment != domain.APIKeyEnvLive {
		t.Errorf("Environment = %q, want %q", validation.Environment, domain.APIKeyEnvLive)
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "bob", Email: "bob@example.com",
	})

	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "test-key", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvTest,
	})

	validation, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validation.Environment != domain.APIKeyEnvTest {
		t.Errorf("Environment = %q, want %q", validation.Environment, domain.APIKeyEnvTest)
	}
}

func TestAPIKey_ValidateExpiredKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	// Create a key that expires in 1 second
	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "short-lived", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive, ExpiresIn: "1s",
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	_, err := apiKeySvc.ValidateAPIKey(ctx, "sk_live_totallygarbage1234567890")
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "usage-key", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	oldKey, oldRawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "original", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive, Permissions: []string{"chat:write"},
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
		"SELECT event_type, payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 AND event_type = $3",
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "to-revoke", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "rotate-test", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	// Create user without specifying principal_type
	user, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "default-user", Email: "default@example.com",
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	human, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})

	agent, err := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T001",
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

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
		TeamID:        "T001",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	agent, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T001",
		Name:          "devin",
		Email:         "devin@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       human.ID,
	})

	_, rawKey, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "agent-key", TeamID: "T001", PrincipalID: agent.ID,
		CreatedBy: human.ID, OnBehalfOf: human.ID,
		Environment: domain.APIKeyEnvLive,
	})

	// Validate key to get the validation info (simulating what auth middleware does)
	validation, err := apiKeySvc.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Simulate what auth middleware does: set context with principal info
	ctxWithAuth := context.WithValue(ctx, "user_id", validation.PrincipalID)

	// Agent creates a conversation
	conv, err := convSvc.Create(ctxWithAuth, domain.CreateConversationParams{
		TeamID:    "T001",
		Name:      "agent-created-channel",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agent.ID,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	if conv.CreatorID != agent.ID {
		t.Errorf("conversation creator = %q, want %q", conv.CreatorID, agent.ID)
	}

	// Verify delegation info is available
	if validation.OnBehalfOf != human.ID {
		t.Errorf("validation.OnBehalfOf = %q, want %q", validation.OnBehalfOf, human.ID)
	}
}

// ---------- 5. Event Sourcing Consistency ----------

func TestAPIKey_FullLifecycleEventCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	// 1. Create
	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "lifecycle-key", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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
		"SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	).Scan(&count)

	// Expected: created + updated + rotated + revoked = 4
	if count != 4 {
		t.Errorf("event count for original key = %d, want 4 (created + updated + rotated + revoked)", count)
	}

	// Count total api_key events (including the new key's created event)
	var totalCount int
	pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM service_events WHERE aggregate_type = $1",
		domain.AggregateAPIKey,
	).Scan(&totalCount)

	// Expected: 4 (original) + 1 (new key created) = 5
	if totalCount != 5 {
		t.Errorf("total api_key events = %d, want 5", totalCount)
	}

	// Verify event types are in order
	rows, err := pool.Query(ctx,
		"SELECT event_type FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2 ORDER BY id ASC",
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	key, _, _ := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "redact-test", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
	})

	// Check all api_key events don't contain key_hash
	rows, err := pool.Query(ctx,
		"SELECT event_type, payload FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
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

		if hash, ok := m["key_hash"]; ok && hash != "" {
			t.Errorf("event %q payload contains non-empty key_hash — should be redacted", et)
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

	migrationsDir := getMigrationsDir(t)
	data := readMigration(t, migrationsDir, "000007_api_keys_principal_type.up.sql")
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run migration 000007: %v", err)
	}

	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	user, _ := userSvc.Create(ctx, domain.CreateUserParams{
		TeamID: "T001", Name: "alice", Email: "alice@example.com",
	})

	// Create a key and verify both projection and event exist
	key, _, err := apiKeySvc.Create(ctx, domain.CreateAPIKeyParams{
		Name: "atomic-test", TeamID: "T001", PrincipalID: user.ID,
		Environment: domain.APIKeyEnvLive,
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
		"SELECT id FROM service_events WHERE aggregate_type = $1 AND aggregate_id = $2",
		domain.AggregateAPIKey, key.ID,
	).Scan(&eventID)
	if err != nil {
		t.Fatalf("event missing: %v", err)
	}
	if eventID == 0 {
		t.Error("event ID should be non-zero")
	}
}
