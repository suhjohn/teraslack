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

	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

	var row *domain.Message
	if params.UserID != "" {
		created, err := qtx.CreateMessageByUser(ctx, sqlcgen.CreateMessageByUserParams{
			Ts:        ts,
			ChannelID: params.ChannelID,
			Text:      params.Text,
			ThreadTs:  stringToText(params.ThreadTS),
			Type:      "message",
			Blocks:    params.Blocks,
			Metadata:  params.Metadata,
			UserID:    params.UserID,
		})
		if err != nil {
			return nil, fmt.Errorf("insert message: %w", err)
		}
		row = msgToDomain(created)
	} else {
		created, err := qtx.CreateMessage(ctx, sqlcgen.CreateMessageParams{
			Ts:                          ts,
			ChannelID:                   params.ChannelID,
			UserID:                      "",
			AuthorAccountID:             stringToText(params.AuthorAccountID),
			AuthorWorkspaceMembershipID: pgtype.Text{},
			Text:                        params.Text,
			ThreadTs:                    stringToText(params.ThreadTS),
			Type:                        "message",
			Blocks:                      params.Blocks,
			Metadata:                    params.Metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("insert message: %w", err)
		}
		row = msgToDomain(created)
	}

	if params.ThreadTS != "" {
		if err := qtx.IncrementParentReplyCountAndLatestReply(ctx, sqlcgen.IncrementParentReplyCountAndLatestReplyParams{
			ChannelID:   params.ChannelID,
			Ts:          params.ThreadTS,
			LatestReply: stringToText(ts),
		}); err != nil {
			return nil, fmt.Errorf("increment parent reply count: %w", err)
		}

		rowsAffected, err := qtx.AddThreadParticipant(ctx, sqlcgen.AddThreadParticipantParams{
			ChannelID: params.ChannelID,
			ThreadTs:  params.ThreadTS,
			UserID:    params.UserID,
		})
		if err != nil {
			return nil, fmt.Errorf("add thread participant: %w", err)
		}
		if rowsAffected == 1 {
			if err := qtx.IncrementParentReplyUsersCount(ctx, sqlcgen.IncrementParentReplyUsersCountParams{
				ChannelID: params.ChannelID,
				Ts:        params.ThreadTS,
			}); err != nil {
				return nil, fmt.Errorf("increment parent reply users count: %w", err)
			}
		}
		if err := qtx.UpdateConversationLastActivity(ctx, sqlcgen.UpdateConversationLastActivityParams{
			ID: params.ChannelID,
			Ts: stringToText(ts),
		}); err != nil {
			return nil, fmt.Errorf("update conversation last activity: %w", err)
		}
	} else {
		if err := qtx.UpdateConversationLastMessageAndActivity(ctx, sqlcgen.UpdateConversationLastMessageAndActivityParams{
			ID: params.ChannelID,
			Ts: stringToText(ts),
		}); err != nil {
			return nil, fmt.Errorf("update conversation last message and activity: %w", err)
		}
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}

	return row, nil
}

func (r *MessageRepo) GetRow(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	row, err := r.q.GetMessageRow(ctx, sqlcgen.GetMessageRowParams{
		ChannelID: channelID,
		Ts:        ts,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get message: %w", err)
	}

	return msgToDomain(row), nil
}

func (r *MessageRepo) Get(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	msg, err := r.GetRow(ctx, channelID, ts)
	if err != nil {
		return nil, err
	}

	reactions, err := r.GetReactions(ctx, channelID, ts)
	if err != nil {
		return nil, fmt.Errorf("get reactions: %w", err)
	}
	msg.Reactions = reactions

	return msg, nil
}

func (r *MessageRepo) Update(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error) {
	existing, err := r.GetRow(ctx, channelID, ts)
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

	cursor := params.Cursor
	if cursor == "" {
		cursor = fmt.Sprintf("%d.999999", time.Now().Add(time.Hour).Unix())
	}

	rows, err := r.q.ListMessagesHistory(ctx, sqlcgen.ListMessagesHistoryParams{
		ChannelID: params.ChannelID,
		Ts:        cursor,
		Limit:     int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	messages := make([]domain.Message, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, *msgToDomain(row))
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

	if params.Cursor != "" {
		rows, err := r.q.ListReplies(ctx, sqlcgen.ListRepliesParams{
			ChannelID: params.ChannelID,
			ThreadTs:  threadTs,
			Ts:        params.Cursor,
			Limit:     int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list replies: %w", err)
		}
		messages := make([]domain.Message, 0, len(rows))
		for _, row := range rows {
			messages = append(messages, *msgToDomain(row))
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
	} else {
		rows, err := r.q.ListRepliesNoCursor(ctx, sqlcgen.ListRepliesNoCursorParams{
			ChannelID: params.ChannelID,
			ThreadTs:  threadTs,
			Limit:     int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list replies: %w", err)
		}
		messages := make([]domain.Message, 0, len(rows))
		for _, row := range rows {
			messages = append(messages, *msgToDomain(row))
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
}

func (r *MessageRepo) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	rowsAffected, err := r.q.AddReaction(ctx, sqlcgen.AddReactionParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		UserID:    params.UserID,
		Emoji:     params.Emoji,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domain.ErrAlreadyReacted
	}
	return nil
}

func (r *MessageRepo) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	rowsAffected, err := r.q.RemoveReaction(ctx, sqlcgen.RemoveReactionParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		UserID:    params.UserID,
		Emoji:     params.Emoji,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domain.ErrNoReaction
	}
	return nil
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
