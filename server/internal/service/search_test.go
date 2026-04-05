package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func TestSearchService_Search_EmptyQuery(t *testing.T) {
	svc := NewSearchService(nil)

	_, err := svc.Search(context.Background(), domain.SearchParams{
		Query: "",
	})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchService_Search_NoTurbopuffer(t *testing.T) {
	svc := NewSearchService(nil)

	results, err := svc.Search(context.Background(), domain.SearchParams{
		WorkspaceID: "T123",
		Query:       "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchService_Search_WithTypes(t *testing.T) {
	svc := NewSearchService(nil)

	// With type filter, still returns empty when no Turbopuffer configured
	results, err := svc.Search(context.Background(), domain.SearchParams{
		WorkspaceID: "T123",
		Query:       "alice",
		Types:       []string{"user", "conversation"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchService_Search_UsesContextWorkspaceID(t *testing.T) {
	svc := NewSearchService(nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "Tctx")
	results, err := svc.Search(ctx, domain.SearchParams{
		Query: "alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchService_Search_RejectsCrossTeamBodyOverride(t *testing.T) {
	svc := NewSearchService(nil)

	ctx := ctxutil.WithUser(context.Background(), "U123", "Tctx")
	_, err := svc.Search(ctx, domain.SearchParams{
		WorkspaceID: "Tother",
		Query:       "alice",
	})
	if err == nil {
		t.Fatal("expected error for mismatched workspace_id")
	}
	if err != domain.ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestSearchService_Index_NoTurbopuffer(t *testing.T) {
	svc := NewSearchService(nil)

	// Index should be a no-op when Turbopuffer is not configured
	err := svc.Index(context.Background(), "user", "U123", "T123", "alice", map[string]string{"name": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchService_Index_EmptyContent(t *testing.T) {
	svc := NewSearchService(nil)

	// Empty content should be a no-op
	err := svc.Index(context.Background(), "user", "U123", "T123", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchService_Search_NormalizesData(t *testing.T) {
	svc := NewSearchService(&searchClientStub{
		resultsByNamespace: map[string][]VectorResult{
			"00": {
				{
					ID:    "user:U123",
					Score: 0.99,
					Metadata: map[string]any{
						"type":         "user",
						"workspace_id": "T123",
						"data":         `{"id":"U123","name":"alice"}`,
					},
				},
				{
					ID:    "message:C123:123.456",
					Score: 0.98,
					Metadata: map[string]any{
						"type":         "message",
						"workspace_id": "T123",
						"data":         json.RawMessage(`{"channel_id":"C123","ts":"123.456"}`),
					},
				},
			},
		},
	})

	results, err := svc.Search(context.Background(), domain.SearchParams{
		WorkspaceID: "T123",
		Query:       "alice",
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if got := string(results[0].Data); got != `{"id":"U123","name":"alice"}` {
		t.Fatalf("first result data = %s", got)
	}
	if got := string(results[1].Data); got != `{"channel_id":"C123","ts":"123.456"}` {
		t.Fatalf("second result data = %s", got)
	}
}

type searchClientStub struct {
	resultsByNamespace map[string][]VectorResult
}

type searchConversationRepoStub struct {
	pages map[string][]domain.Conversation
}

func (r *searchConversationRepoStub) WithTx(_ pgx.Tx) repository.ConversationRepository { return r }
func (r *searchConversationRepoStub) Create(_ context.Context, _ domain.CreateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *searchConversationRepoStub) GetCanonicalDM(_ context.Context, _, _, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrNotFound
}
func (r *searchConversationRepoStub) Get(_ context.Context, _ string) (*domain.Conversation, error) {
	return nil, domain.ErrNotFound
}
func (r *searchConversationRepoStub) Update(_ context.Context, _ string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *searchConversationRepoStub) SetTopic(_ context.Context, _ string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *searchConversationRepoStub) SetPurpose(_ context.Context, _ string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
}
func (r *searchConversationRepoStub) List(_ context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	items := append([]domain.Conversation(nil), r.pages[params.WorkspaceID]...)
	return &domain.CursorPage[domain.Conversation]{Items: items}, nil
}
func (r *searchConversationRepoStub) Archive(_ context.Context, _ string) error   { return nil }
func (r *searchConversationRepoStub) Unarchive(_ context.Context, _ string) error { return nil }
func (r *searchConversationRepoStub) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *searchConversationRepoStub) AddMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (r *searchConversationRepoStub) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (r *searchConversationRepoStub) RemoveMemberByAccount(_ context.Context, _, _ string) error {
	return nil
}
func (r *searchConversationRepoStub) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *searchConversationRepoStub) ListMemberAccounts(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return &domain.CursorPage[domain.ConversationMember]{Items: []domain.ConversationMember{}}, nil
}
func (r *searchConversationRepoStub) IsMember(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (r *searchConversationRepoStub) IsAccountMember(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (s *searchClientStub) Upsert(ctx context.Context, namespace string, id string, embedding []float32, metadata map[string]any) error {
	return nil
}

func (s *searchClientStub) Delete(ctx context.Context, namespace string, id string) error {
	return nil
}

func (s *searchClientStub) Query(ctx context.Context, namespace string, embedding []float32, limit int, filters map[string]any) ([]VectorResult, error) {
	if results, ok := s.resultsByNamespace[namespace]; ok {
		return results, nil
	}
	return nil, nil
}

func (s *searchClientStub) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func TestSearchService_Search_AllowsExternalMemberSharedWorkspace(t *testing.T) {
	svc := NewSearchService(&searchClientStub{
		resultsByNamespace: map[string][]VectorResult{
			"00": {
				{
					ID:    "conversation:C123",
					Score: 0.99,
					Metadata: map[string]any{
						"type":         "conversation",
						"workspace_id": "T123",
						"data":         map[string]any{"id": "C123", "name": "shared"},
					},
				},
				{
					ID:    "conversation:C999",
					Score: 0.98,
					Metadata: map[string]any{
						"type":         "conversation",
						"workspace_id": "T123",
						"data":         map[string]any{"id": "C999", "name": "other"},
					},
				},
				{
					ID:    "message:C123:123.456",
					Score: 0.97,
					Metadata: map[string]any{
						"type":         "message",
						"workspace_id": "T123",
						"data":         map[string]any{"channel_id": "C123", "ts": "123.456"},
					},
				},
				{
					ID:    "user:U123",
					Score: 0.96,
					Metadata: map[string]any{
						"type":         "user",
						"workspace_id": "T123",
						"data":         map[string]any{"id": "U123"},
					},
				},
			},
		},
	})
	svc.SetConversationRepository(&searchConversationRepoStub{
		pages: map[string][]domain.Conversation{
			"T123": {{ID: "C123", WorkspaceID: "T123", OwnerType: domain.ConversationOwnerTypeWorkspace, OwnerWorkspaceID: "T123"}},
		},
	})
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"C123|A123": {
				ConversationID:      "C123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionMessagesRead, domain.PermissionFilesRead},
			},
		},
	})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U_EXT", "T999"), "A123")
	results, err := svc.Search(ctx, domain.SearchParams{
		WorkspaceID: "T123",
		Query:       "shared",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Type != "conversation" || results[1].Type != "message" {
		t.Fatalf("unexpected result types: %#v", results)
	}
}

func TestSearchService_Search_FiltersGuestWorkspaceEvenWithWorkspaceContext(t *testing.T) {
	svc := NewSearchService(&searchClientStub{
		resultsByNamespace: map[string][]VectorResult{
			"00": {
				{
					ID:    "conversation:C123",
					Score: 0.99,
					Metadata: map[string]any{
						"type":         "conversation",
						"workspace_id": "T123",
						"data":         map[string]any{"id": "C123", "name": "shared"},
					},
				},
				{
					ID:    "conversation:C999",
					Score: 0.98,
					Metadata: map[string]any{
						"type":         "conversation",
						"workspace_id": "T123",
						"data":         map[string]any{"id": "C999", "name": "other"},
					},
				},
				{
					ID:    "message:C123:123.456",
					Score: 0.97,
					Metadata: map[string]any{
						"type":         "message",
						"workspace_id": "T123",
						"data":         map[string]any{"channel_id": "C123", "ts": "123.456"},
					},
				},
			},
		},
	})
	svc.SetConversationRepository(&searchConversationRepoStub{
		pages: map[string][]domain.Conversation{
			"T123": {{ID: "C123", WorkspaceID: "T123", OwnerType: domain.ConversationOwnerTypeWorkspace, OwnerWorkspaceID: "T123"}},
		},
	})
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"C123|A123": {
				ConversationID:      "C123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionMessagesRead, domain.PermissionFilesRead},
			},
		},
	})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U_EXT", "T123"), "A123")
	results, err := svc.Search(ctx, domain.SearchParams{
		WorkspaceID: "T123",
		Query:       "shared",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Type == "conversation" {
			var payload map[string]any
			if err := json.Unmarshal(result.Data, &payload); err != nil {
				t.Fatalf("unmarshal conversation payload: %v", err)
			}
			if payload["id"] != "C123" {
				t.Fatalf("unexpected conversation result: %+v", payload)
			}
		}
	}
}
