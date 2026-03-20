package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// MessageRepo implements repository.MessageRepository using Postgres.
type MessageRepo struct {
	pool *pgxpool.Pool
}

// NewMessageRepo creates a new MessageRepo.
func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo {
	return &MessageRepo{pool: pool}
}

// generateTS creates a Slack-style timestamp ID: "epoch.sequence".
func generateTS() string {
	now := time.Now()
	return fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
}

func (r *MessageRepo) Create(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error) {
	ts := generateTS()
	msgType := "message"

	var threadTS *string
	if params.ThreadTS != "" {
		threadTS = &params.ThreadTS
	}

	var m domain.Message
	var blocksBytes, metadataBytes []byte

	err := r.pool.QueryRow(ctx, `
		INSERT INTO messages (ts, channel_id, user_id, text, thread_ts, type, blocks, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ts, channel_id, user_id, text, thread_ts, type, subtype,
		          blocks, metadata, edited_by, edited_at,
		          reply_count, reply_users_count, latest_reply,
		          is_deleted, created_at, updated_at`,
		ts, params.ChannelID, params.UserID, params.Text, threadTS, msgType,
		nullableJSON(params.Blocks), nullableJSON(params.Metadata),
	).Scan(
		&m.TS, &m.ChannelID, &m.UserID, &m.Text, &m.ThreadTS, &m.Type, &m.Subtype,
		&blocksBytes, &metadataBytes, &m.EditedBy, &m.EditedAt,
		&m.ReplyCount, &m.ReplyUsersCount, &m.LatestReply,
		&m.IsDeleted, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	if blocksBytes != nil {
		m.Blocks = json.RawMessage(blocksBytes)
	}
	if metadataBytes != nil {
		m.Metadata = json.RawMessage(metadataBytes)
	}

	// If this is a thread reply, update parent's reply stats
	if params.ThreadTS != "" {
		if _, err := r.pool.Exec(ctx, `
			UPDATE messages
			SET reply_count = (
				SELECT COUNT(*) FROM messages WHERE channel_id = $1 AND thread_ts = $2 AND ts != $2
			),
			reply_users_count = (
				SELECT COUNT(DISTINCT user_id) FROM messages WHERE channel_id = $1 AND thread_ts = $2 AND ts != $2
			),
			latest_reply = $3
			WHERE channel_id = $1 AND ts = $2`,
			params.ChannelID, params.ThreadTS, ts,
		); err != nil {
			return nil, fmt.Errorf("update parent reply stats: %w", err)
		}
	}

	return &m, nil
}

func (r *MessageRepo) Get(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	m, err := r.scanMessage(ctx, `
		SELECT ts, channel_id, user_id, text, thread_ts, type, subtype,
		       blocks, metadata, edited_by, edited_at,
		       reply_count, reply_users_count, latest_reply,
		       is_deleted, created_at, updated_at
		FROM messages WHERE channel_id = $1 AND ts = $2`, channelID, ts)
	if err != nil {
		return nil, err
	}

	reactions, err := r.GetReactions(ctx, channelID, ts)
	if err != nil {
		return nil, fmt.Errorf("get reactions: %w", err)
	}
	m.Reactions = reactions

	return m, nil
}

func (r *MessageRepo) Update(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error) {
	existing, err := r.Get(ctx, channelID, ts)
	if err != nil {
		return nil, err
	}
	if existing.IsDeleted {
		return nil, domain.ErrNotFound
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

	editedAt := generateTS()

	m, err := r.scanMessage(ctx, `
		UPDATE messages
		SET text = $3, blocks = $4, metadata = $5, edited_by = $6, edited_at = $7
		WHERE channel_id = $1 AND ts = $2
		RETURNING ts, channel_id, user_id, text, thread_ts, type, subtype,
		          blocks, metadata, edited_by, edited_at,
		          reply_count, reply_users_count, latest_reply,
		          is_deleted, created_at, updated_at`,
		channelID, ts, text, nullableJSON(blocks), nullableJSON(metadata),
		existing.UserID, editedAt)
	if err != nil {
		return nil, err
	}

	m.Reactions = existing.Reactions
	return m, nil
}

func (r *MessageRepo) Delete(ctx context.Context, channelID, ts string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE messages SET is_deleted = TRUE, text = '' WHERE channel_id = $1 AND ts = $2 AND is_deleted = FALSE`,
		channelID, ts)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *MessageRepo) ListHistory(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var args []any
	query := `
		SELECT ts, channel_id, user_id, text, thread_ts, type, subtype,
		       blocks, metadata, edited_by, edited_at,
		       reply_count, reply_users_count, latest_reply,
		       is_deleted, created_at, updated_at
		FROM messages
		WHERE channel_id = $1 AND is_deleted = FALSE AND thread_ts IS NULL`
	args = append(args, params.ChannelID)

	if params.Latest != "" {
		args = append(args, params.Latest)
		op := "<"
		if params.Inclusive {
			op = "<="
		}
		query += fmt.Sprintf(` AND ts %s $%d`, op, len(args))
	}

	if params.Oldest != "" {
		args = append(args, params.Oldest)
		op := ">"
		if params.Inclusive {
			op = ">="
		}
		query += fmt.Sprintf(` AND ts %s $%d`, op, len(args))
	}

	// Use cursor for pagination (ts-based, descending)
	if params.Cursor != "" {
		args = append(args, params.Cursor)
		query += fmt.Sprintf(` AND ts < $%d`, len(args))
	}

	query += ` ORDER BY ts DESC`
	args = append(args, limit+1)
	query += fmt.Sprintf(` LIMIT $%d`, len(args))

	return r.queryMessages(ctx, query, limit, args...)
}

func (r *MessageRepo) ListReplies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var args []any
	query := `
		SELECT ts, channel_id, user_id, text, thread_ts, type, subtype,
		       blocks, metadata, edited_by, edited_at,
		       reply_count, reply_users_count, latest_reply,
		       is_deleted, created_at, updated_at
		FROM messages
		WHERE channel_id = $1 AND (thread_ts = $2 OR ts = $2) AND is_deleted = FALSE`
	args = append(args, params.ChannelID, params.ThreadTS)

	if params.Cursor != "" {
		args = append(args, params.Cursor)
		query += fmt.Sprintf(` AND ts > $%d`, len(args))
	}

	query += ` ORDER BY ts ASC`
	args = append(args, limit+1)
	query += fmt.Sprintf(` LIMIT $%d`, len(args))

	return r.queryMessages(ctx, query, limit, args...)
}

func (r *MessageRepo) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO reactions (channel_id, message_ts, user_id, emoji)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id, message_ts, user_id, emoji) DO NOTHING`,
		params.ChannelID, params.MessageTS, params.UserID, params.Emoji)
	if err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlreadyReacted
	}
	return nil
}

