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
	conversations map[string]*domain.Conversation
}

func newMockConversationRepoTenant() *mockConversationRepoTenant {
	return &mockConversationRepoTenant{conversations: make(map[string]*domain.Conversation)}
}

func (m *mockConversationRepoTenant) Create(_ context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: params.WorkspaceID, CreatorID: params.CreatorID, Name: params.Name, Type: params.Type}
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
		if params.WorkspaceID == "" || conv.WorkspaceID == params.WorkspaceID {
			items = append(items, *conv)
		}
	}
	return &domain.CursorPage[domain.Conversation]{Items: items}, nil
}

func (m *mockConversationRepoTenant) Archive(_ context.Context, id string) error   { return nil }
func (m *mockConversationRepoTenant) Unarchive(_ context.Context, id string) error { return nil }
func (m *mockConversationRepoTenant) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConversationRepoTenant) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConversationRepoTenant) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (m *mockConversationRepoTenant) IsMember(_ context.Context, _, _ string) (bool, error) {
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
	if _, err := svc.Create(ctx, domain.CreateConversationParams{WorkspaceID: "T999", CreatorID: "U123", Name: "secret"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Create, got %v", err)
	}
}

type canonicalDMRepoStub struct {
	existing   *domain.Conversation
	createUsed bool
	lastList   domain.ListConversationsParams
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
func (r *canonicalDMRepoStub) SetTopic(_ context.Context, _ string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *canonicalDMRepoStub) SetPurpose(_ context.Context, _ string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
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
func (r *canonicalDMRepoStub) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *canonicalDMRepoStub) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *canonicalDMRepoStub) IsMember(_ context.Context, _, _ string) (bool, error) {
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
		Type:        domain.ConversationTypePrivateChannel,
		CreatorID:   "U1",
		Name:        "shared",
		UserIDs:     []string{"U2"},
	})
	if err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for cross-workspace user, got %v", err)
	}
}

func TestConversationService_Get_AllowsExternalMember(t *testing.T) {
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

	conv, err := svc.Get(ctx, "G123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if conv.ID != "G123" {
		t.Fatalf("conversation id = %q, want G123", conv.ID)
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
