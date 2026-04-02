package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockUserRepoTenant struct {
	users map[string]*domain.User
}

func newMockUserRepoTenant() *mockUserRepoTenant {
	return &mockUserRepoTenant{users: make(map[string]*domain.User)}
}

func (m *mockUserRepoTenant) Create(_ context.Context, params domain.CreateUserParams) (*domain.User, error) {
	u := &domain.User{
		ID:            "U123",
		AccountID:     params.AccountID,
		WorkspaceID:   params.WorkspaceID,
		Name:          params.Name,
		RealName:      params.RealName,
		DisplayName:   params.DisplayName,
		Email:         params.Email,
		PrincipalType: params.PrincipalType,
		OwnerID:       params.OwnerID,
		AccountType:   domain.NormalizeAccountType(params.PrincipalType, params.AccountType),
		IsBot:         params.IsBot,
		Profile:       params.Profile,
	}
	m.users[u.ID] = u
	return u, nil
}

func (m *mockUserRepoTenant) Get(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return u, nil
}

func (m *mockUserRepoTenant) GetByWorkspaceAndAccount(_ context.Context, workspaceID, accountID string) (*domain.User, error) {
	for _, u := range m.users {
		if u.WorkspaceID == workspaceID && u.AccountID == accountID {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoTenant) ListByAccount(_ context.Context, accountID string) ([]domain.User, error) {
	users := make([]domain.User, 0)
	for _, u := range m.users {
		if u.AccountID == accountID {
			users = append(users, *u)
		}
	}
	return users, nil
}

func (m *mockUserRepoTenant) GetByTeamEmail(_ context.Context, workspaceID, email string) (*domain.User, error) {
	for _, u := range m.users {
		if u.WorkspaceID == workspaceID && u.Email == email {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoTenant) Update(_ context.Context, id string, params domain.UpdateUserParams) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if params.RealName != nil {
		u.RealName = *params.RealName
	}
	if params.DisplayName != nil {
		u.DisplayName = *params.DisplayName
	}
	if params.Email != nil {
		u.Email = *params.Email
	}
	if params.AccountType != nil {
		u.AccountType = *params.AccountType
	}
	if params.Deleted != nil {
		u.Deleted = *params.Deleted
	}
	if params.Profile != nil {
		u.Profile = *params.Profile
	}
	return u, nil
}

func (m *mockUserRepoTenant) List(_ context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if params.WorkspaceID != "" && params.WorkspaceID != "T123" {
		return &domain.CursorPage[domain.User]{Items: []domain.User{}}, nil
	}
	return &domain.CursorPage[domain.User]{Items: []domain.User{}}, nil
}

func (m *mockUserRepoTenant) WithTx(_ pgx.Tx) repository.UserRepository { return m }

func TestUserService_CreateUsesContextWorkspace(t *testing.T) {
	repo := newMockUserRepoTenant()
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	_, err := svc.Create(ctx, domain.CreateUserParams{
		WorkspaceID: "T999",
		Name:        "Alice",
	})
	if err == nil {
		t.Fatal("expected error for mismatched workspace id")
	}
}

func TestUserService_TenantAccessDenied(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U123"] = &domain.User{ID: "U123", WorkspaceID: "T999", Email: "alice@example.com"}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")

	if _, err := svc.Get(ctx, "U123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Get, got %v", err)
	}
	if _, err := svc.GetByEmail(ctx, "alice@example.com"); err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found from GetByEmail, got %v", err)
	}
	if _, err := svc.Update(ctx, "U123", domain.UpdateUserParams{}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Update, got %v", err)
	}
	if _, err := svc.List(ctx, domain.ListUsersParams{WorkspaceID: "T999"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from List, got %v", err)
	}
}

func TestUserService_ListRejectsExternalWorkspaceParticipant(t *testing.T) {
	repo := newMockUserRepoTenant()
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_EXT", "T999")
	ctx = ctxutil.WithIdentity(ctx, "A123")

	if _, err := svc.List(ctx, domain.ListUsersParams{WorkspaceID: "T123"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for external participant list, got %v", err)
	}
}

func TestUserService_CreateRejectsAccountTypeForAgents(t *testing.T) {
	repo := newMockUserRepoTenant()
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	_, err := svc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "agent-a",
		PrincipalType: domain.PrincipalTypeAgent,
		AccountType:   domain.AccountTypeAdmin,
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestUserService_CreateRequiresAdminForAuthenticatedCaller(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	repo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	repo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	memberCtx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	if _, err := svc.Create(memberCtx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "bob",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member create, got %v", err)
	}

	adminCtx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	if _, err := svc.Create(adminCtx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "carol",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}); err != nil {
		t.Fatalf("admin should create member: %v", err)
	}
	if _, err := svc.Create(adminCtx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "dave",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for admin creating admin, got %v", err)
	}

	primaryCtx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	if _, err := svc.Create(primaryCtx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "erin",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}); err != nil {
		t.Fatalf("primary admin should create admin: %v", err)
	}
}

func TestUserService_CreateAllowsMemberToCreateOwnedAgent(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	repo.users["U_OTHER"] = &domain.User{ID: "U_OTHER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	user, err := svc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "session-agent",
		PrincipalType: domain.PrincipalTypeAgent,
		IsBot:         true,
	})
	if err != nil {
		t.Fatalf("member owned agent create should succeed: %v", err)
	}
	if user.OwnerID != "U_MEMBER" {
		t.Fatalf("owner_id = %q, want U_MEMBER", user.OwnerID)
	}

	if _, err := svc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   "T123",
		Name:          "foreign-agent",
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       "U_OTHER",
		IsBot:         true,
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for foreign-owned agent create, got %v", err)
	}
}

