package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockConversationRepoTenant struct {
	conversations  map[string]*domain.Conversation
	accountMembers map[string]map[string]struct{}
	lastCreate     domain.CreateConversationParams
	lastAddMember  struct {
		conversationID string
		userID         string
	}
	lastAddMemberByAccount struct {
		conversationID string
		accountID      string
	}
}

func newMockConversationRepoTenant() *mockConversationRepoTenant {
	return &mockConversationRepoTenant{conversations: make(map[string]*domain.Conversation)}
}

func (m *mockConversationRepoTenant) Create(_ context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	m.lastCreate = params
	conv := &domain.Conversation{ID: "C123", WorkspaceID: params.WorkspaceID, CreatorID: params.CreatorID, Name: params.Name, Type: params.Type}
	conv.OwnerType = params.OwnerType
	conv.OwnerAccountID = params.OwnerAccountID
	conv.OwnerWorkspaceID = params.OwnerWorkspaceID
	m.conversations[conv.ID] = conv
	return conv, nil
}

func (m *mockConversationRepoTenant) Get(_ context.Context, id string) (*domain.Conversation, error) {
	conv, ok := m.conversations[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return conv, nil
}

func (m *mockConversationRepoTenant) GetCanonicalDM(_ context.Context, _, _, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrNotFound
}

func (m *mockConversationRepoTenant) Update(_ context.Context, id string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return m.Get(context.Background(), id)
}

func (m *mockConversationRepoTenant) SetTopic(_ context.Context, id string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return m.Get(context.Background(), id)
}

func (m *mockConversationRepoTenant) SetPurpose(_ context.Context, id string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return m.Get(context.Background(), id)
}

func (m *mockConversationRepoTenant) List(_ context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	var items []domain.Conversation
	for _, conv := range m.conversations {
		if params.WorkspaceID != "" && conversationWorkspaceID(conv) != params.WorkspaceID {
			continue
		}
		if params.AccountID != "" && conv.Type != domain.ConversationTypePublicChannel {
			members := m.accountMembers[conv.ID]
			if _, ok := members[params.AccountID]; !ok {
				continue
			}
		}
		items = append(items, *conv)
	}
	return &domain.CursorPage[domain.Conversation]{Items: items}, nil
}

func (m *mockConversationRepoTenant) Archive(_ context.Context, id string) error   { return nil }
func (m *mockConversationRepoTenant) Unarchive(_ context.Context, id string) error { return nil }
func (m *mockConversationRepoTenant) AddMember(_ context.Context, conversationID, userID string) error {
	m.lastAddMember.conversationID = conversationID
	m.lastAddMember.userID = userID
	return nil
}
func (m *mockConversationRepoTenant) AddMemberByAccount(_ context.Context, conversationID, accountID string) error {
	m.lastAddMemberByAccount.conversationID = conversationID
	m.lastAddMemberByAccount.accountID = accountID
	return nil
}
func (m *mockConversationRepoTenant) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConversationRepoTenant) RemoveMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConversationRepoTenant) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (m *mockConversationRepoTenant) ListMemberAccounts(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (m *mockConversationRepoTenant) IsMember(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (m *mockConversationRepoTenant) IsAccountMember(_ context.Context, conversationID, accountID string) (bool, error) {
	if members, ok := m.accountMembers[conversationID]; ok {
		_, ok := members[accountID]
		return ok, nil
	}
	return false, nil
}
func (m *mockConversationRepoTenant) WithTx(_ pgx.Tx) repository.ConversationRepository { return m }

func TestConversationService_TenantAccessDenied(t *testing.T) {
	repo := newMockConversationRepoTenant()
	repo.conversations["C123"] = &domain.Conversation{ID: "C123", WorkspaceID: "T999", Name: "general"}
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")

	if _, err := svc.Get(ctx, "C123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Get, got %v", err)
	}
	if _, err := svc.ListMembers(ctx, "C123", "", 10); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from ListMembers, got %v", err)
	}
	if _, err := svc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: "T999",
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		CreatorID:   "U123",
		Name:        "secret",
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Create, got %v", err)
	}
}

type canonicalDMRepoStub struct {
	existing    *domain.Conversation
	createUsed  bool
	lastList    domain.ListConversationsParams
	lastTopic   domain.SetTopicParams
	lastPurpose domain.SetPurposeParams
}

