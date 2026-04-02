package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type externalEventRepoStub struct {
	page          *domain.CursorPage[domain.ExternalEvent]
	lastPrincipal repository.ExternalEventPrincipal
	lastParams    domain.ListExternalEventsParams
}

type externalEventExternalMemberRepoStub struct {
	itemsByWorkspace map[string][]domain.ExternalMember
}

func (r *externalEventRepoStub) WithTx(tx pgx.Tx) repository.ExternalEventRepository { return r }
func (r *externalEventRepoStub) Insert(ctx context.Context, event domain.ExternalEvent) (*domain.ExternalEvent, error) {
	return &event, nil
}
func (r *externalEventRepoStub) RecordProjectionFailure(ctx context.Context, internalEventID int64, message string) error {
	return nil
}
func (r *externalEventRepoStub) ListVisible(ctx context.Context, principal repository.ExternalEventPrincipal, params domain.ListExternalEventsParams) (*domain.CursorPage[domain.ExternalEvent], error) {
	r.lastPrincipal = principal
	r.lastParams = params
	if r.page == nil {
		return &domain.CursorPage[domain.ExternalEvent]{Items: []domain.ExternalEvent{}}, nil
	}
	return r.page, nil
}
func (r *externalEventRepoStub) GetSince(ctx context.Context, afterID int64, limit int) ([]domain.ExternalEvent, error) {
	return nil, nil
}
func (r *externalEventRepoStub) Rebuild(ctx context.Context, events []domain.ExternalEvent) error {
	return nil
}
func (r *externalEventRepoStub) RebuildFeeds(ctx context.Context) error { return nil }

func (r *externalEventExternalMemberRepoStub) WithTx(tx pgx.Tx) repository.ExternalMemberRepository {
	return r
}
func (r *externalEventExternalMemberRepoStub) Create(ctx context.Context, params domain.CreateExternalMemberParams, hostWorkspaceID string) (*domain.ExternalMember, error) {
	return nil, nil
}
func (r *externalEventExternalMemberRepoStub) Get(ctx context.Context, id string) (*domain.ExternalMember, error) {
	return nil, nil
}
func (r *externalEventExternalMemberRepoStub) GetActiveByConversationAndAccount(ctx context.Context, conversationID, accountID string) (*domain.ExternalMember, error) {
	return nil, nil
}
func (r *externalEventExternalMemberRepoStub) ListActiveByAccountAndWorkspace(ctx context.Context, accountID, workspaceID string) ([]domain.ExternalMember, error) {
	if r.itemsByWorkspace == nil {
		return nil, nil
	}
	return append([]domain.ExternalMember(nil), r.itemsByWorkspace[workspaceID]...), nil
}
func (r *externalEventExternalMemberRepoStub) ListByConversation(ctx context.Context, conversationID string) ([]domain.ExternalMember, error) {
	return nil, nil
}
func (r *externalEventExternalMemberRepoStub) Update(ctx context.Context, id string, params domain.UpdateExternalMemberParams) (*domain.ExternalMember, error) {
	return nil, nil
}
func (r *externalEventExternalMemberRepoStub) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	return nil
}
func (r *externalEventExternalMemberRepoStub) RevokeByExternalWorkspace(ctx context.Context, hostWorkspaceID, externalWorkspaceID string, revokedAt time.Time) error {
	return nil
}

