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
		TeamID:        params.TeamID,
		Name:          params.Name,
		RealName:      params.RealName,
		DisplayName:   params.DisplayName,
		Email:         params.Email,
		PrincipalType: params.PrincipalType,
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

func (m *mockUserRepoTenant) GetByTeamEmail(_ context.Context, teamID, email string) (*domain.User, error) {
	for _, u := range m.users {
		if u.TeamID == teamID && u.Email == email {
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
	if params.TeamID != "" && params.TeamID != "T123" {
		return &domain.CursorPage[domain.User]{Items: []domain.User{}}, nil
	}
	return &domain.CursorPage[domain.User]{Items: []domain.User{}}, nil
}

func (m *mockUserRepoTenant) WithTx(_ pgx.Tx) repository.UserRepository { return m }

func TestUserService_CreateUsesContextTeam(t *testing.T) {
	repo := newMockUserRepoTenant()
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyTeamID, "T123")
	_, err := svc.Create(ctx, domain.CreateUserParams{
		TeamID: "T999",
		Name:   "Alice",
	})
	if err == nil {
		t.Fatal("expected error for mismatched team id")
	}
}

func TestUserService_TenantAccessDenied(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U123"] = &domain.User{ID: "U123", TeamID: "T999", Email: "alice@example.com"}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyTeamID, "T123")

	if _, err := svc.Get(ctx, "U123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Get, got %v", err)
	}
	if _, err := svc.GetByEmail(ctx, "alice@example.com"); err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found from GetByEmail, got %v", err)
	}
	if _, err := svc.Update(ctx, "U123", domain.UpdateUserParams{}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Update, got %v", err)
	}
	if _, err := svc.List(ctx, domain.ListUsersParams{TeamID: "T999"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from List, got %v", err)
	}
}

func TestUserService_CreateRejectsAccountTypeForAgents(t *testing.T) {
	repo := newMockUserRepoTenant()
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	_, err := svc.Create(ctx, domain.CreateUserParams{
		TeamID:        "T123",
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
	repo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	repo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	repo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	svc := NewUserService(repo, nil, mockTxBeginner{}, nil)

	memberCtx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	if _, err := svc.Create(memberCtx, domain.CreateUserParams{
		TeamID:        "T123",
		Name:          "bob",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member create, got %v", err)
	}

	adminCtx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	if _, err := svc.Create(adminCtx, domain.CreateUserParams{
		TeamID:        "T123",
		Name:          "carol",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}); err != nil {
		t.Fatalf("admin should create member: %v", err)
	}
	if _, err := svc.Create(adminCtx, domain.CreateUserParams{
		TeamID:        "T123",
		Name:          "dave",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for admin creating admin, got %v", err)
	}

	primaryCtx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	if _, err := svc.Create(primaryCtx, domain.CreateUserParams{
		TeamID:        "T123",
		Name:          "erin",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}); err != nil {
		t.Fatalf("primary admin should create admin: %v", err)
	}
}

func TestUserService_UpdateEnforcesAccountTypeRank(t *testing.T) {
	repo := newMockUserRepoTenant()
	repo.users["U_MEMBER"] = &domain.User{ID: "U_MEMBER", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	repo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	repo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	repo.users["U_TARGET"] = &domain.User{ID: "U_TARGET", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
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
