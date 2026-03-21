package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type MessageRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewMessageRepo(db DBTX) *MessageRepo {
	return &MessageRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new MessageRepo that operates within the given transaction.
func (r *MessageRepo) WithTx(tx pgx.Tx) repository.MessageRepository {
	return &MessageRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *MessageRepo) Create(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error) {
	now := timeNow()
	ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)

	if params.ThreadTS != "" {
		// Thread reply requires updating parent stats in same tx
		tx, err := r.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)
		qtx := r.q.WithTx(tx)

		row, err := qtx.CreateMessage(ctx, sqlcgen.CreateMessageParams{
			Ts:        ts,
			ChannelID: params.ChannelID,
			UserID:    params.UserID,
			Text:      params.Text,
			ThreadTs:  stringToText(params.ThreadTS),
			Type:      "message",
			Blocks:    params.Blocks,
			Metadata:  params.Metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("insert message: %w", err)
		}

		if err := qtx.UpdateParentReplyStats(ctx, sqlcgen.UpdateParentReplyStatsParams{
			ChannelID:   params.ChannelID,
			ThreadTs:    stringToText(params.ThreadTS),
			LatestReply: stringToText(ts),
		}); err != nil {
			return nil, fmt.Errorf("update parent reply stats: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		return msgToDomain(row), nil
	}

	// Non-threaded message: single statement, no tx needed
	row, err := r.q.CreateMessage(ctx, sqlcgen.CreateMessageParams{
		Ts:        ts,
		ChannelID: params.ChannelID,
		UserID:    params.UserID,
		Text:      params.Text,
		ThreadTs:  stringToText(params.ThreadTS),
		Type:      "message",
		Blocks:    params.Blocks,
		Metadata:  params.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	return msgToDomain(row), nil
}

func (r *MessageRepo) Get(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	row, err := r.q.GetMessage(ctx, sqlcgen.GetMessageParams{
		ChannelID: channelID,
		Ts:        ts,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get message: %w", err)
	}

	msg := msgToDomain(row)

	reactions, err := r.GetReactions(ctx, channelID, ts)
	if err != nil {
		return nil, fmt.Errorf("get reactions: %w", err)
	}
	msg.Reactions = reactions

	return msg, nil
}

func (r *MessageRepo) Update(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error) {
	existing, err := r.Get(ctx, channelID, ts)
	if err != nil {
		return nil, err
	}

	text := existing.Text
	if params.Text != nil {
		text = *params.Text
	}
	blocks := existing.Blocks
	if params.Blocks != nil {
		blocks = params.Blocks
	}
	metadata := existing.Metadata
	if params.Metadata != nil {
		metadata = params.Metadata
	}

	editedAt := strconv.FormatFloat(float64(timeNow().UnixNano())/1e9, 'f', 6, 64)

	row, err := r.q.UpdateMessage(ctx, sqlcgen.UpdateMessageParams{
		ChannelID: channelID,
		Ts:        ts,
		Text:      text,
		Blocks:    []byte(blocks),
		Metadata:  []byte(metadata),
		EditedBy:  stringToText(existing.UserID),
		EditedAt:  stringToText(editedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update message: %w", err)
	}

	return msgToDomain(row), nil
}

func (r *MessageRepo) Delete(ctx context.Context, channelID, ts string) error {
	return r.q.SoftDeleteMessage(ctx, sqlcgen.SoftDeleteMessageParams{
		ChannelID: channelID,
		Ts:        ts,
	})
}

func (r *MessageRepo) ListHistory(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var msgs []sqlcgen.Message
	var err error

	cursor := params.Cursor
	if cursor == "" {
		cursor = fmt.Sprintf("%d.999999", time.Now().Add(time.Hour).Unix())
	}

	msgs, err = r.q.ListMessagesHistory(ctx, sqlcgen.ListMessagesHistoryParams{
		ChannelID: params.ChannelID,
		Ts:        cursor,
		Limit:     int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	messages := make([]domain.Message, 0, len(msgs))
	for _, m := range msgs {
		messages = append(messages, *msgToDomain(m))
	}

	page := &domain.CursorPage[domain.Message]{}
	if len(messages) > limit {
		page.HasMore = true
		page.NextCursor = messages[limit].TS
		page.Items = messages[:limit]
	} else {
		page.Items = messages
	}
	if page.Items == nil {
		page.Items = []domain.Message{}
	}
	return page, nil
}

func (r *MessageRepo) ListReplies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	threadTs := pgtype.Text{String: params.ThreadTS, Valid: true}

	var msgs []sqlcgen.Message
	var err error

	if params.Cursor != "" {
		msgs, err = r.q.ListReplies(ctx, sqlcgen.ListRepliesParams{
			ChannelID: params.ChannelID,
			ThreadTs:  threadTs,
			Ts:        params.Cursor,
			Limit:     int32(limit + 1),
		})
	} else {
		msgs, err = r.q.ListRepliesNoCursor(ctx, sqlcgen.ListRepliesNoCursorParams{
			ChannelID: params.ChannelID,
			ThreadTs:  threadTs,
			Limit:     int32(limit + 1),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("list replies: %w", err)
	}

	messages := make([]domain.Message, 0, len(msgs))
	for _, m := range msgs {
		messages = append(messages, *msgToDomain(m))
	}

	page := &domain.CursorPage[domain.Message]{}
	if len(messages) > limit {
		page.HasMore = true
		page.NextCursor = messages[limit].TS
		page.Items = messages[:limit]
	} else {
		page.Items = messages
	}
	if page.Items == nil {
		page.Items = []domain.Message{}
	}
	return page, nil
}

func (r *MessageRepo) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	return r.q.AddReaction(ctx, sqlcgen.AddReactionParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		UserID:    params.UserID,
		Emoji:     params.Emoji,
	})
}

func (r *MessageRepo) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	return r.q.RemoveReaction(ctx, sqlcgen.RemoveReactionParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		UserID:    params.UserID,
		Emoji:     params.Emoji,
	})
}

func (r *MessageRepo) GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error) {
	rows, err := r.q.GetReactions(ctx, sqlcgen.GetReactionsParams{
		ChannelID: channelID,
		MessageTs: messageTS,
	})
	if err != nil {
		return nil, fmt.Errorf("get reactions: %w", err)
	}

	reactions := make([]domain.Reaction, 0, len(rows))
	for _, row := range rows {
		var users []string
		switch v := row.Users.(type) {
		case []string:
			users = v
		case []interface{}:
			for _, u := range v {
				if s, ok := u.(string); ok {
					users = append(users, s)
				}
			}
		}
		reactions = append(reactions, domain.Reaction{
			Name:  row.Emoji,
			Users: users,
			Count: int(row.Count),
		})
	}
	return reactions, nil
}