func TestExternalEventService_ListRejectsCursorPrincipalMismatch(t *testing.T) {
	svc := NewExternalEventService(&externalEventRepoStub{})

	cursor, err := encodeExternalEventCursor(externalEventCursor{
		AfterID:                 41,
		WorkspaceID:             "T123",
		UserID:                  "U123",
		AccountID:               "A123",
		HasWorkspaceUserContext: true,
		Type:                    domain.EventTypeConversationMessageCreated,
		ResourceType:            domain.ResourceTypeConversation,
		ResourceID:              "C123",
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U999", "T123"), "A123")
	if _, err := svc.List(ctx, domain.ListExternalEventsParams{
		Cursor:       cursor,
		Type:         domain.EventTypeConversationMessageCreated,
		ResourceType: domain.ResourceTypeConversation,
		ResourceID:   "C123",
	}); err == nil || !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for mismatched principal, got %v", err)
	}
}

func TestProjectExternalEvents_SubscriptionPayloadRedactsSecrets(t *testing.T) {
	raw, err := json.Marshal(domain.EventSubscription{
		ID:              "ES123",
		WorkspaceID:     "T123",
		URL:             "https://example.com/webhook",
		Type:            domain.EventTypeConversationMessageCreated,
		ResourceType:    domain.ResourceTypeConversation,
		ResourceID:      "C123",
		Secret:          "plaintext-secret",
		EncryptedSecret: "ciphertext-secret",
		Enabled:         true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	events, err := projectExternalEvents(domain.InternalEvent{
		ID:            9,
		EventType:     domain.EventSubscriptionCreated,
		AggregateType: domain.AggregateSubscription,
		AggregateID:   "ES123",
		WorkspaceID:   "T123",
		Payload:       raw,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("projectExternalEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != domain.EventTypeEventSubscriptionCreated {
		t.Fatalf("unexpected external event type %q", events[0].Type)
	}
	if events[0].ResourceType != domain.ResourceTypeWorkspace || events[0].ResourceID != "T123" {
		t.Fatalf("unexpected canonical resource %s/%s", events[0].ResourceType, events[0].ResourceID)
	}

	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("decode projected payload: %v", err)
	}
	if _, ok := payload["secret"]; ok {
		t.Fatal("projected payload leaked plaintext secret")
	}
	if _, ok := payload["encrypted_secret"]; ok {
		t.Fatal("projected payload leaked encrypted secret")
	}
	if payload["id"] != "ES123" || payload["type"] != domain.EventTypeConversationMessageCreated {
		t.Fatalf("unexpected projected payload: %+v", payload)
	}
}

func TestProjectExternalEvents_MessageDeleteUsesTombstonePayload(t *testing.T) {
	raw, err := json.Marshal(domain.Message{
		TS:        "1712345678.000001",
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "to be deleted",
	})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	events, err := projectExternalEvents(domain.InternalEvent{
		ID:            11,
		EventType:     domain.EventMessageDeleted,
		AggregateType: domain.AggregateMessage,
		AggregateID:   "1712345678.000001",
		WorkspaceID:   "T123",
		Payload:       raw,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("projectExternalEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != domain.EventTypeConversationMessageDeleted {
		t.Fatalf("unexpected event type %q", events[0].Type)
	}
	if events[0].ResourceType != domain.ResourceTypeConversation || events[0].ResourceID != "C123" {
		t.Fatalf("unexpected canonical resource %s/%s", events[0].ResourceType, events[0].ResourceID)
	}

	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload) != 2 || payload["channel_id"] != "C123" || payload["ts"] != "1712345678.000001" {
		t.Fatalf("expected tombstone payload, got %+v", payload)
	}
}

func TestDecodeExternalEventCursor_RejectsLegacyIntegerCursor(t *testing.T) {
	_, err := decodeExternalEventCursor("41")
	if err == nil {
		t.Fatal("expected error for legacy integer cursor")
	}
}

func TestExternalEventService_ListReturnsResumeCursorOnTerminalPage(t *testing.T) {
	svc := NewExternalEventService(&externalEventRepoStub{
		page: &domain.CursorPage[domain.ExternalEvent]{
			Items: []domain.ExternalEvent{{
				ID:           41,
				WorkspaceID:  "T123",
				Type:         domain.EventTypeConversationMessageCreated,
				ResourceType: domain.ResourceTypeConversation,
				ResourceID:   "C123",
			}},
		},
	})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U123", "T123"), "A123")
	page, err := svc.List(ctx, domain.ListExternalEventsParams{
		Type:         domain.EventTypeConversationMessageCreated,
		ResourceType: domain.ResourceTypeConversation,
		ResourceID:   "C123",
	})
	if err != nil {
		t.Fatalf("list external events: %v", err)
	}
	if page.NextCursor == "" {
		t.Fatal("expected resume cursor on terminal page")
	}

	decoded, err := decodeExternalEventCursor(page.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	if decoded.AfterID != 41 || decoded.WorkspaceID != "T123" || decoded.UserID != "U123" || decoded.AccountID != "A123" || !decoded.HasWorkspaceUserContext {
		t.Fatalf("unexpected cursor state: %+v", decoded)
	}
}

func TestExternalEventService_ListPassesWorkspaceUserIdentityToRepository(t *testing.T) {
	repo := &externalEventRepoStub{}
	svc := NewExternalEventService(repo)

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U123", "T123"), "A123")
	if _, err := svc.List(ctx, domain.ListExternalEventsParams{ResourceType: domain.ResourceTypeConversation}); err != nil {
		t.Fatalf("list external events: %v", err)
	}
	if repo.lastPrincipal.AccountID != "A123" || !repo.lastPrincipal.HasWorkspaceUserContext {
		t.Fatalf("unexpected principal: %+v", repo.lastPrincipal)
	}
}

func TestExternalEventService_ListAllowsRequestedSharedWorkspace(t *testing.T) {
	repo := &externalEventRepoStub{}
	svc := NewExternalEventService(repo)
	svc.SetExternalMemberRepository(&externalEventExternalMemberRepoStub{
		itemsByWorkspace: map[string][]domain.ExternalMember{
			"T999": {{ConversationID: "C123", HostWorkspaceID: "T999", AccountID: "A123"}},
		},
	})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U123", "T123"), "A123")
	if _, err := svc.List(ctx, domain.ListExternalEventsParams{WorkspaceID: "T999"}); err != nil {
		t.Fatalf("list external events: %v", err)
	}
	if repo.lastPrincipal.WorkspaceID != "T999" {
		t.Fatalf("unexpected workspace id: %+v", repo.lastPrincipal)
	}
	if repo.lastPrincipal.HasWorkspaceUserContext {
		t.Fatalf("expected shared workspace request to clear workspace-user context, got %+v", repo.lastPrincipal)
	}
}

func TestExternalEventService_ListRejectsUnsharedRequestedWorkspace(t *testing.T) {
	repo := &externalEventRepoStub{}
	svc := NewExternalEventService(repo)
	svc.SetExternalMemberRepository(&externalEventExternalMemberRepoStub{})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U123", "T123"), "A123")
	if _, err := svc.List(ctx, domain.ListExternalEventsParams{WorkspaceID: "T999"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
