package eventsourcing_test

import (
	"context"
	"testing"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

func TestAPIKey_CreateAccountKeyAndValidateAcrossAllowedWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	account, err := accountRepo.Create(ctx, domain.CreateAccountParams{
		Email:         "owner@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	userA, err := userSvc.Create(ctx, domain.CreateUserParams{
		AccountID:     account.ID,
		WorkspaceID:   "T001",
		Name:          "owner-a",
		Email:         account.Email,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	_, err = userSvc.Create(ctx, domain.CreateUserParams{
		AccountID:     account.ID,
		WorkspaceID:   "T002",
		Name:          "owner-b",
		Email:         account.Email,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	})
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}

	createCtx := ctxutil.WithUser(context.Background(), userA.ID, userA.WorkspaceID)
	createCtx = ctxutil.WithIdentity(createCtx, account.ID)
	createCtx = ctxutil.WithPrincipal(createCtx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	key, rawKey, err := apiKeySvc.Create(createCtx, domain.CreateAPIKeyParams{
		Name:         "account-key",
		Scope:        domain.APIKeyScopeAccount,
		AccountID:    account.ID,
		WorkspaceIDs: []string{"T002"},
		Permissions:  []string{domain.PermissionMessagesRead},
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if key.Scope != domain.APIKeyScopeAccount {
		t.Fatalf("scope = %q, want account", key.Scope)
	}
	if len(key.WorkspaceIDs) != 1 || key.WorkspaceIDs[0] != "T002" {
		t.Fatalf("workspace_ids = %#v", key.WorkspaceIDs)
	}

	validateCtx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T002")
	validation, err := apiKeySvc.ValidateAPIKey(validateCtx, rawKey)
	if err != nil {
		t.Fatalf("validate api key: %v", err)
	}
	if validation.UserID != "" || validation.WorkspaceID != "T002" || validation.AccountID != account.ID {
		t.Fatalf("unexpected validation = %+v", validation)
	}
	if validation.WorkspaceMembershipID == "" {
		t.Fatalf("expected workspace_membership_id in validation, got %+v", validation)
	}
	if validation.AccountType != domain.AccountTypeAdmin {
		t.Fatalf("account_type = %q, want admin", validation.AccountType)
	}
}

func TestAPIKey_ValidateAccountKeyRejectsAmbiguousWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	account, err := accountRepo.Create(ctx, domain.CreateAccountParams{
		Email:         "ambiguous@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	var actor *domain.User
	for _, workspaceID := range []string{"T001", "T002"} {
		user, err := userSvc.Create(ctx, domain.CreateUserParams{
			AccountID:     account.ID,
			WorkspaceID:   workspaceID,
			Name:          "ambiguous-" + workspaceID,
			Email:         account.Email,
			PrincipalType: domain.PrincipalTypeHuman,
			AccountType:   domain.AccountTypeMember,
		})
		if err != nil {
			t.Fatalf("create user for %s: %v", workspaceID, err)
		}
		if workspaceID == "T001" {
			actor = user
		}
	}

	createCtx := ctxutil.WithUser(context.Background(), actor.ID, "T001")
	createCtx = ctxutil.WithIdentity(createCtx, account.ID)
	createCtx = ctxutil.WithPrincipal(createCtx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	_, rawKey, err := apiKeySvc.Create(createCtx, domain.CreateAPIKeyParams{
		Name:        "ambiguous-key",
		Scope:       domain.APIKeyScopeAccount,
		AccountID:   account.ID,
		Permissions: []string{domain.PermissionMessagesRead},
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if _, err := apiKeySvc.ValidateAPIKey(context.Background(), rawKey); err == nil {
		t.Fatal("expected ambiguous account key validation to fail")
	}
}

func TestAPIKey_CreateWorkspaceSystemKeyAndValidateWithoutUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := setupTestDB(t)
	ctx := context.Background()
	logger := newTestLogger()

	accountRepo := pgRepo.NewAccountRepo(pool)
	userRepo := pgRepo.NewUserRepo(pool)
	apiKeyRepo := pgRepo.NewAPIKeyRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)
	userSvc.SetIdentityRepositories(accountRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, recorder, pool, logger)

	adminAccount, err := accountRepo.Create(ctx, domain.CreateAccountParams{
		Email:         "admin@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	})
	if err != nil {
		t.Fatalf("create admin account: %v", err)
	}
	admin, err := userSvc.Create(ctx, domain.CreateUserParams{
		AccountID:     adminAccount.ID,
		WorkspaceID:   "T001",
		Name:          "admin",
		Email:         adminAccount.Email,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	})
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	createCtx := ctxutil.WithUser(context.Background(), admin.ID, admin.WorkspaceID)
	createCtx = ctxutil.WithIdentity(createCtx, adminAccount.ID)
	createCtx = ctxutil.WithPrincipal(createCtx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	key, rawKey, err := apiKeySvc.Create(createCtx, domain.CreateAPIKeyParams{
		Name:        "system-key",
		Scope:       domain.APIKeyScopeWorkspaceSystem,
		WorkspaceID: "T001",
	})
	if err != nil {
		t.Fatalf("create workspace system key: %v", err)
	}
	if key.Scope != domain.APIKeyScopeWorkspaceSystem {
		t.Fatalf("scope = %q", key.Scope)
	}

	validation, err := apiKeySvc.ValidateAPIKey(context.Background(), rawKey)
	if err != nil {
		t.Fatalf("validate workspace system key: %v", err)
	}
	if validation.WorkspaceID != "T001" || validation.UserID != "" {
		t.Fatalf("unexpected validation = %+v", validation)
	}
}
