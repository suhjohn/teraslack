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

type mockWorkspaceRepo struct {
	workspaces map[string]*domain.Workspace
}

func newMockWorkspaceRepo() *mockWorkspaceRepo {
	return &mockWorkspaceRepo{
		workspaces: map[string]*domain.Workspace{},
	}
}

func (m *mockWorkspaceRepo) WithTx(_ pgx.Tx) repository.WorkspaceRepository { return m }

func (m *mockWorkspaceRepo) Create(_ context.Context, params domain.CreateWorkspaceParams) (*domain.Workspace, error) {
	ws := &domain.Workspace{
		ID:              "T_NEW",
		Name:            params.Name,
		Domain:          params.Domain,
		EmailDomain:     params.EmailDomain,
		Description:     params.Description,
		Icon:            params.Icon,
		Discoverability: params.Discoverability,
		DefaultChannels: params.DefaultChannels,
		Preferences:     params.Preferences,
		ProfileFields:   params.ProfileFields,
		Billing:         params.Billing,
	}
	if ws.Discoverability == "" {
		ws.Discoverability = domain.WorkspaceDiscoverabilityInviteOnly
	}
	m.workspaces[ws.ID] = ws
	return ws, nil
}

func (m *mockWorkspaceRepo) Get(_ context.Context, id string) (*domain.Workspace, error) {
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return ws, nil
}

func (m *mockWorkspaceRepo) List(_ context.Context) ([]domain.Workspace, error) {
	out := make([]domain.Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		out = append(out, *ws)
	}
	return out, nil
}

func (m *mockWorkspaceRepo) Update(_ context.Context, id string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error) {
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if params.Name != nil {
		ws.Name = *params.Name
	}
	if params.Description != nil {
		ws.Description = *params.Description
	}
	if params.Icon != nil {
		ws.Icon = *params.Icon
	}
	if params.Discoverability != nil {
		ws.Discoverability = *params.Discoverability
	}
	if params.DefaultChannels != nil {
		ws.DefaultChannels = *params.DefaultChannels
	}
	return ws, nil
}

func (m *mockWorkspaceRepo) ListAdmins(_ context.Context, _ string) ([]domain.User, error) {
	return []domain.User{}, nil
}

func (m *mockWorkspaceRepo) ListOwners(_ context.Context, _ string) ([]domain.User, error) {
	return []domain.User{}, nil
}

func (m *mockWorkspaceRepo) ListBillableInfo(_ context.Context, _ string) ([]domain.WorkspaceBillableInfo, error) {
	return []domain.WorkspaceBillableInfo{}, nil
}

func (m *mockWorkspaceRepo) ListAccessLogs(_ context.Context, _ string, _ int) ([]domain.WorkspaceAccessLog, error) {
	return []domain.WorkspaceAccessLog{}, nil
}

func (m *mockWorkspaceRepo) ListIntegrationLogs(_ context.Context, _ string, _ int) ([]domain.WorkspaceIntegrationLog, error) {
	return []domain.WorkspaceIntegrationLog{}, nil
}

func (m *mockWorkspaceRepo) ListExternalTeams(_ context.Context, _ string) ([]domain.ExternalTeam, error) {
	return []domain.ExternalTeam{}, nil
}

func (m *mockWorkspaceRepo) DisconnectExternalTeam(_ context.Context, _, _ string) error {
	return nil
}

func TestWorkspaceService_TeamInfoUsesContextTeam(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Acme"}
	svc := NewWorkspaceService(workspaceRepo, newMockUserRepoTenant(), nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyTeamID, "T123")

	ws, err := svc.TeamInfo(ctx, "")
	if err != nil {
		t.Fatalf("TeamInfo empty team_id: %v", err)
	}
	if ws.ID != "T123" {
		t.Fatalf("expected T123, got %s", ws.ID)
	}

	if _, err := svc.TeamInfo(ctx, "T999"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for mismatched team_id, got %v", err)
	}
}

func TestWorkspaceService_AdminCreateRequiresAdmin(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{ID: "U123", TeamID: "T123", Name: "alice", PrincipalType: domain.PrincipalTypeHuman}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	if _, err := svc.AdminCreate(ctx, domain.CreateWorkspaceParams{Name: "Acme"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for non-admin create, got %v", err)
	}

	userRepo.users["U123"].AccountType = domain.AccountTypeAdmin
	ws, err := svc.AdminCreate(ctx, domain.CreateWorkspaceParams{Name: "Acme"})
	if err != nil {
		t.Fatalf("AdminCreate admin: %v", err)
	}
	if ws.Name != "Acme" {
		t.Fatalf("expected Acme, got %s", ws.Name)
	}
}

func TestWorkspaceService_AdminCanTargetOtherWorkspace(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T999"] = &domain.Workspace{ID: "T999", Name: "Before"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{ID: "U123", TeamID: "T123", Name: "alice", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ws, err := svc.AdminSetName(ctx, "T999", "After")
	if err != nil {
		t.Fatalf("AdminSetName: %v", err)
	}
	if ws.ID != "T999" || ws.Name != "After" {
		t.Fatalf("unexpected workspace after rename: %+v", ws)
	}
}

func TestWorkspaceService_TransferPrimaryAdmin(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Acme"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", TeamID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	user, err := svc.TransferPrimaryAdmin(ctx, "T123", "U_ADMIN")
	if err != nil {
		t.Fatalf("TransferPrimaryAdmin: %v", err)
	}
	if user.ID != "U_ADMIN" || user.EffectiveAccountType() != domain.AccountTypePrimaryAdmin {
		t.Fatalf("unexpected new primary admin: %+v", user)
	}
	if userRepo.users["U_PRIMARY"].EffectiveAccountType() != domain.AccountTypeAdmin {
		t.Fatalf("expected previous primary admin to be demoted, got %+v", userRepo.users["U_PRIMARY"])
	}
}
