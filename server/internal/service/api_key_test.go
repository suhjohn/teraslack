package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockAPIKeyRepo struct {
	keys map[string]*domain.APIKey
}

func newMockAPIKeyRepo() *mockAPIKeyRepo {
	return &mockAPIKeyRepo{keys: map[string]*domain.APIKey{}}
}

func (m *mockAPIKeyRepo) WithTx(_ pgx.Tx) repository.APIKeyRepository { return m }

func (m *mockAPIKeyRepo) Create(_ context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error) {
	key := &domain.APIKey{
		ID:           "AK123",
		Name:         params.Name,
		Description:  params.Description,
		Scope:        params.Scope,
		WorkspaceID:  params.WorkspaceID,
		AccountID:    params.AccountID,
		WorkspaceIDs: params.WorkspaceIDs,
		CreatedBy:    params.CreatedBy,
		Permissions:  params.Permissions,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	m.keys[key.ID] = key
	return key, "sk_test", nil
}

func (m *mockAPIKeyRepo) Get(_ context.Context, id string) (*domain.APIKey, error) {
	key, ok := m.keys[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return key, nil
}

func (m *mockAPIKeyRepo) GetByHash(_ context.Context, _ string) (*domain.APIKey, error) {
	for _, key := range m.keys {
		return key, nil
	}
	return nil, domain.ErrNotFound
}

func (m *mockAPIKeyRepo) List(_ context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error) {
	items := make([]domain.APIKey, 0, len(m.keys))
	for _, key := range m.keys {
		if params.Scope != "" && key.Scope != params.Scope {
			continue
		}
		if params.AccountID != "" && key.AccountID != params.AccountID {
			continue
		}
		if params.WorkspaceID != "" {
			switch key.Scope {
			case domain.APIKeyScopeWorkspaceSystem:
				if key.WorkspaceID != params.WorkspaceID {
					continue
				}
			case domain.APIKeyScopeAccount:
				if len(key.WorkspaceIDs) > 0 {
					matches := false
					for _, workspaceID := range key.WorkspaceIDs {
						if workspaceID == params.WorkspaceID {
							matches = true
							break
						}
					}
					if !matches {
						continue
					}
				}
			}
		}
		items = append(items, *key)
	}
	return &domain.CursorPage[domain.APIKey]{Items: items}, nil
}

func (m *mockAPIKeyRepo) Update(_ context.Context, id string, params domain.UpdateAPIKeyParams) (*domain.APIKey, error) {
	key, ok := m.keys[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if params.Name != nil {
		key.Name = *params.Name
	}
	if params.Description != nil {
		key.Description = *params.Description
	}
	if params.Permissions != nil {
		key.Permissions = *params.Permissions
	}
	if params.WorkspaceIDs != nil {
		key.WorkspaceIDs = *params.WorkspaceIDs
	}
	return key, nil
}

func (m *mockAPIKeyRepo) Revoke(_ context.Context, id string) error {
	key, ok := m.keys[id]
	if !ok {
		return domain.ErrNotFound
	}
	key.Revoked = true
	now := time.Now().UTC()
	key.RevokedAt = &now
	return nil
}

func (m *mockAPIKeyRepo) SetRotated(_ context.Context, oldKeyID, newKeyID string, gracePeriodEndsAt time.Time) error {
	key, ok := m.keys[oldKeyID]
	if !ok {
		return domain.ErrNotFound
	}
	key.RotatedToID = newKeyID
	key.GracePeriodEndsAt = &gracePeriodEndsAt
	return nil
}

func (m *mockAPIKeyRepo) UpdateUsage(_ context.Context, _ string) error { return nil }

func accountActorContext(userID, accountID, workspaceID string, accountType domain.AccountType) context.Context {
	ctx := ctxutil.WithUser(context.Background(), userID, workspaceID)
	ctx = ctxutil.WithIdentity(ctx, accountID)
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, accountType, false)
	return ctx
}

func TestAPIKeyService_CreateAccountKeyRestrictsToActorAccount(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_MEMBER"] = &domain.User{
		ID:            "U_MEMBER",
		AccountID:     "A_MEMBER",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := accountActorContext("U_MEMBER", "A_MEMBER", "T123", domain.AccountTypeMember)
	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "self",
		Scope:       domain.APIKeyScopeAccount,
		AccountID:   "A_MEMBER",
		Permissions: []string{domain.PermissionMessagesRead},
	}); err != nil {
		t.Fatalf("member account key should succeed: %v", err)
	}

	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "other",
		Scope:       domain.APIKeyScopeAccount,
		AccountID:   "A_OTHER",
		Permissions: []string{domain.PermissionMessagesRead},
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for foreign account key create, got %v", err)
	}
}

func TestAPIKeyService_CreateAccountKeyRestrictsMemberPermissions(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_MEMBER"] = &domain.User{
		ID:            "U_MEMBER",
		AccountID:     "A_MEMBER",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := accountActorContext("U_MEMBER", "A_MEMBER", "T123", domain.AccountTypeMember)
	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "elevated",
		Scope:       domain.APIKeyScopeAccount,
		AccountID:   "A_MEMBER",
		Permissions: []string{domain.PermissionUsersCreate},
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member elevated permission, got %v", err)
	}
}

