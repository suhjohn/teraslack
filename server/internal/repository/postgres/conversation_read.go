package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type ConversationReadRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewConversationReadRepo(db DBTX) *ConversationReadRepo {
	return &ConversationReadRepo{q: sqlcgen.New(db), db: db}
}

func (r *ConversationReadRepo) WithTx(tx pgx.Tx) repository.ConversationReadRepository {
	return &ConversationReadRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ConversationReadRepo) Upsert(ctx context.Context, read domain.ConversationRead) error {
	if read.AccountID == "" {
		return fmt.Errorf("account_id: %w", domain.ErrInvalidArgument)
	}
	return r.UpsertByAccount(ctx, read.ConversationID, read.AccountID, read.LastReadTS, read.LastReadAt)
}

func (r *ConversationReadRepo) UpsertByAccount(ctx context.Context, conversationID, accountID, lastReadTS string, lastReadAt time.Time) error {
	if err := r.q.UpsertConversationReadV2(ctx, sqlcgen.UpsertConversationReadV2Params{
		ConversationID: conversationID,
		AccountID:      accountID,
		LastReadTs:     lastReadTS,
		LastReadAt:     lastReadAt,
	}); err != nil {
		return fmt.Errorf("upsert conversation read v2: %w", err)
	}
	return nil
}

func (r *ConversationReadRepo) GetByAccount(ctx context.Context, conversationID, accountID string) (*domain.ConversationRead, error) {
	read, err := r.q.GetConversationReadV2(ctx, sqlcgen.GetConversationReadV2Params{
		ConversationID: conversationID,
		AccountID:      accountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation read v2: %w", err)
	}
	return &domain.ConversationRead{
		ConversationID: read.ConversationID,
		AccountID:      read.AccountID,
		LastReadTS:     read.LastReadTs,
		LastReadAt:     read.LastReadAt,
	}, nil
}
