package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func TestMessageService_UpdateMessage_UsesConversationWorkspaceID(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T999"}
	updated := &domain.Message{
		TS:        "123.456",
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "updated text",
	}

	recorder := &captureEventRecorder{}
	svc := NewMessageService(
		&messageRepoStub{existing: updated, updated: updated},
		&conversationRepoStub{conversation: conv},
		recorder,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	got, err := svc.UpdateMessage(context.Background(), "C123", "123.456", domain.UpdateMessageParams{
		Text: ptrString("updated text"),
	})
	if err != nil {
		t.Fatalf("UpdateMessage() error = %v", err)
	}
	if got.Text != "updated text" {
		t.Fatalf("UpdateMessage() text = %q, want %q", got.Text, "updated text")
	}
	if recorder.event.WorkspaceID != "T999" {
		t.Fatalf("recorded workspace_id = %q, want %q", recorder.event.WorkspaceID, "T999")
	}

	var payload domain.Message
	if err := json.Unmarshal(recorder.event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Text != "updated text" || payload.ChannelID != "C123" || payload.TS != "123.456" {
		t.Fatalf("unexpected recorded payload: %+v", payload)
	}
}

func TestMessageService_DeleteMessage_UsesConversationWorkspaceIDAndSnapshot(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T999"}
	existing := &domain.Message{
		TS:        "123.456",
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "delete me",
	}

	msgRepo := &messageRepoStub{existing: existing}
	recorder := &captureEventRecorder{}
	svc := NewMessageService(
		msgRepo,
		&conversationRepoStub{conversation: conv},
		recorder,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	if err := svc.DeleteMessage(context.Background(), "C123", "123.456"); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}
	if !msgRepo.deleted {
		t.Fatal("expected DeleteMessage() to delete the message")
	}
	if recorder.event.WorkspaceID != "T999" {
		t.Fatalf("recorded workspace_id = %q, want %q", recorder.event.WorkspaceID, "T999")
	}

	var payload domain.Message
	if err := json.Unmarshal(recorder.event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Text != "delete me" || payload.ChannelID != "C123" || payload.TS != "123.456" {
		t.Fatalf("unexpected recorded payload: %+v", payload)
	}
}

func TestMessageService_AddReaction_UsesConversationWorkspaceID(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T999"}
	existing := &domain.Message{TS: "123.456", ChannelID: "C123", UserID: "U123", Text: "hello"}

	recorder := &captureEventRecorder{}
	svc := NewMessageService(
		&messageRepoStub{existing: existing},
		&conversationRepoStub{conversation: conv},
		recorder,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	if err := svc.AddReaction(context.Background(), domain.AddReactionParams{
		ChannelID: "C123",
		MessageTS: "123.456",
		UserID:    "U123",
		Emoji:     "thumbsup",
	}); err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
	if recorder.event.WorkspaceID != "T999" {
		t.Fatalf("recorded workspace_id = %q, want %q", recorder.event.WorkspaceID, "T999")
	}
}

func TestMessageService_RemoveReaction_UsesConversationWorkspaceID(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T999"}

	recorder := &captureEventRecorder{}
	svc := NewMessageService(
		&messageRepoStub{},
		&conversationRepoStub{conversation: conv},
		recorder,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	if err := svc.RemoveReaction(context.Background(), domain.RemoveReactionParams{
		ChannelID: "C123",
		MessageTS: "123.456",
		UserID:    "U123",
		Emoji:     "thumbsup",
	}); err != nil {
		t.Fatalf("RemoveReaction() error = %v", err)
	}
	if recorder.event.WorkspaceID != "T999" {
		t.Fatalf("recorded workspace_id = %q, want %q", recorder.event.WorkspaceID, "T999")
	}
}

func TestMessageService_History_DeniesPrivateConversationNonMember(t *testing.T) {
	svc := NewMessageService(
		&messageRepoStub{},
		&conversationRepoStub{
			conversation: &domain.Conversation{
				ID:     "G123",
				WorkspaceID: "T123",
				Type:   domain.ConversationTypePrivateChannel,
			},
			isMember: false,
		},
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	if _, err := svc.History(ctx, domain.ListMessagesParams{ChannelID: "G123"}); err != domain.ErrForbidden {
		t.Fatalf("History() error = %v, want forbidden", err)
	}
}

func TestMessageService_History_AllowsPublicConversationNonMember(t *testing.T) {
	repo := &messageRepoStub{
		historyPage: &domain.CursorPage[domain.Message]{
			Items: []domain.Message{{TS: "123.456", ChannelID: "C123", UserID: "U123", Text: "hello"}},
		},
	}
	svc := NewMessageService(
		repo,
		&conversationRepoStub{
			conversation: &domain.Conversation{
				ID:     "C123",
				WorkspaceID: "T123",
				Type:   domain.ConversationTypePublicChannel,
			},
			isMember: false,
		},
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	page, err := svc.History(ctx, domain.ListMessagesParams{ChannelID: "C123"})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("History() items = %d, want 1", len(page.Items))
	}
}

func TestMessageService_PostMessage_DeniesPrivateConversationNonMember(t *testing.T) {
	svc := NewMessageService(
		&messageRepoStub{
			created: &domain.Message{TS: "123.456", ChannelID: "G123", UserID: "U123", Text: "hello"},
		},
		&conversationRepoStub{
			conversation: &domain.Conversation{
				ID:     "G123",
				WorkspaceID: "T123",
				Type:   domain.ConversationTypePrivateChannel,
			},
			isMember: false,
		},
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	if _, err := svc.PostMessage(ctx, domain.PostMessageParams{ChannelID: "G123", Text: "hello"}); err != domain.ErrForbidden {
		t.Fatalf("PostMessage() error = %v, want forbidden", err)
	}
}

func TestMessageService_PostMessage_UsesRowOnlyParentLookup(t *testing.T) {
	msgRepo := &messageRepoStub{
		existing: &domain.Message{TS: "123.456", ChannelID: "C123", UserID: "U123", Text: "root"},
		created:  &domain.Message{TS: "123.457", ChannelID: "C123", UserID: "U123", Text: "reply"},
	}
	svc := NewMessageService(
		msgRepo,
		&conversationRepoStub{
			conversation: &domain.Conversation{
				ID:     "C123",
				WorkspaceID: "T123",
				Type:   domain.ConversationTypePublicChannel,
			},
			isMember: true,
		},
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	if _, err := svc.PostMessage(ctx, domain.PostMessageParams{
		ChannelID: "C123",
		Text:      "reply",
		ThreadTS:  "123.456",
	}); err != nil {
		t.Fatalf("PostMessage() error = %v", err)
	}
	if msgRepo.getRowCalls != 1 {
		t.Fatalf("GetRow() calls = %d, want 1", msgRepo.getRowCalls)
	}
	if msgRepo.getCalls != 0 {
		t.Fatalf("Get() calls = %d, want 0", msgRepo.getCalls)
	}
}

func TestMessageService_UpdateMessage_RequiresAuthor(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T123"}
	existing := &domain.Message{TS: "123.456", ChannelID: "C123", UserID: "U_AUTHOR", Text: "original"}
	svc := NewMessageService(
		&messageRepoStub{existing: existing, updated: existing},
		&conversationRepoStub{conversation: conv},
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U_OTHER", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	if _, err := svc.UpdateMessage(ctx, "C123", "123.456", domain.UpdateMessageParams{Text: ptrString("edited")}); err == nil || err != domain.ErrForbidden {
		t.Fatalf("expected forbidden for non-author update, got %v", err)
	}
}

func TestMessageService_DeleteMessage_AllowsWorkspaceAdmin(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T123"}
	existing := &domain.Message{TS: "123.456", ChannelID: "C123", UserID: "U_AUTHOR", Text: "delete me"}
	msgRepo := &messageRepoStub{existing: existing}
	svc := NewMessageService(
		msgRepo,
		&conversationRepoStub{conversation: conv},
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)
	if err := svc.DeleteMessage(ctx, "C123", "123.456"); err != nil {
		t.Fatalf("admin delete should succeed: %v", err)
	}
	if !msgRepo.deleted {
		t.Fatal("expected message to be deleted by admin")
	}
}

func ptrString(v string) *string {
	return &v
}

type captureEventRecorder struct {
	event domain.InternalEvent
}

func (r *captureEventRecorder) Record(_ context.Context, event domain.InternalEvent) error {
	r.event = event
	return nil
}

func (r *captureEventRecorder) WithTx(tx pgx.Tx) EventRecorder {
	return r
}

type messageRepoStub struct {
	created           *domain.Message
	existing          *domain.Message
	updated           *domain.Message
	deleted           bool
	getCalls          int
	getRowCalls       int
	historyPage       *domain.CursorPage[domain.Message]
	repliesPage       *domain.CursorPage[domain.Message]
	reactions         []domain.Reaction
	addReactionErr    error
	removeReactionErr error
}

func (r *messageRepoStub) WithTx(tx pgx.Tx) repository.MessageRepository { return r }

func (r *messageRepoStub) Create(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error) {
	if r.created == nil {
		return nil, domain.ErrNotFound
	}
	return r.created, nil
}

func (r *messageRepoStub) Get(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	r.getCalls++
	if r.existing == nil {
		return nil, domain.ErrNotFound
	}
	return r.existing, nil
}

func (r *messageRepoStub) GetRow(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	r.getRowCalls++
	if r.existing == nil {
		return nil, domain.ErrNotFound
	}
	return r.existing, nil
}

func (r *messageRepoStub) Update(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error) {
	if r.updated == nil {
		return nil, domain.ErrNotFound
	}
	return r.updated, nil
}

func (r *messageRepoStub) Delete(ctx context.Context, channelID, ts string) error {
	r.deleted = true
	return nil
}

func (r *messageRepoStub) ListHistory(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error) {
	if r.historyPage == nil {
		return &domain.CursorPage[domain.Message]{Items: []domain.Message{}}, nil
	}
	return r.historyPage, nil
}

func (r *messageRepoStub) ListReplies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error) {
	if r.repliesPage == nil {
		return &domain.CursorPage[domain.Message]{Items: []domain.Message{}}, nil
	}
	return r.repliesPage, nil
}

func (r *messageRepoStub) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	return r.addReactionErr
}

func (r *messageRepoStub) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	return r.removeReactionErr
}

func (r *messageRepoStub) GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error) {
	return r.reactions, nil
}

type conversationRepoStub struct {
	conversation *domain.Conversation
	isMember     bool
}

func (r *conversationRepoStub) WithTx(tx pgx.Tx) repository.ConversationRepository { return r }

func (r *conversationRepoStub) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}

func (r *conversationRepoStub) Get(ctx context.Context, id string) (*domain.Conversation, error) {
	if r.conversation == nil {
		return nil, domain.ErrNotFound
	}
	return r.conversation, nil
}

func (r *conversationRepoStub) GetCanonicalDM(ctx context.Context, workspaceID, userAID, userBID string) (*domain.Conversation, error) {
	if r.conversation == nil {
		return nil, domain.ErrNotFound
	}
	return r.conversation, nil
}

func (r *conversationRepoStub) Update(ctx context.Context, id string, params domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}

func (r *conversationRepoStub) SetTopic(ctx context.Context, id string, params domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}

func (r *conversationRepoStub) SetPurpose(ctx context.Context, id string, params domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
}

func (r *conversationRepoStub) List(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	return nil, nil
}

func (r *conversationRepoStub) Archive(ctx context.Context, id string) error { return nil }

func (r *conversationRepoStub) Unarchive(ctx context.Context, id string) error { return nil }

func (r *conversationRepoStub) AddMember(ctx context.Context, conversationID, userID string) error {
	return nil
}

func (r *conversationRepoStub) RemoveMember(ctx context.Context, conversationID, userID string) error {
	return nil
}

func (r *conversationRepoStub) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	return nil, nil
}

func (r *conversationRepoStub) IsMember(ctx context.Context, conversationID, userID string) (bool, error) {
	return r.isMember, nil
}