func TestAPIKeyService_AdminCanCreateWorkspaceSystemKey(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_ADMIN"] = &domain.User{
		ID:            "U_ADMIN",
		AccountID:     "A_ADMIN",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := accountActorContext("U_ADMIN", "A_ADMIN", "T123", domain.AccountTypeAdmin)
	key, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "system",
		Scope:       domain.APIKeyScopeWorkspaceSystem,
		WorkspaceID: "T123",
	})
	if err != nil {
		t.Fatalf("workspace system key should succeed: %v", err)
	}
	if key.Scope != domain.APIKeyScopeWorkspaceSystem {
		t.Fatalf("scope = %q, want workspace_system", key.Scope)
	}
	if key.WorkspaceID != "T123" {
		t.Fatalf("workspace_id = %q, want T123", key.WorkspaceID)
	}
	if key.AccountID != "" {
		t.Fatalf("account_id = %q, want empty", key.AccountID)
	}
}

func TestAPIKeyService_WorkspaceSystemKeyRequiresAdmin(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_MEMBER"] = &domain.User{
		ID:            "U_MEMBER",
		AccountID:     "A_MEMBER",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := accountActorContext("U_MEMBER", "A_MEMBER", "T123", domain.AccountTypeMember)
	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "system",
		Scope:       domain.APIKeyScopeWorkspaceSystem,
		WorkspaceID: "T123",
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member workspace-system key, got %v", err)
	}
}

func TestAPIKeyService_ValidateAccountKeyUsesRequestedWorkspaceMembership(t *testing.T) {
	repo := newMockAPIKeyRepo()
	repo.keys["AK1"] = &domain.APIKey{
		ID:          "AK1",
		Scope:       domain.APIKeyScopeAccount,
		AccountID:   "A123",
		Permissions: []string{domain.PermissionMessagesRead},
	}
	userRepo := &mockUserRepoMap{users: map[string]*domain.User{
		"U1": {
			ID:            "U1",
			AccountID:     "A123",
			WorkspaceID:   "T123",
			PrincipalType: domain.PrincipalTypeHuman,
			AccountType:   domain.AccountTypeMember,
		},
		"U2": {
			ID:            "U2",
			AccountID:     "A123",
			WorkspaceID:   "T456",
			PrincipalType: domain.PrincipalTypeHuman,
			AccountType:   domain.AccountTypeAdmin,
		},
	}}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T456")
	validation, err := svc.ValidateAPIKey(ctx, "sk_test_value")
	if err != nil {
		t.Fatalf("ValidateAPIKey() error = %v", err)
	}
	if validation.WorkspaceID != "T456" || validation.UserID != "" || validation.AccountID != "A123" {
		t.Fatalf("unexpected validation identity: %+v", validation)
	}
	if validation.WorkspaceMembershipID != "WM_U2" {
		t.Fatalf("workspace_membership_id = %q, want %q", validation.WorkspaceMembershipID, "WM_U2")
	}
	if validation.AccountType != domain.AccountTypeAdmin {
		t.Fatalf("account_type = %q, want admin", validation.AccountType)
	}
}

func TestAPIKeyService_ValidateAccountKeyRejectsAmbiguousWorkspace(t *testing.T) {
	repo := newMockAPIKeyRepo()
	repo.keys["AK1"] = &domain.APIKey{
		ID:          "AK1",
		Scope:       domain.APIKeyScopeAccount,
		AccountID:   "A123",
		Permissions: []string{domain.PermissionMessagesRead},
	}
	userRepo := &mockUserRepoMap{users: map[string]*domain.User{
		"U1": {ID: "U1", AccountID: "A123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		"U2": {ID: "U2", AccountID: "A123", WorkspaceID: "T456", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin},
	}}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	if _, err := svc.ValidateAPIKey(context.Background(), "sk_test_value"); err == nil || !errors.Is(err, domain.ErrInvalidAuth) {
		t.Fatalf("expected invalid auth for ambiguous workspace resolution, got %v", err)
	}
}

func TestAPIKeyService_ValidateAccountKeyHonorsWorkspaceAllowlist(t *testing.T) {
	repo := newMockAPIKeyRepo()
	repo.keys["AK1"] = &domain.APIKey{
		ID:           "AK1",
		Scope:        domain.APIKeyScopeAccount,
		AccountID:    "A123",
		WorkspaceIDs: []string{"T123"},
		Permissions:  []string{domain.PermissionMessagesRead},
	}
	userRepo := &mockUserRepoMap{users: map[string]*domain.User{
		"U1": {ID: "U1", AccountID: "A123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		"U2": {ID: "U2", AccountID: "A123", WorkspaceID: "T456", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin},
	}}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T456")
	if _, err := svc.ValidateAPIKey(ctx, "sk_test_value"); err == nil || !errors.Is(err, domain.ErrInvalidAuth) {
		t.Fatalf("expected invalid auth for disallowed workspace, got %v", err)
	}
}

func TestAPIKeyService_ValidateWorkspaceSystemKeyDoesNotRequireUser(t *testing.T) {
	repo := newMockAPIKeyRepo()
	repo.keys["AK_SYSTEM"] = &domain.APIKey{
		ID:          "AK_SYSTEM",
		Scope:       domain.APIKeyScopeWorkspaceSystem,
		WorkspaceID: "T123",
		Permissions: []string{domain.PermissionMessagesRead},
	}
	svc := NewAPIKeyService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	validation, err := svc.ValidateAPIKey(context.Background(), "sk_test_system")
	if err != nil {
		t.Fatalf("ValidateAPIKey() error = %v", err)
	}
	if validation.WorkspaceID != "T123" {
		t.Fatalf("workspace_id = %q, want T123", validation.WorkspaceID)
	}
	if validation.UserID != "" {
		t.Fatalf("user_id = %q, want empty", validation.UserID)
	}
	if validation.PrincipalType != domain.PrincipalTypeSystem {
		t.Fatalf("principal_type = %q, want %q", validation.PrincipalType, domain.PrincipalTypeSystem)
	}
}
