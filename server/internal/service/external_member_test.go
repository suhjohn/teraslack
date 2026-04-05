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

type capturingExternalMemberRepo struct {
	created    *domain.ExternalMember
	createHost string
	revokedID  string
}

func (r *capturingExternalMemberRepo) WithTx(_ pgx.Tx) repository.ExternalMemberRepository { return r }
func (r *capturingExternalMemberRepo) Create(_ context.Context, params domain.CreateExternalMemberParams, hostWorkspaceID string) (*domain.ExternalMember, error) {
	item := &domain.ExternalMember{
		ID:                  "EM123",
		ConversationID:      params.ConversationID,
		HostWorkspaceID:     hostWorkspaceID,
		ExternalWorkspaceID: params.ExternalWorkspaceID,
		AccountID:           params.AccountID,
		AccessMode:          params.AccessMode,
		AllowedCapabilities: append([]string(nil), params.AllowedCapabilities...),
		InvitedBy:           params.InvitedBy,
		CreatedAt:           time.Now().UTC(),
		ExpiresAt:           params.ExpiresAt,
	}
	r.created = item
	r.createHost = hostWorkspaceID
	return item, nil
}
func (r *capturingExternalMemberRepo) Get(_ context.Context, id string) (*domain.ExternalMember, error) {
	if r.created == nil || r.created.ID != id {
		return nil, domain.ErrNotFound
	}
	return r.created, nil
}
func (r *capturingExternalMemberRepo) GetActiveByConversationAndAccount(_ context.Context, _, _ string) (*domain.ExternalMember, error) {
	return nil, domain.ErrNotFound
}
func (r *capturingExternalMemberRepo) ListActiveByAccountAndWorkspace(_ context.Context, _, _ string) ([]domain.ExternalMember, error) {
	return nil, nil
}
func (r *capturingExternalMemberRepo) ListByConversation(_ context.Context, conversationID string) ([]domain.ExternalMember, error) {
	if r.created == nil || r.created.ConversationID != conversationID {
		return []domain.ExternalMember{}, nil
	}
	return []domain.ExternalMember{*r.created}, nil
}
func (r *capturingExternalMemberRepo) Update(_ context.Context, id string, params domain.UpdateExternalMemberParams) (*domain.ExternalMember, error) {
	return r.created, nil
}
func (r *capturingExternalMemberRepo) Revoke(_ context.Context, id string, revokedAt time.Time) error {
	r.revokedID = id
	return nil
}
func (r *capturingExternalMemberRepo) RevokeByExternalWorkspace(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func TestExternalMemberService_CreateRequiresConnectedExternalWorkspace(t *testing.T) {
	repo := &capturingExternalMemberRepo{}
	accountRepo := newMockAccountRepo()
	convRepo := &conversationRepoStub{conversation: &domain.Conversation{ID: "C123", WorkspaceID: "T123", Type: domain.ConversationTypePrivateChannel}}
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.externalWorkspaces["T123|T999"] = &domain.ExternalWorkspace{
		ID:                  "EW123",
		ExternalWorkspaceID: "T999",
		Name:                "External",
		Connected:           true,
	}
	svc := NewExternalMemberService(repo, accountRepo, convRepo, workspaceRepo)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)
	item, err := svc.Create(ctx, "C123", domain.CreateExternalMemberParams{
		ExternalWorkspaceID: "T999",
		Email:               "user@example.com",
		Name:                "External User",
		AccessMode:          domain.ExternalPrincipalAccessModeSharedReadOnly,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repo.createHost != "T123" {
		t.Fatalf("host workspace = %q, want T123", repo.createHost)
	}
	if item.Account == nil || item.Account.Email != "user@example.com" {
		t.Fatalf("expected resolved account, got %+v", item.Account)
	}
	if len(item.AllowedCapabilities) == 0 {
		t.Fatal("expected normalized capabilities")
	}
}

func TestExternalMemberService_CreateRejectsDisconnectedExternalWorkspace(t *testing.T) {
	repo := &capturingExternalMemberRepo{}
	accountRepo := newMockAccountRepo()
	convRepo := &conversationRepoStub{conversation: &domain.Conversation{ID: "C123", WorkspaceID: "T123", Type: domain.ConversationTypePrivateChannel}}
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.externalWorkspaces["T123|T999"] = &domain.ExternalWorkspace{
		ID:                  "EW123",
		ExternalWorkspaceID: "T999",
		Name:                "External",
		Connected:           false,
	}
	svc := NewExternalMemberService(repo, accountRepo, convRepo, workspaceRepo)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)
	if _, err := svc.Create(ctx, "C123", domain.CreateExternalMemberParams{
		ExternalWorkspaceID: "T999",
		Email:               "user@example.com",
		AccessMode:          domain.ExternalPrincipalAccessModeShared,
	}); err == nil {
		t.Fatal("expected disconnected external workspace to be rejected")
	}
}

func TestExternalMemberService_CreateIgnoresForgedAdminContext(t *testing.T) {
	repo := &capturingExternalMemberRepo{}
	accountRepo := newMockAccountRepo()
	convRepo := &conversationRepoStub{
		conversation: &domain.Conversation{ID: "C123", WorkspaceID: "T123", Type: domain.ConversationTypePublicChannel},
	}
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.externalWorkspaces["T123|T999"] = &domain.ExternalWorkspace{
		ID:                  "EW123",
		ExternalWorkspaceID: "T999",
		Name:                "External",
		Connected:           true,
	}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_ACTOR": {ID: "U_ACTOR", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}
	accessSvc := NewConversationAccessService(
		&conversationAccessRepoStub{},
		convRepo,
		userRepo,
		&roleAssignmentRepoStub{},
		nil,
		mockTxBeginner{},
		nil,
	)
	svc := NewExternalMemberService(repo, accountRepo, convRepo, workspaceRepo)
	svc.SetConversationAccessService(accessSvc)

	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	if _, err := svc.Create(ctx, "C123", domain.CreateExternalMemberParams{
		ExternalWorkspaceID: "T999",
		Email:               "user@example.com",
		AccessMode:          domain.ExternalPrincipalAccessModeSharedReadOnly,
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Create() error = %v, want forbidden", err)
	}
}

func TestExternalMemberService_CreateUsesCanonicalWorkspaceAdminUser(t *testing.T) {
	repo := &capturingExternalMemberRepo{}
	accountRepo := newMockAccountRepo()
	convRepo := &conversationRepoStub{
		conversation: &domain.Conversation{ID: "C123", WorkspaceID: "T123", Type: domain.ConversationTypePublicChannel},
	}
	workspaceRepo := newMockWorkspaceRepo()
	workspaceRepo.externalWorkspaces["T123|T999"] = &domain.ExternalWorkspace{
		ID:                  "EW123",
		ExternalWorkspaceID: "T999",
		Name:                "External",
		Connected:           true,
	}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_ACTOR": {ID: "U_ACTOR", AccountID: "A_ADMIN", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin},
		},
	}
	accessSvc := NewConversationAccessService(
		&conversationAccessRepoStub{},
		convRepo,
		userRepo,
		&roleAssignmentRepoStub{},
		nil,
		mockTxBeginner{},
		nil,
	)
	svc := NewExternalMemberService(repo, accountRepo, convRepo, workspaceRepo)
	svc.SetConversationAccessService(accessSvc)

	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A_ADMIN")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	item, err := svc.Create(ctx, "C123", domain.CreateExternalMemberParams{
		ExternalWorkspaceID: "T999",
		Email:               "user@example.com",
		AccessMode:          domain.ExternalPrincipalAccessModeSharedReadOnly,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if item.Account == nil || item.Account.Email != "user@example.com" {
		t.Fatalf("expected resolved account, got %+v", item.Account)
	}
}
