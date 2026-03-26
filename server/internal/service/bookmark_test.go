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

type mockBookmarkRepo struct {
	bookmarks map[string]*domain.Bookmark
}

func newMockBookmarkRepo() *mockBookmarkRepo {
	return &mockBookmarkRepo{bookmarks: make(map[string]*domain.Bookmark)}
}

func (m *mockBookmarkRepo) Create(_ context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error) {
	b := &domain.Bookmark{
		ID:        "Bk123",
		ChannelID: params.ChannelID,
		Title:     params.Title,
		Type:      params.Type,
		Link:      params.Link,
		Emoji:     params.Emoji,
		CreatedBy: params.CreatedBy,
		UpdatedBy: params.CreatedBy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.bookmarks[b.ID] = b
	return b, nil
}

func (m *mockBookmarkRepo) Get(_ context.Context, id string) (*domain.Bookmark, error) {
	b, ok := m.bookmarks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return b, nil
}

func (m *mockBookmarkRepo) Update(_ context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error) {
	b, ok := m.bookmarks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if params.Title != nil {
		b.Title = *params.Title
	}
	if params.Link != nil {
		b.Link = *params.Link
	}
	return b, nil
}

func (m *mockBookmarkRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.bookmarks[id]; !ok {
		return domain.ErrNotFound
	}
	delete(m.bookmarks, id)
	return nil
}

func (m *mockBookmarkRepo) List(_ context.Context, params domain.ListBookmarksParams) ([]domain.Bookmark, error) {
	var result []domain.Bookmark
	for _, b := range m.bookmarks {
		if b.ChannelID == params.ChannelID {
			result = append(result, *b)
		}
	}
	if result == nil {
		result = []domain.Bookmark{}
	}
	return result, nil
}

func (m *mockBookmarkRepo) WithTx(_ pgx.Tx) repository.BookmarkRepository { return m }

// mockConvRepoForBookmark is a minimal conversation repo mock for bookmark tests.
type mockConvRepoForBookmark struct{}

func (m *mockConvRepoForBookmark) Create(_ context.Context, _ domain.CreateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmark) Get(_ context.Context, id string) (*domain.Conversation, error) {
	if id == "" {
		return nil, domain.ErrNotFound
	}
	return &domain.Conversation{ID: id}, nil
}
func (m *mockConvRepoForBookmark) GetCanonicalDM(_ context.Context, _, _, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrNotFound
}
func (m *mockConvRepoForBookmark) Update(_ context.Context, _ string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmark) SetTopic(_ context.Context, _ string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmark) SetPurpose(_ context.Context, _ string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmark) List(_ context.Context, _ domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	return nil, nil
}
func (m *mockConvRepoForBookmark) Archive(_ context.Context, _ string) error   { return nil }
func (m *mockConvRepoForBookmark) Unarchive(_ context.Context, _ string) error { return nil }
func (m *mockConvRepoForBookmark) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConvRepoForBookmark) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConvRepoForBookmark) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return nil, nil
}
func (m *mockConvRepoForBookmark) IsMember(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}
func (m *mockConvRepoForBookmark) WithTx(_ pgx.Tx) repository.ConversationRepository { return m }

type mockConvRepoForBookmarkTenant struct {
	workspaceID string
}

func (m *mockConvRepoForBookmarkTenant) Create(_ context.Context, _ domain.CreateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmarkTenant) Get(_ context.Context, id string) (*domain.Conversation, error) {
	if id == "" {
		return nil, domain.ErrNotFound
	}
	return &domain.Conversation{ID: id, WorkspaceID: m.workspaceID}, nil
}
func (m *mockConvRepoForBookmarkTenant) GetCanonicalDM(_ context.Context, _, _, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrNotFound
}
func (m *mockConvRepoForBookmarkTenant) Update(_ context.Context, _ string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmarkTenant) SetTopic(_ context.Context, _ string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmarkTenant) SetPurpose(_ context.Context, _ string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForBookmarkTenant) List(_ context.Context, _ domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	return nil, nil
}
func (m *mockConvRepoForBookmarkTenant) Archive(_ context.Context, _ string) error   { return nil }
func (m *mockConvRepoForBookmarkTenant) Unarchive(_ context.Context, _ string) error { return nil }
func (m *mockConvRepoForBookmarkTenant) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConvRepoForBookmarkTenant) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConvRepoForBookmarkTenant) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return nil, nil
}
func (m *mockConvRepoForBookmarkTenant) IsMember(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}
func (m *mockConvRepoForBookmarkTenant) WithTx(_ pgx.Tx) repository.ConversationRepository { return m }

