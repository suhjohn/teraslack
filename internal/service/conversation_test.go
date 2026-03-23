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

type mockConversationRepoTenant struct {
	conversations map[string]*domain.Conversation
}

func newMockConversationRepoTenant() *mockConversationRepoTenant {
	return &mockConversationRepoTenant{conversations: make(map[string]*domain.Conversation)}
}

func (m *mockConversationRepoTenant) Create(_ context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	conv := &domain.Conversation{ID: "C123", TeamID: params.TeamID, CreatorID: params.CreatorID, Name: params.Name, Type: params.Type}
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
		if params.TeamID == "" || conv.TeamID == params.TeamID {
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
	repo.conversations["C123"] = &domain.Conversation{ID: "C123", TeamID: "T999", Name: "general"}
	svc := NewConversationService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyTeamID, "T123")

	if _, err := svc.Get(ctx, "C123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Get, got %v", err)
	}
	if _, err := svc.ListMembers(ctx, "C123", "", 10); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from ListMembers, got %v", err)
	}
	if _, err := svc.Create(ctx, domain.CreateConversationParams{TeamID: "T999", CreatorID: "U123", Name: "secret"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Create, got %v", err)
	}
}
