package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type conversationReadRepoStub struct {
	upserted bool
}

func (r *conversationReadRepoStub) WithTx(tx pgx.Tx) repository.ConversationReadRepository {
	return r
}

func (r *conversationReadRepoStub) Upsert(ctx context.Context, read domain.ConversationRead) error {
	r.upserted = true
	return nil
}

func (r *conversationReadRepoStub) UpsertByAccount(ctx context.Context, conversationID, accountID, lastReadTS string, lastReadAt time.Time) error {
	r.upserted = true
	return nil
}

func (r *conversationReadRepoStub) GetByAccount(ctx context.Context, conversationID, accountID string) (*domain.ConversationRead, error) {
	return nil, domain.ErrNotFound
}

func TestConversationReadService_MarkRead_AllowsSystemPrincipalNoOp(t *testing.T) {
	repo := &conversationReadRepoStub{}
	svc := NewConversationReadService(repo, &conversationRepoStub{
		conversation: &domain.Conversation{
			ID:          "C123",
			WorkspaceID: "T123",
			Type:        domain.ConversationTypePrivateChannel,
		},
		isMember: false,
	})

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeSystem, domain.AccountTypePrimaryAdmin, true)
	ctx = ctxutil.WithDelegation(ctx, "", "AK_SYSTEM")
	ctx = ctxutil.WithPermissions(ctx, []string{"*"})

	if err := svc.MarkRead(ctx, domain.MarkConversationReadParams{
		ConversationID: "C123",
		LastReadTS:     "123.456",
	}); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	if repo.upserted {
		t.Fatal("MarkRead() should not persist per-user read state for the system principal")
	}
}
