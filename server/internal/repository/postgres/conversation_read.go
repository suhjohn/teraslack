package postgres

import (
	"context"
	"errors"
	"fmt"

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
	if err := r.q.UpsertConversationRead(ctx, sqlcgen.UpsertConversationReadParams{
		WorkspaceID:         read.WorkspaceID,
		ConversationID: read.ConversationID,
		UserID:         read.UserID,
		LastReadTs:     read.LastReadTS,
		LastReadAt:     read.LastReadAt,
	}); err != nil {
		return fmt.Errorf("upsert conversation read: %w", err)
	}
	return nil
}

func (r *ConversationReadRepo) Get(ctx context.Context, conversationID, userID string) (*domain.ConversationRead, error) {
	read, err := r.q.GetConversationRead(ctx, sqlcgen.GetConversationReadParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation read: %w", err)
	}
	return &domain.ConversationRead{
		WorkspaceID: read.WorkspaceID,
		ConversationID: read.ConversationID,
		UserID:         read.UserID,
		LastReadTS:     read.LastReadTs,
		LastReadAt:     read.LastReadAt,
	}, nil
}