func (r *canonicalDMRepoStub) WithTx(_ pgx.Tx) repository.ConversationRepository { return r }
func (r *canonicalDMRepoStub) Create(_ context.Context, _ domain.CreateConversationParams) (*domain.Conversation, error) {
	r.createUsed = true
	return &domain.Conversation{ID: "D_NEW", WorkspaceID: "T123", Type: domain.ConversationTypeIM}, nil
}
func (r *canonicalDMRepoStub) GetCanonicalDM(_ context.Context, _, _, _ string) (*domain.Conversation, error) {
	if r.existing == nil {
		return nil, domain.ErrNotFound
	}
	return r.existing, nil
}
func (r *canonicalDMRepoStub) Get(_ context.Context, id string) (*domain.Conversation, error) {
	if r.existing != nil && r.existing.ID == id {
		return r.existing, nil
	}
	return nil, domain.ErrNotFound
}
func (r *canonicalDMRepoStub) Update(_ context.Context, _ string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *canonicalDMRepoStub) SetTopic(_ context.Context, _ string, params domain.SetTopicParams) (*domain.Conversation, error) {
	r.lastTopic = params
	return r.existing, nil
}
func (r *canonicalDMRepoStub) SetPurpose(_ context.Context, _ string, params domain.SetPurposeParams) (*domain.Conversation, error) {
	r.lastPurpose = params
	return r.existing, nil
}
func (r *canonicalDMRepoStub) List(_ context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	r.lastList = params
	return &domain.CursorPage[domain.Conversation]{Items: []domain.Conversation{}}, nil
}
func (r *canonicalDMRepoStub) Archive(_ context.Context, _ string) error   { return nil }
func (r *canonicalDMRepoStub) Unarchive(_ context.Context, _ string) error { return nil }
func (r *canonicalDMRepoStub) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *canonicalDMRepoStub) AddMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (r *canonicalDMRepoStub) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *canonicalDMRepoStub) RemoveMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (r *canonicalDMRepoStub) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *canonicalDMRepoStub) ListMemberAccounts(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *canonicalDMRepoStub) IsMember(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}
func (r *canonicalDMRepoStub) IsAccountMember(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

func TestConversationService_Create_ReusesCanonicalDMWithoutCreateEvent(t *testing.T) {
	repo := &canonicalDMRepoStub{
		existing: &domain.Conversation{ID: "D123", WorkspaceID: "T123", Type: domain.ConversationTypeIM, NumMembers: 2},
	}
	recorder := &captureEventRecorder{}
	svc := NewConversationService(
		repo,
		&mockUserRepoMap{
			users: map[string]*domain.User{
				"U1": {ID: "U1", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
				"U2": {ID: "U2", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
			},
		},
		recorder,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U1", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
		conv, err := svc.Create(ctx, domain.CreateConversationParams{
			WorkspaceID: "T123",
			OwnerType:   domain.ConversationOwnerTypeWorkspace,
			Type:        domain.ConversationTypeIM,
			CreatorID:   "U1",
			UserIDs:     []string{"U2"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if conv.ID != "D123" {
		t.Fatalf("Create() id = %q, want %q", conv.ID, "D123")
	}
	if repo.createUsed {
		t.Fatal("expected canonical DM lookup to bypass Create()")
	}
	if recorder.event.EventType != "" {
		t.Fatalf("expected no create event, got %q", recorder.event.EventType)
	}
}

func TestConversationService_Create_UsesAccountOwnershipAndIncludesOwnerAccountAsMember(t *testing.T) {
	repo := newMockConversationRepoTenant()
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	conv, err := svc.Create(ctx, domain.CreateConversationParams{
		OwnerType: domain.ConversationOwnerTypeAccount,
		Name:      "account notes",
		Type:      domain.ConversationTypePublicChannel,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repo.lastCreate.WorkspaceID != "" {
		t.Fatalf("Create() workspace_id = %q, want empty", repo.lastCreate.WorkspaceID)
	}
	if repo.lastCreate.OwnerType != domain.ConversationOwnerTypeAccount {
		t.Fatalf("Create() owner_type = %q, want %q", repo.lastCreate.OwnerType, domain.ConversationOwnerTypeAccount)
	}
	if repo.lastCreate.OwnerAccountID != "A123" {
		t.Fatalf("Create() owner_account_id = %q, want %q", repo.lastCreate.OwnerAccountID, "A123")
	}
	if repo.lastCreate.OwnerWorkspaceID != "" {
		t.Fatalf("Create() owner_workspace_id = %q, want empty", repo.lastCreate.OwnerWorkspaceID)
	}
	if len(repo.lastCreate.AccountIDs) != 1 || repo.lastCreate.AccountIDs[0] != "A123" {
		t.Fatalf("Create() account_ids = %v, want [A123]", repo.lastCreate.AccountIDs)
	}
	if conv.OwnerType != domain.ConversationOwnerTypeAccount {
		t.Fatalf("returned conversation owner_type = %q, want %q", conv.OwnerType, domain.ConversationOwnerTypeAccount)
	}
	if conv.OwnerAccountID != "A123" {
		t.Fatalf("returned conversation owner_account_id = %q, want %q", conv.OwnerAccountID, "A123")
	}
}

func TestConversationService_Create_UsesWorkspaceOwnershipAndWorkspaceMembers(t *testing.T) {
	repo := newMockConversationRepoTenant()
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U1": {ID: "U1", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
			"U2": {ID: "U2", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}
	svc := NewConversationService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U1", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	conv, err := svc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: "T123",
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		CreatorID:   "U1",
		Name:        "general",
		Type:        domain.ConversationTypePublicChannel,
		UserIDs:     []string{"U2"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repo.lastCreate.WorkspaceID != "T123" {
		t.Fatalf("Create() workspace_id = %q, want %q", repo.lastCreate.WorkspaceID, "T123")
	}
	if repo.lastCreate.OwnerType != domain.ConversationOwnerTypeWorkspace {
		t.Fatalf("Create() owner_type = %q, want %q", repo.lastCreate.OwnerType, domain.ConversationOwnerTypeWorkspace)
	}
	if repo.lastCreate.OwnerWorkspaceID != "T123" {
		t.Fatalf("Create() owner_workspace_id = %q, want %q", repo.lastCreate.OwnerWorkspaceID, "T123")
	}
	if repo.lastCreate.OwnerAccountID != "" {
		t.Fatalf("Create() owner_account_id = %q, want empty", repo.lastCreate.OwnerAccountID)
	}
	if repo.lastCreate.CreatorID != "U1" {
		t.Fatalf("Create() creator_id = %q, want %q", repo.lastCreate.CreatorID, "U1")
	}
	if len(repo.lastCreate.UserIDs) != 1 || repo.lastCreate.UserIDs[0] != "U2" {
		t.Fatalf("Create() user_ids = %v, want [U2]", repo.lastCreate.UserIDs)
	}
	if conv.OwnerType != domain.ConversationOwnerTypeWorkspace {
		t.Fatalf("returned conversation owner_type = %q, want %q", conv.OwnerType, domain.ConversationOwnerTypeWorkspace)
	}
	if conv.OwnerWorkspaceID != "T123" {
		t.Fatalf("returned conversation owner_workspace_id = %q, want %q", conv.OwnerWorkspaceID, "T123")
	}
}

func TestConversationService_Invite_UsesAccountMembershipPathForWorkspaceUsers(t *testing.T) {
	repo := newMockConversationRepoTenant()
	repo.conversations["C123"] = &domain.Conversation{
		ID:               "C123",
		WorkspaceID:      "T123",
		OwnerType:        domain.ConversationOwnerTypeWorkspace,
		OwnerWorkspaceID: "T123",
		Type:             domain.ConversationTypePublicChannel,
	}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_MEMBER": {ID: "U_MEMBER", AccountID: "A_MEMBER", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman},
		},
	}
	svc := NewConversationService(repo, userRepo, &captureEventRecorder{}, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T123")
	ctx = ctxutil.WithPermissions(ctx, []string{"*"})

	if err := svc.Invite(ctx, "C123", "U_MEMBER"); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	if repo.lastAddMemberByAccount.conversationID != "C123" || repo.lastAddMemberByAccount.accountID != "A_MEMBER" {
		t.Fatalf("AddMemberByAccount() = (%q, %q), want (%q, %q)", repo.lastAddMemberByAccount.conversationID, repo.lastAddMemberByAccount.accountID, "C123", "A_MEMBER")
	}
	if repo.lastAddMember.userID != "" {
		t.Fatalf("AddMember() user_id = %q, want empty", repo.lastAddMember.userID)
	}
}

func TestConversationService_List_UsesActingUserID(t *testing.T) {
	repo := &canonicalDMRepoStub{}
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U_KEY", "T123")
	ctx = ctxutil.WithDelegation(ctx, "U_ACTOR", "")
	if _, err := svc.List(ctx, domain.ListConversationsParams{WorkspaceID: "T123"}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if repo.lastList.UserID != "U_ACTOR" {
		t.Fatalf("List() user_id = %q, want %q", repo.lastList.UserID, "U_ACTOR")
	}
}

type accountOwnedListRepoStub struct {
	lastList domain.ListConversationsParams
	items    []domain.Conversation
}

func (r *accountOwnedListRepoStub) WithTx(_ pgx.Tx) repository.ConversationRepository { return r }
func (r *accountOwnedListRepoStub) Create(_ context.Context, _ domain.CreateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *accountOwnedListRepoStub) GetCanonicalDM(_ context.Context, _, _, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrNotFound
}
func (r *accountOwnedListRepoStub) Get(_ context.Context, id string) (*domain.Conversation, error) {
	for _, item := range r.items {
		if item.ID == id {
			copy := item
			return &copy, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r *accountOwnedListRepoStub) Update(_ context.Context, _ string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *accountOwnedListRepoStub) SetTopic(_ context.Context, _ string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *accountOwnedListRepoStub) SetPurpose(_ context.Context, _ string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *accountOwnedListRepoStub) List(_ context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	r.lastList = params
	items := make([]domain.Conversation, 0, len(r.items))
	for _, item := range r.items {
		if item.OwnerType == domain.ConversationOwnerTypeAccount {
			if params.AccountID != "" && item.OwnerAccountID == params.AccountID {
				items = append(items, item)
			}
			continue
		}
		if params.WorkspaceID != "" && item.WorkspaceID == params.WorkspaceID {
			items = append(items, item)
		}
	}
	return &domain.CursorPage[domain.Conversation]{Items: items}, nil
}
func (r *accountOwnedListRepoStub) Archive(_ context.Context, _ string) error   { return nil }
func (r *accountOwnedListRepoStub) Unarchive(_ context.Context, _ string) error { return nil }
func (r *accountOwnedListRepoStub) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *accountOwnedListRepoStub) AddMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (r *accountOwnedListRepoStub) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *accountOwnedListRepoStub) RemoveMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (r *accountOwnedListRepoStub) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *accountOwnedListRepoStub) ListMemberAccounts(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *accountOwnedListRepoStub) IsMember(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (r *accountOwnedListRepoStub) IsAccountMember(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func TestConversationService_List_AllowsAccountOwnedWithoutWorkspaceContext(t *testing.T) {
	repo := &accountOwnedListRepoStub{
		items: []domain.Conversation{
			{ID: "C_OWNER", OwnerType: domain.ConversationOwnerTypeAccount, OwnerAccountID: "A123"},
			{ID: "C_OTHER", OwnerType: domain.ConversationOwnerTypeAccount, OwnerAccountID: "A999"},
			{ID: "C_WORKSPACE", OwnerType: domain.ConversationOwnerTypeWorkspace, WorkspaceID: "T123"},
		},
	}
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	page, err := svc.List(ctx, domain.ListConversationsParams{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if repo.lastList.WorkspaceID != "" {
		t.Fatalf("List() workspace_id = %q, want empty", repo.lastList.WorkspaceID)
	}
	if repo.lastList.AccountID != "A123" {
		t.Fatalf("List() account_id = %q, want %q", repo.lastList.AccountID, "A123")
	}
	if len(page.Items) != 1 || page.Items[0].ID != "C_OWNER" {
		t.Fatalf("List() items = %#v, want only owner conversation", page.Items)
	}
}

func TestConversationService_SetTopicAndPurpose_UseAccountIDForAccountOwnedConversation(t *testing.T) {
	repo := &canonicalDMRepoStub{
		existing: &domain.Conversation{
			ID:             "C_ACCOUNT",
			OwnerType:      domain.ConversationOwnerTypeAccount,
			OwnerAccountID: "A123",
		},
	}
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	if _, err := svc.SetTopic(ctx, "C_ACCOUNT", domain.SetTopicParams{Topic: "topic"}); err != nil {
		t.Fatalf("SetTopic() error = %v", err)
	}
	if repo.lastTopic.SetByID != "A123" {
		t.Fatalf("SetTopic() set_by_id = %q, want %q", repo.lastTopic.SetByID, "A123")
	}

	if _, err := svc.SetPurpose(ctx, "C_ACCOUNT", domain.SetPurposeParams{Purpose: "purpose"}); err != nil {
		t.Fatalf("SetPurpose() error = %v", err)
	}
	if repo.lastPurpose.SetByID != "A123" {
		t.Fatalf("SetPurpose() set_by_id = %q, want %q", repo.lastPurpose.SetByID, "A123")
	}
}

func TestConversationService_CreateRejectsUserOutsideWorkspace(t *testing.T) {
	repo := newMockConversationRepoTenant()
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U1": {ID: "U1", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
			"U2": {ID: "U2", WorkspaceID: "T999", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}
	svc := NewConversationService(repo, userRepo, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U1", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	_, err := svc.Create(ctx, domain.CreateConversationParams{
		WorkspaceID: "T123",
		OwnerType:   domain.ConversationOwnerTypeWorkspace,
		Type:        domain.ConversationTypePrivateChannel,
		CreatorID:   "U1",
		Name:        "shared",
		UserIDs:     []string{"U2"},
	})
	if err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for cross-workspace user, got %v", err)
	}
}

func TestConversationService_Get_AllowsAccountOwnedConversationMember(t *testing.T) {
	repo := newMockConversationRepoTenant()
	repo.conversations["C123"] = &domain.Conversation{
		ID:             "C123",
		OwnerType:      domain.ConversationOwnerTypeAccount,
		OwnerAccountID: "A999",
		Name:           "shared",
		Type:           domain.ConversationTypePrivateChannel,
	}
	repo.accountMembers = map[string]map[string]struct{}{
		"C123": {"A123": {}},
	}
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	conv, err := svc.Get(ctx, "C123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if conv.ID != "C123" {
		t.Fatalf("conversation id = %q, want C123", conv.ID)
	}
}

func TestConversationService_List_AllowsExternalMemberHostWorkspace(t *testing.T) {
	repo := newMockConversationRepoTenant()
	repo.conversations["G123"] = &domain.Conversation{
		ID:          "G123",
		WorkspaceID: "T123",
		Name:        "shared",
		Type:        domain.ConversationTypePrivateChannel,
	}
	repo.conversations["G124"] = &domain.Conversation{
		ID:          "G124",
		WorkspaceID: "T123",
		Name:        "shared-2",
		Type:        domain.ConversationTypePrivateChannel,
	}
	repo.accountMembers = map[string]map[string]struct{}{
		"G123": {"A123": {}},
	}
	svc := NewConversationService(repo, &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_GUEST": {ID: "U_GUEST", AccountID: "A123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}, nil, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"G123|A123": {
				ID:                  "EM123",
				ConversationID:      "G123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
			},
		},
	})

	ctx := ctxutil.WithUser(context.Background(), "U_EXT", "T999")
	ctx = ctxutil.WithIdentity(ctx, "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	page, err := svc.List(ctx, domain.ListConversationsParams{WorkspaceID: "T123", Limit: 100})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	if page.Items[0].ID != "G123" {
		t.Fatalf("conversation id = %q, want G123", page.Items[0].ID)
	}
}

func TestConversationService_ListMembers_RejectsExternalMember(t *testing.T) {
	repo := newMockConversationRepoTenant()
	repo.conversations["G123"] = &domain.Conversation{
		ID:          "G123",
		WorkspaceID: "T123",
		Name:        "shared",
		Type:        domain.ConversationTypePrivateChannel,
	}
	svc := NewConversationService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"G123|A123": {
				ID:                  "EM123",
				ConversationID:      "G123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionMessagesRead},
			},
		},
	})

	ctx := ctxutil.WithUser(context.Background(), "U_EXT", "T999")
	ctx = ctxutil.WithIdentity(ctx, "A123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	if _, err := svc.ListMembers(ctx, "G123", "", 10); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for external member listing members, got %v", err)
	}
}