func TestBookmarkService_Create(t *testing.T) {
	repo := newMockBookmarkRepo()
	svc := NewBookmarkService(repo, &mockConvRepoForBookmark{}, nil, mockTxBeginner{}, nil)

	tests := []struct {
		name    string
		params  domain.CreateBookmarkParams
		wantErr bool
	}{
		{
			name: "valid create",
			params: domain.CreateBookmarkParams{
				ChannelID: "C123",
				Title:     "Go Docs",
				Link:      "https://go.dev",
				CreatedBy: "U123",
			},
			wantErr: false,
		},
		{
			name: "missing channel_id",
			params: domain.CreateBookmarkParams{
				Title:     "Go Docs",
				Link:      "https://go.dev",
				CreatedBy: "U123",
			},
			wantErr: true,
		},
		{
			name: "missing title",
			params: domain.CreateBookmarkParams{
				ChannelID: "C123",
				Link:      "https://go.dev",
				CreatedBy: "U123",
			},
			wantErr: true,
		},
		{
			name: "missing link",
			params: domain.CreateBookmarkParams{
				ChannelID: "C123",
				Title:     "Go Docs",
				CreatedBy: "U123",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := svc.Create(context.Background(), tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b.Title != tt.params.Title {
				t.Errorf("got title %q, want %q", b.Title, tt.params.Title)
			}
		})
	}
}

func TestBookmarkService_CRUD(t *testing.T) {
	repo := newMockBookmarkRepo()
	svc := NewBookmarkService(repo, &mockConvRepoForBookmark{}, nil, mockTxBeginner{}, nil)

	// Create
	b, err := svc.Create(context.Background(), domain.CreateBookmarkParams{
		ChannelID: "C123",
		Title:     "Go Docs",
		Link:      "https://go.dev",
		CreatedBy: "U123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update
	newTitle := "Updated Title"
	updated, err := svc.Update(context.Background(), b.ID, domain.UpdateBookmarkParams{
		Title:     &newTitle,
		UpdatedBy: "U456",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("got title %q, want %q", updated.Title, newTitle)
	}

	// List
	bookmarks, err := svc.List(context.Background(), "C123")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bookmarks) != 1 {
		t.Errorf("got %d bookmarks, want 1", len(bookmarks))
	}

	// Delete
	if err := svc.Delete(context.Background(), b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify deleted
	bookmarks, err = svc.List(context.Background(), "C123")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(bookmarks) != 0 {
		t.Errorf("got %d bookmarks after delete, want 0", len(bookmarks))
	}
}

func TestBookmarkService_TenantAccessDenied(t *testing.T) {
	repo := newMockBookmarkRepo()
	repo.bookmarks["Bk123"] = &domain.Bookmark{ID: "Bk123", ChannelID: "C123", Title: "Secret", Link: "https://example.com"}
	svc := NewBookmarkService(repo, &mockConvRepoForBookmarkTenant{workspaceID: "T999"}, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")

	if _, err := svc.Create(ctx, domain.CreateBookmarkParams{ChannelID: "C123", Title: "Doc", Link: "https://go.dev", CreatedBy: "U123"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Create, got %v", err)
	}
	if _, err := svc.Update(ctx, "Bk123", domain.UpdateBookmarkParams{UpdatedBy: "U123"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Update, got %v", err)
	}
	if err := svc.Delete(ctx, "Bk123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Delete, got %v", err)
	}
	if _, err := svc.List(ctx, "C123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from List, got %v", err)
	}
}
