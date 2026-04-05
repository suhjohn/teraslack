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

type mockWorkspaceRepo struct {
	workspaces         map[string]*domain.Workspace
	externalWorkspaces map[string]*domain.ExternalWorkspace
}

type workspaceExternalMemberRepoStub struct {
	hostWorkspaceID     string
	externalWorkspaceID string
	revokedAt           time.Time
}

func newMockWorkspaceRepo() *mockWorkspaceRepo {
	return &mockWorkspaceRepo{
		workspaces:         map[string]*domain.Workspace{},
		externalWorkspaces: map[string]*domain.ExternalWorkspace{},
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

func (m *mockWorkspaceRepo) ListExternalWorkspaces(_ context.Context, _ string) ([]domain.ExternalWorkspace, error) {
	out := make([]domain.ExternalWorkspace, 0, len(m.externalWorkspaces))
	for _, workspace := range m.externalWorkspaces {
		out = append(out, *workspace)
	}
	return out, nil
}

func (m *mockWorkspaceRepo) CreateExternalWorkspace(_ context.Context, params domain.CreateExternalWorkspaceParams) (*domain.ExternalWorkspace, error) {
	workspace := &domain.ExternalWorkspace{
		ID:                  "EW_NEW",
		ExternalWorkspaceID: params.ExternalWorkspaceID,
		Name:                params.Name,
		ConnectionType:      params.ConnectionType,
		Connected:           true,
	}
	m.externalWorkspaces[params.WorkspaceID+"|"+params.ExternalWorkspaceID] = workspace
	return workspace, nil
}

func (m *mockWorkspaceRepo) GetExternalWorkspace(_ context.Context, workspaceID, externalWorkspaceID string) (*domain.ExternalWorkspace, error) {
	workspace, ok := m.externalWorkspaces[workspaceID+"|"+externalWorkspaceID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return workspace, nil
}

func (m *mockWorkspaceRepo) DisconnectExternalWorkspace(_ context.Context, workspaceID, externalWorkspaceID string) error {
	workspace, ok := m.externalWorkspaces[workspaceID+"|"+externalWorkspaceID]
	if !ok {
		return domain.ErrNotFound
	}
	workspace.Connected = false
	return nil
}

func (r *workspaceExternalMemberRepoStub) WithTx(_ pgx.Tx) repository.ExternalMemberRepository {
	return r
}
func (r *workspaceExternalMemberRepoStub) Create(_ context.Context, _ domain.CreateExternalMemberParams, _ string) (*domain.ExternalMember, error) {
	return nil, nil
}
func (r *workspaceExternalMemberRepoStub) Get(_ context.Context, _ string) (*domain.ExternalMember, error) {
	return nil, domain.ErrNotFound
}
func (r *workspaceExternalMemberRepoStub) GetActiveByConversationAndAccount(_ context.Context, _, _ string) (*domain.ExternalMember, error) {
	return nil, domain.ErrNotFound
}
func (r *workspaceExternalMemberRepoStub) ListActiveByAccountAndWorkspace(_ context.Context, _, _ string) ([]domain.ExternalMember, error) {
	return nil, nil
}
func (r *workspaceExternalMemberRepoStub) ListByConversation(_ context.Context, _ string) ([]domain.ExternalMember, error) {
	return nil, nil
}
func (r *workspaceExternalMemberRepoStub) Update(_ context.Context, _ string, _ domain.UpdateExternalMemberParams) (*domain.ExternalMember, error) {
	return nil, nil
}
func (r *workspaceExternalMemberRepoStub) Revoke(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (r *workspaceExternalMemberRepoStub) RevokeByExternalWorkspace(_ context.Context, hostWorkspaceID, externalWorkspaceID string, revokedAt time.Time) error {
	r.hostWorkspaceID = hostWorkspaceID
	r.externalWorkspaceID = externalWorkspaceID
	r.revokedAt = revokedAt
	return nil
}

func TestWorkspaceService_WorkspaceInfoUsesContextWorkspace(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Acme"}
	svc := NewWorkspaceService(workspaceRepo, newMockUserRepoTenant(), nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")

	ws, err := svc.WorkspaceInfo(ctx, "")
	if err != nil {
		t.Fatalf("WorkspaceInfo empty workspace_id: %v", err)
	}
	if ws.ID != "T123" {
		t.Fatalf("expected T123, got %s", ws.ID)
	}

	if _, err := svc.WorkspaceInfo(ctx, "T999"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for mismatched workspace_id, got %v", err)
	}
}

func TestWorkspaceService_AdminCreateRequiresAdmin(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	userRepo.users["U_ADMIN"] = &domain.User{
		ID:            "U_ADMIN",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(accountRepo)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeNone, false)
	ws, err := svc.AdminCreate(ctx, domain.CreateWorkspaceParams{Name: "Acme"})
	if err != nil {
		t.Fatalf("AdminCreate account-first creator: %v", err)
	}
	if ws.Name != "Acme" {
		t.Fatalf("expected Acme, got %s", ws.Name)
	}
	createdUser, err := userRepo.GetByTeamEmail(context.Background(), ws.ID, "alice@example.com")
	if err != nil {
		t.Fatalf("expected creator workspace user in new workspace: %v", err)
	}
	if createdUser.EffectiveAccountType() != domain.AccountTypePrimaryAdmin {
		t.Fatalf("expected primary admin workspace user, got %s", createdUser.EffectiveAccountType())
	}
}

func TestWorkspaceService_AdminListReturnsWorkspacesForAccountUsers(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Current"}
	workspaceRepo.workspaces["T999"] = &domain.Workspace{ID: "T999", Name: "Other"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	userRepo.users["U999"] = &domain.User{
		ID:            "U999",
		AccountID:     "A123",
		WorkspaceID:   "T999",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(newMockAccountRepo())

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A123")

	workspaces, err := svc.AdminList(ctx)
	if err != nil {
		t.Fatalf("AdminList: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(workspaces))
	}
	seen := map[string]bool{}
	for _, workspace := range workspaces {
		seen[workspace.ID] = true
	}
	if !seen["T123"] || !seen["T999"] {
		t.Fatalf("expected account-linked workspace users for T123 and T999, got %+v", workspaces)
	}
}

func TestWorkspaceService_AdminListUsesAccountLinkedUsers(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Current"}
	workspaceRepo.workspaces["T999"] = &domain.Workspace{ID: "T999", Name: "Other"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Name:          "alice",
		Email:         "primary@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	userRepo.users["U999"] = &domain.User{
		ID:            "U999",
		AccountID:     "A123",
		WorkspaceID:   "T999",
		Name:          "alice",
		Email:         "primary@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	workspaces, err := svc.AdminList(ctx)
	if err != nil {
		t.Fatalf("AdminList: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(workspaces))
	}
	seen := map[string]bool{}
	for _, workspace := range workspaces {
		seen[workspace.ID] = true
	}
	if !seen["T123"] || !seen["T999"] {
		t.Fatalf("expected account-linked workspace users for T123 and T999, got %+v", workspaces)
	}
}

func TestWorkspaceService_DisconnectExternalWorkspaceRevokesExternalMembers(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Current"}
	workspaceRepo.externalWorkspaces["T123|T999"] = &domain.ExternalWorkspace{
		ID:                  "EW123",
		ExternalWorkspaceID: "T999",
		Name:                "External",
		Connected:           true,
	}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_ADMIN"] = &domain.User{
		ID:            "U_ADMIN",
		WorkspaceID:   "T123",
		Email:         "admin@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	externalMemberRepo := &workspaceExternalMemberRepoStub{}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(externalMemberRepo)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)
	if err := svc.DisconnectExternalWorkspace(ctx, "T123", "T999"); err != nil {
		t.Fatalf("DisconnectExternalWorkspace() error = %v", err)
	}
	if externalMemberRepo.hostWorkspaceID != "T123" || externalMemberRepo.externalWorkspaceID != "T999" {
		t.Fatalf("unexpected revoke target: %+v", externalMemberRepo)
	}
	if externalMemberRepo.revokedAt.IsZero() {
		t.Fatal("expected revoke timestamp to be set")
	}
}

func TestWorkspaceService_AdminCreateKeepsCreatorAccountIdentity(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_ADMIN"] = &domain.User{
		ID:            "U_ADMIN",
		WorkspaceID:   "T123",
		Name:          "alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["alice@example.com"] = accountRepo.byID["A123"]
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)
	svc.SetIdentityRepositories(accountRepo)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	ws, err := svc.AdminCreate(ctx, domain.CreateWorkspaceParams{Name: "Acme"})
	if err != nil {
		t.Fatalf("AdminCreate admin: %v", err)
	}
	createdUser, err := userRepo.GetByTeamEmail(context.Background(), ws.ID, "alice@example.com")
	if err != nil {
		t.Fatalf("expected creator workspace user in new workspace: %v", err)
	}
	if createdUser.AccountID != "A123" || createdUser.WorkspaceID != ws.ID {
		t.Fatalf("unexpected synced identity for creator: %+v", createdUser)
	}
}

func TestWorkspaceService_AdminCannotTargetOtherWorkspace(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Current"}
	workspaceRepo.workspaces["T999"] = &domain.Workspace{ID: "T999", Name: "Before"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{ID: "U123", WorkspaceID: "T123", Name: "alice", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	if _, err := svc.AdminSetName(ctx, "T999", "After"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for cross-workspace admin update, got %v", err)
	}
}

func TestWorkspaceService_TransferPrimaryAdmin(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Acme"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_PRIMARY"] = &domain.User{ID: "U_PRIMARY", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypePrimaryAdmin}
	userRepo.users["U_ADMIN"] = &domain.User{ID: "U_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin}
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

func TestWorkspaceService_TransferPrimaryAdminRejectsCrossWorkspaceTarget(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Acme"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_PRIMARY"] = &domain.User{
		ID:            "U_PRIMARY",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypePrimaryAdmin,
	}
	userRepo.users["U_OTHER"] = &domain.User{
		ID:            "U_OTHER",
		WorkspaceID:   "T999",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	if _, err := svc.TransferPrimaryAdmin(ctx, "T123", "U_OTHER"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for cross-workspace transfer target, got %v", err)
	}
}

func TestWorkspaceService_TransferPrimaryAdminRejectsNonHumanTarget(t *testing.T) {
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.workspaces["T123"] = &domain.Workspace{ID: "T123", Name: "Acme"}
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_PRIMARY"] = &domain.User{
		ID:            "U_PRIMARY",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypePrimaryAdmin,
	}
	userRepo.users["U_AGENT"] = &domain.User{
		ID:            "U_AGENT",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeAgent,
		AccountType:   domain.AccountTypeNone,
		IsBot:         true,
	}
	svc := NewWorkspaceService(workspaceRepo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_PRIMARY", "T123")
	if _, err := svc.TransferPrimaryAdmin(ctx, "T123", "U_AGENT"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for non-human transfer target, got %v", err)
	}
}
