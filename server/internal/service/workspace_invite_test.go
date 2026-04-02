package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockWorkspaceInviteRepo struct {
	invites map[string]*domain.WorkspaceInvite
}

func newMockWorkspaceInviteRepo() *mockWorkspaceInviteRepo {
	return &mockWorkspaceInviteRepo{invites: make(map[string]*domain.WorkspaceInvite)}
}

func (m *mockWorkspaceInviteRepo) WithTx(_ pgx.Tx) repository.WorkspaceInviteRepository { return m }

func (m *mockWorkspaceInviteRepo) Create(_ context.Context, params domain.CreateWorkspaceInviteParams, tokenHash string) (*domain.WorkspaceInvite, error) {
	invite := &domain.WorkspaceInvite{
		ID:          "WI1",
		WorkspaceID: params.WorkspaceID,
		Email:       params.Email,
		InvitedBy:   params.InvitedBy,
		ExpiresAt:   params.ExpiresAt,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	m.invites[tokenHash] = invite
	return invite, nil
}

func (m *mockWorkspaceInviteRepo) GetByTokenHash(_ context.Context, tokenHash string) (*domain.WorkspaceInvite, error) {
	invite, ok := m.invites[tokenHash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return invite, nil
}

func (m *mockWorkspaceInviteRepo) MarkAccepted(_ context.Context, id, acceptedByAccountID string, acceptedAt time.Time) error {
	for _, invite := range m.invites {
		if invite.ID != id {
			continue
		}
		invite.AcceptedByAccountID = acceptedByAccountID
		invite.AcceptedAt = &acceptedAt
		invite.UpdatedAt = acceptedAt
		return nil
	}
	return domain.ErrNotFound
}

func TestWorkspaceInviteService_AcceptCreatesMembershipInInvitedWorkspace(t *testing.T) {
	inviteRepo := newMockWorkspaceInviteRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	code := "invite_accept"

	userRepo.users["U_ACTOR"] = &domain.User{
		ID:            "U_ACTOR",
		WorkspaceID:   "T_CURRENT",
		Name:          "alice",
		RealName:      "Alice Example",
		DisplayName:   "Alice",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
		Profile:       domain.UserProfile{Title: "Engineer"},
	}
	inviteRepo.invites[crypto.HashToken(code)] = &domain.WorkspaceInvite{
		ID:          "WI1",
		WorkspaceID: "T_INVITED",
		Email:       "alice@example.com",
		InvitedBy:   "U_ADMIN",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	svc := NewWorkspaceInviteService(inviteRepo, userRepo, nil, mockTxBeginner{}, "https://teraslack.ai")
	svc.SetIdentityRepositories(accountRepo)
	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T_CURRENT")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	result, err := svc.Accept(ctx, code)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if result.Invite.AcceptedByAccountID == "" || result.Invite.AcceptedAt == nil {
		t.Fatalf("expected accepted invite, got %+v", result.Invite)
	}
	if result.User == nil {
		t.Fatalf("expected workspace user to be created on invite accept, got %+v", result)
	}
	account, err := accountRepo.GetByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("expected account for accepted invite: %v", err)
	}
	user, err := userRepo.GetByWorkspaceAndAccount(context.Background(), "T_INVITED", account.ID)
	if err != nil {
		t.Fatalf("expected user in invited workspace: %v", err)
	}
	if user.ID != result.User.ID {
		t.Fatalf("expected accepted user to be persisted, got result=%+v repo=%+v", result.User, user)
	}
	if user.AccountID != account.ID {
		t.Fatalf("expected accepted user to link account %s, got %+v", account.ID, user)
	}
}

func TestWorkspaceInviteService_AcceptReusesExistingUser(t *testing.T) {
	inviteRepo := newMockWorkspaceInviteRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	code := "invite_existing_member"

	userRepo.users["U_ACTOR"] = &domain.User{
		ID:            "U_ACTOR",
		WorkspaceID:   "T_CURRENT",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		WorkspaceID:   "T_INVITED",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["alice@example.com"] = accountRepo.byID["A123"]
	inviteRepo.invites[crypto.HashToken(code)] = &domain.WorkspaceInvite{
		ID:          "WI1",
		WorkspaceID: "T_INVITED",
		Email:       "alice@example.com",
		InvitedBy:   "U_ADMIN",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	svc := NewWorkspaceInviteService(inviteRepo, userRepo, nil, mockTxBeginner{}, "")
	svc.SetIdentityRepositories(accountRepo)
	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T_CURRENT")
	ctx = ctxutil.WithIdentity(ctx, "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	result, err := svc.Accept(ctx, code)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if result.User.ID != "U123" {
		t.Fatalf("expected existing invited member to be reused, got %+v", result.User)
	}
}

func TestWorkspaceInviteService_AcceptCreatesWorkspaceUserForExistingAccount(t *testing.T) {
	inviteRepo := newMockWorkspaceInviteRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	code := "invite_existing_user"

	userRepo.users["U_ACTOR"] = &domain.User{
		ID:            "U_ACTOR",
		WorkspaceID:   "T_CURRENT",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		WorkspaceID:   "T_INVITED",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["alice@example.com"] = accountRepo.byID["A123"]
	inviteRepo.invites[crypto.HashToken(code)] = &domain.WorkspaceInvite{
		ID:          "WI1",
		WorkspaceID: "T_INVITED",
		Email:       "alice@example.com",
		InvitedBy:   "U_ADMIN",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	svc := NewWorkspaceInviteService(inviteRepo, userRepo, nil, mockTxBeginner{}, "")
	svc.SetIdentityRepositories(accountRepo)
	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T_CURRENT")
	ctx = ctxutil.WithIdentity(ctx, "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	result, err := svc.Accept(ctx, code)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if result.User == nil {
		t.Fatalf("expected workspace user to be created, got %+v", result)
	}
	user, err := userRepo.GetByWorkspaceAndAccount(context.Background(), "T_INVITED", "A123")
	if err != nil {
		t.Fatalf("expected workspace user to be created: %v", err)
	}
	if user.ID != result.User.ID {
		t.Fatalf("expected created user to match response, got %+v %+v", user, result.User)
	}
}

func TestWorkspaceInviteService_AcceptRejectsMismatchedEmail(t *testing.T) {
	inviteRepo := newMockWorkspaceInviteRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	code := "invite_mismatch"

	userRepo.users["U_ACTOR"] = &domain.User{
		ID:            "U_ACTOR",
		WorkspaceID:   "T_CURRENT",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	inviteRepo.invites[crypto.HashToken(code)] = &domain.WorkspaceInvite{
		ID:          "WI1",
		WorkspaceID: "T_INVITED",
		Email:       "bob@example.com",
		InvitedBy:   "U_ADMIN",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	svc := NewWorkspaceInviteService(inviteRepo, userRepo, nil, mockTxBeginner{}, "")
	svc.SetIdentityRepositories(accountRepo)
	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T_CURRENT")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	if _, err := svc.Accept(ctx, code); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Accept() error = %v, want forbidden", err)
	}
}

func TestWorkspaceInviteService_CreateReturnsRawCode(t *testing.T) {
	inviteRepo := newMockWorkspaceInviteRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_ADMIN"] = &domain.User{
		ID:            "U_ADMIN",
		WorkspaceID:   "T123",
		Email:         "admin@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}

	svc := NewWorkspaceInviteService(inviteRepo, userRepo, nil, mockTxBeginner{}, "https://teraslack.ai")
	svc.SetIdentityRepositories(accountRepo)
	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	result, err := svc.Create(ctx, "T123", "alice@example.com")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Code == "" || !strings.HasPrefix(result.Code, "invite_") {
		t.Fatalf("expected raw invite code, got %+v", result)
	}
	if !strings.Contains(result.InviteURL, result.Code) {
		t.Fatalf("expected invite_url to contain raw code, got %+v", result)
	}
}