func (r *MessageRepo) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM reactions WHERE channel_id = $1 AND message_ts = $2 AND user_id = $3 AND emoji = $4`,
		params.ChannelID, params.MessageTS, params.UserID, params.Emoji)
	if err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNoReaction
	}
	return nil
}

func (r *MessageRepo) GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT emoji, ARRAY_AGG(user_id ORDER BY created_at) AS users, COUNT(*) AS count
		FROM reactions
		WHERE channel_id = $1 AND message_ts = $2
		GROUP BY emoji
		ORDER BY MIN(created_at)`, channelID, messageTS)
	if err != nil {
		return nil, fmt.Errorf("get reactions: %w", err)
	}
	defer rows.Close()

	var reactions []domain.Reaction
	for rows.Next() {
		var reaction domain.Reaction
		if err := rows.Scan(&reaction.Name, &reaction.Users, &reaction.Count); err != nil {
			return nil, fmt.Errorf("scan reaction: %w", err)
		}
		reactions = append(reactions, reaction)
	}
	return reactions, nil
}

func (r *MessageRepo) scanMessage(ctx context.Context, query string, args ...any) (*domain.Message, error) {
	var m domain.Message
	var blocksBytes, metadataBytes []byte
	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&m.TS, &m.ChannelID, &m.UserID, &m.Text, &m.ThreadTS, &m.Type, &m.Subtype,
		&blocksBytes, &metadataBytes, &m.EditedBy, &m.EditedAt,
		&m.ReplyCount, &m.ReplyUsersCount, &m.LatestReply,
		&m.IsDeleted, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan message: %w", err)
	}
	if blocksBytes != nil {
		m.Blocks = json.RawMessage(blocksBytes)
	}
	if metadataBytes != nil {
		m.Metadata = json.RawMessage(metadataBytes)
	}
	return &m, nil
}

func (r *MessageRepo) queryMessages(ctx context.Context, query string, limit int, args ...any) (*domain.CursorPage[domain.Message], error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		var m domain.Message
		var blocksBytes, metadataBytes []byte
		if err := rows.Scan(
			&m.TS, &m.ChannelID, &m.UserID, &m.Text, &m.ThreadTS, &m.Type, &m.Subtype,
			&blocksBytes, &metadataBytes, &m.EditedBy, &m.EditedAt,
			&m.ReplyCount, &m.ReplyUsersCount, &m.LatestReply,
			&m.IsDeleted, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		if blocksBytes != nil {
			m.Blocks = json.RawMessage(blocksBytes)
		}
		if metadataBytes != nil {
			m.Metadata = json.RawMessage(metadataBytes)
		}
		messages = append(messages, m)
	}

	page := &domain.CursorPage[domain.Message]{}
	if len(messages) > limit {
		page.HasMore = true
		page.NextCursor = messages[limit-1].TS
		page.Items = messages[:limit]
	} else {
		page.Items = messages
	}
	if page.Items == nil {
		page.Items = []domain.Message{}
	}
	return page, nil
}

// nullableJSON returns nil if the input is nil or empty, otherwise the raw bytes.
func nullableJSON(data json.RawMessage) []byte {
	if len(data) == 0 {
		return nil
	}
	return []byte(data)
}

// Ensure strconv is used (for potential future use in cursor encoding).
var _ = strconv.Itoa
