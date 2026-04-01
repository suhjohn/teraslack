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

type mockEventRepoTenant struct {
	subs map[string]*domain.EventSubscription
}

func newMockEventRepoTenant() *mockEventRepoTenant {
	return &mockEventRepoTenant{subs: make(map[string]*domain.EventSubscription)}
}

func (m *mockEventRepoTenant) CreateSubscription(_ context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	sub := &domain.EventSubscription{
		ID:           "ES123",
		WorkspaceID:       params.WorkspaceID,
		URL:          params.URL,
		Type:         params.Type,
		ResourceType: params.ResourceType,
		ResourceID:   params.ResourceID,
		Enabled:      true,
	}
	m.subs[sub.ID] = sub
	return sub, nil
}

func (m *mockEventRepoTenant) GetSubscription(_ context.Context, id string) (*domain.EventSubscription, error) {
	sub, ok := m.subs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return sub, nil
}

func (m *mockEventRepoTenant) UpdateSubscription(_ context.Context, id string, _ domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error) {
	return m.GetSubscription(context.Background(), id)
}

func (m *mockEventRepoTenant) DeleteSubscription(_ context.Context, id string) error { return nil }

func (m *mockEventRepoTenant) ListSubscriptions(_ context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	var out []domain.EventSubscription
	for _, sub := range m.subs {
		if params.WorkspaceID == "" || sub.WorkspaceID == params.WorkspaceID {
			out = append(out, *sub)
		}
	}
	return out, nil
}

func (m *mockEventRepoTenant) ListSubscriptionsByEvent(_ context.Context, workspaceID, eventType, resourceType, resourceID string) ([]domain.EventSubscription, error) {
	return m.ListSubscriptions(context.Background(), domain.ListEventSubscriptionsParams{WorkspaceID: workspaceID})
}

func (m *mockEventRepoTenant) WithTx(_ pgx.Tx) repository.EventRepository { return m }

func TestEventService_TenantAccessDenied(t *testing.T) {
	repo := newMockEventRepoTenant()
	repo.subs["ES123"] = &domain.EventSubscription{ID: "ES123", WorkspaceID: "T999", URL: "https://example.com", Type: domain.EventTypeConversationMessageCreated}
	svc := NewEventService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")

	if _, err := svc.GetSubscription(ctx, "ES123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from GetSubscription, got %v", err)
	}
	if _, err := svc.ListSubscriptions(ctx, domain.ListEventSubscriptionsParams{WorkspaceID: "T999"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from ListSubscriptions, got %v", err)
	}
	if _, err := svc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{WorkspaceID: "T999", URL: "https://example.com", Type: domain.EventTypeConversationMessageCreated}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from CreateSubscription, got %v", err)
	}
}

func TestEventService_CreateSubscription_RejectsLegacyEventType(t *testing.T) {
	repo := newMockEventRepoTenant()
	svc := NewEventService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	_, err := svc.CreateSubscription(ctx, domain.CreateEventSubscriptionParams{
		WorkspaceID: "T123",
		URL:    "https://example.com",
		Type:   "message.posted",
	})
	if err == nil || !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestEventService_UpdateSubscription_RejectsLegacyEventType(t *testing.T) {
	repo := newMockEventRepoTenant()
	repo.subs["ES123"] = &domain.EventSubscription{
		ID:     "ES123",
		WorkspaceID: "T123",
		URL:    "https://example.com",
		Type:   domain.EventTypeConversationMessageCreated,
	}
	svc := NewEventService(repo, &mockUserRepoDefault{}, nil, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	legacyType := "channel.created"
	_, err := svc.UpdateSubscription(ctx, "ES123", domain.UpdateEventSubscriptionParams{
		Type: &legacyType,
	})
	if err == nil || !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestEventService_SubscriptionMutationsRequireWorkspaceAdmin(t *testing.T) {
	repo := newMockEventRepoTenant()
	repo.subs["ES123"] = &domain.EventSubscription{
		ID:     "ES123",
		WorkspaceID: "T123",
		URL:    "https://example.com",
		Type:   domain.EventTypeConversationMessageCreated,
	}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U123": {ID: "U123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
			"U999": {ID: "U999", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeAdmin},
		},
	}
	svc := NewEventService(repo, userRepo, nil, mockTxBeginner{}, nil)

	memberCtx := ctxutil.WithUser(context.Background(), "U123", "T123")
	if _, err := svc.CreateSubscription(memberCtx, domain.CreateEventSubscriptionParams{
		WorkspaceID: "T123",
		URL:    "https://example.com",
		Type:   domain.EventTypeConversationMessageCreated,
	}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member create, got %v", err)
	}
	if _, err := svc.GetSubscription(memberCtx, "ES123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden for member get, got %v", err)
	}

	adminCtx := ctxutil.WithUser(context.Background(), "U999", "T123")
	if _, err := svc.CreateSubscription(adminCtx, domain.CreateEventSubscriptionParams{
		WorkspaceID: "T123",
		URL:    "https://example.com",
		Type:   domain.EventTypeConversationMessageCreated,
	}); err != nil {
		t.Fatalf("admin create should succeed: %v", err)
	}
}