func TestUserService_UpdateEnforcesAccountTypeRank(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	repo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	repo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	repo.users["U_TARGET"] = &domain.User{ID: "U_TARGET", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	adminCtx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	accountTypeAdmin := domain.AccountTypeAdmin
	if _, err := svc.Update(adminCtx, "U_TARGET", domain.UpdateUserParams{AccountType: &accountTypeAdmin}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for admin promoting member to admin, got %v", err)
	}

	memberCtx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	newRealName := "self-edit"
	if _, err := svc.Update(memberCtx, "U_MEMBER", domain.UpdateUserParams{RealName: &newRealName}); err != nil {
		t.Fatalf("member self update should succeed: %v", err)
	}
	if _, err := svc.Update(memberCtx, "U_MEMBER", domain.UpdateUserParams{AccountType: &accountTypeAdmin}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member self role change, got %v", err)
	}

	primaryCtx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	if _, err := svc.Update(primaryCtx, "U_TARGET", domain.UpdateUserParams{AccountType: &accountTypeAdmin}); err != nil {
		t.Fatalf("primary admin should promote member to admin: %v", err)
	}
}

func TestUserService_UpdateUsesUserAccountTypeAsCanonical(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	repo.users["U_TARGET"] = &domain.User{ID: "U_TARGET", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	primaryCtx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	primaryCtx = ctxutil.WithPrincipal(primaryCtx, domain.PrincipalTypeHuman, domain.AccountTypePrimaryAdmin, false)
	accountTypeAdmin := domain.AccountTypeAdmin

	updated, err := svc.Update(primaryCtx, "U_TARGET", domain.UpdateUserParams{AccountType: &accountTypeAdmin})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.EffectiveAccountType() != domain.AccountTypeAdmin {
		t.Fatalf("expected updated user to be admin, got %s", updated.EffectiveAccountType())
	}
}

func TestUserService_GetUsesUserAccountType(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U123"] = &domain.User{
		ID:            "U123",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	user, err := svc.Get(ctx, "U123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if user.EffectiveAccountType() != domain.AccountTypeMember {
		t.Fatalf("expected user account type to stay canonical, got %s", user.EffectiveAccountType())
	}
}

func TestUserService_GetHydratesGlobalIdentityFromAccount(t *testing.T) {
	repo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "canonical@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		IsBot:         true,
	}
	repo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "stale@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		IsBot:         false,
		AccountType:   domain.AccountTypeNone,
	}

	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(accountRepo, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	user, err := svc.Get(ctx, "U123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if user.Email != "canonical@example.com" {
		t.Fatalf("email = %q, want canonical@example.com", user.Email)
	}
	if user.PrincipalType != domain.PrincipalTypeAgent {
		t.Fatalf("principal_type = %q, want %q", user.PrincipalType, domain.PrincipalTypeAgent)
	}
	if !user.IsBot {
		t.Fatal("expected hydrated is_bot=true")
	}
}

func TestUserService_GetByEmailUsesUserAccountIdentity(t *testing.T) {
	repo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["alice@example.com"] = accountRepo.byID["A123"]
	repo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}

	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(accountRepo, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	user, err := svc.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetByEmail() error = %v", err)
	}
	if user.WorkspaceID != "T123" {
		t.Fatalf("workspace_id = %q, want T123", user.WorkspaceID)
	}
	if user.EffectiveAccountType() != domain.AccountTypeAdmin {
		t.Fatalf("account_type = %q, want admin", user.EffectiveAccountType())
	}
	if user.AccountID != "A123" {
		t.Fatalf("account_id = %q, want A123", user.AccountID)
	}
}

func TestUserService_UpdateRejectsDirectEmailMutationWhenAccountBacked(t *testing.T) {
	repo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	repo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	repo.users["U_ADMIN"] = &domain.User{
		ID:            "U_ADMIN",
		AccountID:     "A999",
		WorkspaceID:   "T123",
		Email:         "admin@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}

	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(accountRepo, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	email := "new@example.com"
	if _, err := svc.Update(ctx, "U123", domain.UpdateUserParams{Email: &email}); err == nil || !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for direct email update, got %v", err)
	}
}

func TestUserService_ListUsesWorkspaceUsers(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	repo.users["U456"] = &domain.User{
		ID:            "U456",
		AccountID:     "A456",
		WorkspaceID:   "T123",
		Email:         "bot@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		IsBot:         true,
	}

	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	page, err := svc.List(ctx, domain.ListUsersParams{WorkspaceID: "T123", Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("expected tenant mock list to stay empty, got %+v", page.Items)
	}
}
