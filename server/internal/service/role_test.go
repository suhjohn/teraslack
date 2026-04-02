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

type mockRoleAssignmentRepo struct {
	roles map[string][]domain.DelegatedRole
}

func newMockRoleAssignmentRepo() *mockRoleAssignmentRepo {
	return &mockRoleAssignmentRepo{roles: map[string][]domain.DelegatedRole{}}
}

func (m *mockRoleAssignmentRepo) WithTx(_ pgx.Tx) repository.RoleAssignmentRepository { return m }

func (m *mockRoleAssignmentRepo) ListByUser(_ context.Context, _, userID string) ([]domain.DelegatedRole, error) {
	roles := m.roles[userID]
	if roles == nil {
		return []domain.DelegatedRole{}, nil
	}
	return append([]domain.DelegatedRole(nil), roles...), nil
}

func (m *mockRoleAssignmentRepo) ReplaceForUser(_ context.Context, _, userID string, roles []domain.DelegatedRole, _ string) error {
	m.roles[userID] = append([]domain.DelegatedRole(nil), roles...)
	return nil
}

func TestRoleService_SetUserRoles_RequiresPrimaryAdminOrRolesAdmin(t *testing.T) {
	roleRepo := newMockRoleAssignmentRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	userRepo.users["U_TARGET"] = &domain.User{ID: "U_TARGET", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewRoleService(roleRepo, userRepo)

	adminCtx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	if _, err := svc.SetUserRoles(adminCtx, "U_TARGET", []domain.DelegatedRole{domain.DelegatedRoleChannelsAdmin}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for plain admin role write, got %v", err)
	}

	primaryCtx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	roles, err := svc.SetUserRoles(primaryCtx, "U_TARGET", []domain.DelegatedRole{domain.DelegatedRoleChannelsAdmin})
	if err != nil {
		t.Fatalf("primary admin set roles: %v", err)
	}
	if len(roles) != 1 || roles[0] != domain.DelegatedRoleChannelsAdmin {
		t.Fatalf("unexpected roles: %+v", roles)
	}
}

func TestRoleService_AdminWithRolesAdminCanSetManageableUserRoles(t *testing.T) {
	roleRepo := newMockRoleAssignmentRepo()
	roleRepo.roles["U_ADMIN"] = []domain.DelegatedRole{domain.DelegatedRoleRolesAdmin}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	userRepo.users["U_TARGET"] = &domain.User{ID: "U_TARGET", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewRoleService(roleRepo, userRepo)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	roles, err := svc.SetUserRoles(ctx, "U_TARGET", []domain.DelegatedRole{domain.DelegatedRoleIntegrationsAdmin})
	if err != nil {
		t.Fatalf("roles_admin set roles: %v", err)
	}
	if len(roles) != 1 || roles[0] != domain.DelegatedRoleIntegrationsAdmin {
		t.Fatalf("unexpected roles: %+v", roles)
	}
}

func TestRoleService_ListUserRoles_AllowsSelf(t *testing.T) {
	roleRepo := newMockRoleAssignmentRepo()
	roleRepo.roles["U123"] = []domain.DelegatedRole{domain.DelegatedRoleSupportReadonly}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{ID: "U123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember}
	svc := NewRoleService(roleRepo, userRepo)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	roles, err := svc.ListUserRoles(ctx, "U123")
	if err != nil {
		t.Fatalf("list own roles: %v", err)
	}
	if len(roles) != 1 || roles[0] != domain.DelegatedRoleSupportReadonly {
		t.Fatalf("unexpected roles: %+v", roles)
	}
}

func TestRoleService_UsesMembershipAccountTypeForTargetAuthorization(t *testing.T) {
	roleRepo := newMockRoleAssignmentRepo()
	roleRepo.roles["U_ADMIN"] = []domain.DelegatedRole{domain.DelegatedRoleRolesAdmin}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	userRepo.users["U_TARGET"] = &domain.User{ID: "U_TARGET", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	membershipRepo := newMockWorkspaceMembershipRepo()
	membershipRepo.byUser["U_TARGET"] = &domain.WorkspaceMembership{
		ID:          "WM_TARGET",
		AccountID:   "A_TARGET",
		WorkspaceID: "T123",
		UserID:      "U_TARGET",
		AccountType: domain.AccountTypeMember,
	}
	membershipRepo.byWorkspaceAccount["T123|A_TARGET"] = membershipRepo.byUser["U_TARGET"]
	svc := NewRoleService(roleRepo, userRepo)
	svc.SetIdentityRepositories(membershipRepo)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)
	roles, err := svc.SetUserRoles(ctx, "U_TARGET", []domain.DelegatedRole{domain.DelegatedRoleIntegrationsAdmin})
	if err != nil {
		t.Fatalf("expected membership-backed member target to be manageable: %v", err)
	}
	if len(roles) != 1 || roles[0] != domain.DelegatedRoleIntegrationsAdmin {
		t.Fatalf("unexpected roles: %+v", roles)
	}
}
