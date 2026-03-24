package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ConversationReadRepo struct {
	db DBTX
}

func NewConversationReadRepo(db DBTX) *ConversationReadRepo {
	return &ConversationReadRepo{db: db}
}

func (r *ConversationReadRepo) WithTx(tx pgx.Tx) repository.ConversationReadRepository {
	return &ConversationReadRepo{db: tx}
}

func (r *ConversationReadRepo) Upsert(ctx context.Context, read domain.ConversationRead) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO conversation_reads (team_id, conversation_id, user_id, last_read_ts, last_read_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (conversation_id, user_id) DO UPDATE SET
			team_id = EXCLUDED.team_id,
			last_read_ts = EXCLUDED.last_read_ts,
			last_read_at = EXCLUDED.last_read_at
	`, read.TeamID, read.ConversationID, read.UserID, read.LastReadTS, read.LastReadAt)
	if err != nil {
		return fmt.Errorf("upsert conversation read: %w", err)
	}
	return nil
}

func (r *ConversationReadRepo) Get(ctx context.Context, conversationID, userID string) (*domain.ConversationRead, error) {
	var read domain.ConversationRead
	if err := r.db.QueryRow(ctx, `
		SELECT team_id, conversation_id, user_id, last_read_ts, last_read_at
		FROM conversation_reads
		WHERE conversation_id = $1 AND user_id = $2
	`, conversationID, userID).Scan(
		&read.TeamID, &read.ConversationID, &read.UserID, &read.LastReadTS, &read.LastReadAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation read: %w", err)
	}
	return &read, nil
}
