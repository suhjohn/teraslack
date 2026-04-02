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

type capturingInternalEventStore struct {
	event *domain.InternalEvent
}

func (s *capturingInternalEventStore) WithTx(tx pgx.Tx) repository.InternalEventStoreRepository {
	return s
}

func (s *capturingInternalEventStore) Append(_ context.Context, event domain.InternalEvent) (*domain.InternalEvent, error) {
	copy := event
	s.event = &copy
	return &copy, nil
}

func (s *capturingInternalEventStore) GetByAggregate(_ context.Context, _, _ string) ([]domain.InternalEvent, error) {
	return nil, nil
}

func (s *capturingInternalEventStore) GetAllSince(_ context.Context, _ int64, _ int) ([]domain.InternalEvent, error) {
	return nil, nil
}

func (s *capturingInternalEventStore) GetAllSinceByShard(_ context.Context, _ int, _ int64, _ int) ([]domain.InternalEvent, error) {
	return nil, nil
}

type capturingAuthorizationAuditRepo struct {
	params *domain.CreateAuthorizationAuditLogParams
}

func (r *capturingAuthorizationAuditRepo) WithTx(tx pgx.Tx) repository.AuthorizationAuditRepository {
	return r
}

func (r *capturingAuthorizationAuditRepo) Create(_ context.Context, params domain.CreateAuthorizationAuditLogParams) (*domain.AuthorizationAuditLog, error) {
	copy := params
	r.params = &copy
	return &domain.AuthorizationAuditLog{}, nil
}

func (r *capturingAuthorizationAuditRepo) List(_ context.Context, params domain.ListAuthorizationAuditLogsParams) ([]domain.AuthorizationAuditLog, error) {
	return nil, nil
}

func TestRequireWorkspaceAdminActor_AllowsMembershipOnlyActor(t *testing.T) {
	ctx := ctxutil.WithUser(context.Background(), "", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A123", "WM123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	actor, err := requireWorkspaceAdminActor(ctx, newMockUserRepoTenant())
	if err != nil {
		t.Fatalf("requireWorkspaceAdminActor() error = %v", err)
	}
	if actor.WorkspaceID != "T123" {
		t.Fatalf("actor workspace_id = %q, want T123", actor.WorkspaceID)
	}
	if actor.AccountType != domain.AccountTypeAdmin {
		t.Fatalf("actor account_type = %q, want %q", actor.AccountType, domain.AccountTypeAdmin)
	}
}

func TestEventRecorder_RecordAddsCanonicalActorMetadata(t *testing.T) {
	store := &capturingInternalEventStore{}
	recorder := NewEventRecorder(store)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A123", "WM123")

	if err := recorder.Record(ctx, domain.InternalEvent{
		EventType:     domain.EventWorkspaceUpdated,
		AggregateType: domain.AggregateWorkspace,
		AggregateID:   "T123",
		WorkspaceID:   "T123",
		Payload:       json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if store.event == nil {
		t.Fatal("expected event to be captured")
	}
	if store.event.ActorID != "U123" {
		t.Fatalf("event actor_id = %q, want U123", store.event.ActorID)
	}

	var metadata map[string]string
	if err := json.Unmarshal(store.event.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["actor_account_id"] != "A123" {
		t.Fatalf("actor_account_id = %q, want A123", metadata["actor_account_id"])
	}
	if metadata["actor_membership_id"] != "WM123" {
		t.Fatalf("actor_membership_id = %q, want WM123", metadata["actor_membership_id"])
	}
}

func TestRecordAuthorizationAuditAddsCanonicalActorMetadata(t *testing.T) {
	repo := &capturingAuthorizationAuditRepo{}
	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithIdentity(ctx, "A123", "WM123")

	if err := recordAuthorizationAudit(ctx, repo, nil, "T123", "test.action", "conversation", "C123", map[string]any{
		"note": "ok",
	}); err != nil {
		t.Fatalf("recordAuthorizationAudit() error = %v", err)
	}
	if repo.params == nil {
		t.Fatal("expected audit params to be captured")
	}
	if repo.params.ActorID != "U123" {
		t.Fatalf("audit actor_id = %q, want U123", repo.params.ActorID)
	}

	var metadata map[string]any
	if err := json.Unmarshal(repo.params.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["actor_account_id"] != "A123" {
		t.Fatalf("actor_account_id = %#v, want A123", metadata["actor_account_id"])
	}
	if metadata["actor_membership_id"] != "WM123" {
		t.Fatalf("actor_membership_id = %#v, want WM123", metadata["actor_membership_id"])
	}
	if metadata["note"] != "ok" {
		t.Fatalf("note = %#v, want ok", metadata["note"])
	}
}
