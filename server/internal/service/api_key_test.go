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
		ID:          "AK123",
		Name:        params.Name,
		Description: params.Description,
		WorkspaceID:      params.WorkspaceID,
		UserID:      params.UserID,
		CreatedBy:   params.CreatedBy,
		Permissions: params.Permissions,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
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
	return nil, domain.ErrNotFound
}

func (m *mockAPIKeyRepo) List(_ context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error) {
	items := make([]domain.APIKey, 0, len(m.keys))
	for _, key := range m.keys {
		if key.WorkspaceID != params.WorkspaceID {
			continue
		}
		if params.UserID != "" && key.UserID != params.UserID {
			continue
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
	if params.Permissions != nil {
		key.Permissions = *params.Permissions
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

func TestAPIKeyService_CreateRestrictsMemberToSelfAndMemberPermissions(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	userRepo.users["U_OTHER"] = &domain.User{ID: "U_OTHER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "self",
		WorkspaceID:      "T123",
		UserID:      "U_MEMBER",
		Permissions: []string{domain.PermissionMessagesRead},
	}); err != nil {
		t.Fatalf("member self key should succeed: %v", err)
	}

	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "other",
		WorkspaceID:      "T123",
		UserID:      "U_OTHER",
		Permissions: []string{domain.PermissionMessagesRead},
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member creating key for another user, got %v", err)
	}

	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "elevated",
		WorkspaceID:      "T123",
		UserID:      "U_MEMBER",
		Permissions: []string{domain.PermissionUsersCreate},
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member elevated permission, got %v", err)
	}
}

func TestAPIKeyService_CreateAllowsMemberOwnedAgentKeys(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	userRepo.users["U_AGENT"] = &domain.User{ID: "U_AGENT", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeAgent, OwnerID: "U_MEMBER", IsBot: true}
	userRepo.users["U_OTHER_AGENT"] = &domain.User{ID: "U_OTHER_AGENT", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeAgent, OwnerID: "U_OTHER", IsBot: true}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	key, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "owned-agent",
		WorkspaceID: "T123",
		UserID:      "U_AGENT",
		Permissions: []string{"*"},
	})
	if err != nil {
		t.Fatalf("member owned agent key should succeed: %v", err)
	}
	if key.CreatedBy != "U_MEMBER" {
		t.Fatalf("created_by = %q, want U_MEMBER", key.CreatedBy)
	}

	if _, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:        "foreign-agent",
		WorkspaceID: "T123",
		UserID:      "U_OTHER_AGENT",
		Permissions: []string{"*"},
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for foreign agent key create, got %v", err)
	}
}

func TestAPIKeyService_AdminCanManageMemberKeys(t *testing.T) {
	repo := newMockAPIKeyRepo()
	repo.keys["AK1"] = &domain.APIKey{ID: "AK1", WorkspaceID: "T123", UserID: "U_MEMBER", Permissions: []string{domain.PermissionMessagesRead}}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	userRepo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	if err := svc.Revoke(ctx, "AK1"); err != nil {
		t.Fatalf("admin revoke should succeed: %v", err)
	}
}

func TestAPIKeyService_AdminCanCreateSystemKey(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	key, _, err := svc.Create(ctx, domain.CreateAPIKeyParams{
		Name:   "system",
		WorkspaceID: "T123",
	})
	if err != nil {
		t.Fatalf("admin system key should succeed: %v", err)
	}
	if key.UserID != "" {
		t.Fatalf("expected empty user id for system key, got %q", key.UserID)
	}
	if key.CreatedBy != "U_ADMIN" {
		t.Fatalf("expected created_by to default to admin actor, got %q", key.CreatedBy)
	}
}

func TestAPIKeyService_SystemKeyRequiresCreatorWithoutAuth(t *testing.T) {
	repo := newMockAPIKeyRepo()
	userRepo := newMockUserRepoTenant()
	svc := NewAPIKeyService(repo, userRepo, nil, mockTxBeginner{}, nil)

	if _, _, err := svc.Create(context.Background(), domain.CreateAPIKeyParams{
		Name:   "system",
		WorkspaceID: "T123",
	}); err == nil || !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for missing created_by, got %v", err)
	}
}
