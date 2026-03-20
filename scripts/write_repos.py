#!/usr/bin/env python3
"""Write all repository files for sqlc + event sourcing migration."""
import os

BASE = "/home/ubuntu/repos/slackbackend/internal/repository/postgres"

files = {}

files["conversation.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type ConversationRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *ConversationRepo) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	prefix := "C"
	switch params.Type {
	case domain.ConversationTypePrivateChannel, domain.ConversationTypeMPIM:
		prefix = "G"
	case domain.ConversationTypeIM:
		prefix = "D"
	}
	id := generateID(prefix)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateConversation(ctx, sqlcgen.CreateConversationParams{
		ID:           id,
		TeamID:       params.TeamID,
		Name:         params.Name,
		Type:         string(params.Type),
		CreatorID:    params.CreatorID,
		TopicValue:   params.Topic,
		PurposeValue: params.Purpose,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert conversation: %w", err)
	}

	if err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
		ConversationID: id,
		UserID:         params.CreatorID,
	}); err != nil {
		return nil, fmt.Errorf("add creator as member: %w", err)
	}
	if err := qtx.UpdateConversationMemberCount(ctx, id); err != nil {
		return nil, fmt.Errorf("update member count: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		EventType:     domain.EventConversationCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	c := convToDomain(row)
	c.NumMembers = 1
	return c, nil
}

func (r *ConversationRepo) Get(ctx context.Context, id string) (*domain.Conversation, error) {
	row, err := r.q.GetConversation(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return convToDomain(row), nil
}

func (r *ConversationRepo) Update(ctx context.Context, id string, params domain.UpdateConversationParams) (*domain.Conversation, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if params.Name != nil {
		name = *params.Name
	}
	isArchived := existing.IsArchived
	if params.IsArchived != nil {
		isArchived = *params.IsArchived
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateConversation(ctx, sqlcgen.UpdateConversationParams{
		ID:         id,
		Name:       name,
		IsArchived: isArchived,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update conversation: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		EventType:     domain.EventConversationUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return convToDomain(row), nil
}

func (r *ConversationRepo) SetTopic(ctx context.Context, id string, params domain.SetTopicParams) (*domain.Conversation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.SetConversationTopic(ctx, sqlcgen.SetConversationTopicParams{
		ID:           id,
		TopicValue:   params.Topic,
		TopicCreator: params.SetByID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("set topic: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		EventType:     domain.EventConversationTopicSet,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return convToDomain(row), nil
}

func (r *ConversationRepo) SetPurpose(ctx context.Context, id string, params domain.SetPurposeParams) (*domain.Conversation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.SetConversationPurpose(ctx, sqlcgen.SetConversationPurposeParams{
		ID:             id,
		PurposeValue:   params.Purpose,
		PurposeCreator: params.SetByID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("set purpose: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		EventType:     domain.EventConversationPurposeSet,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return convToDomain(row), nil
}

func (r *ConversationRepo) Archive(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.ArchiveConversation(ctx, id); err != nil {
		return fmt.Errorf("archive conversation: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		EventType:     domain.EventConversationArchived,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *ConversationRepo) Unarchive(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.UnarchiveConversation(ctx, id); err != nil {
		return fmt.Errorf("unarchive conversation: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		EventType:     domain.EventConversationUnarchived,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *ConversationRepo) List(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	if len(params.Types) > 0 {
		return r.listWithTypes(ctx, params, limit)
	}

	var conversations []domain.Conversation

	if params.ExcludeArchived {
		rows, err := r.q.ListConversationsByTeamExcludeArchived(ctx, sqlcgen.ListConversationsByTeamExcludeArchivedParams{
			TeamID: params.TeamID,
			ID:     params.Cursor,
			Limit:  int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list conversations: %w", err)
		}
		for _, row := range rows {
			conversations = append(conversations, *convToDomain(row))
		}
	} else {
		rows, err := r.q.ListConversationsByTeam(ctx, sqlcgen.ListConversationsByTeamParams{
			TeamID: params.TeamID,
			ID:     params.Cursor,
			Limit:  int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list conversations: %w", err)
		}
		for _, row := range rows {
			conversations = append(conversations, *convToDomain(row))
		}
	}

	page := &domain.CursorPage[domain.Conversation]{}
	if len(conversations) > limit {
		page.HasMore = true
		page.NextCursor = conversations[limit-1].ID
		page.Items = conversations[:limit]
	} else {
		page.Items = conversations
	}
	if page.Items == nil {
		page.Items = []domain.Conversation{}
	}
	return page, nil
}

func (r *ConversationRepo) listWithTypes(ctx context.Context, params domain.ListConversationsParams, limit int) (*domain.CursorPage[domain.Conversation], error) {
	var args []any
	query := "SELECT id, team_id, name, type, creator_id, is_archived, topic_value, topic_creator, topic_last_set, purpose_value, purpose_creator, purpose_last_set, num_members, created_at, updated_at FROM conversations WHERE team_id = $1"
	args = append(args, params.TeamID)

	typeStrs := make([]string, len(params.Types))
	for i, t := range params.Types {
		typeStrs[i] = string(t)
	}
	args = append(args, typeStrs)
	query += fmt.Sprintf(" AND type = ANY($%d)", len(args))

	if params.ExcludeArchived {
		query += " AND is_archived = FALSE"
	}
	if params.Cursor != "" {
		args = append(args, params.Cursor)
		query += fmt.Sprintf(" AND id > $%d", len(args))
	}

	query += " ORDER BY id ASC"
	args = append(args, limit+1)
	query += fmt.Sprintf(" LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var conversations []domain.Conversation
	for rows.Next() {
		var c domain.Conversation
		if err := rows.Scan(
			&c.ID, &c.TeamID, &c.Name, &c.Type, &c.CreatorID, &c.IsArchived,
			&c.Topic.Value, &c.Topic.Creator, &c.Topic.LastSet,
			&c.Purpose.Value, &c.Purpose.Creator, &c.Purpose.LastSet,
			&c.NumMembers, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		conversations = append(conversations, c)
	}

	page := &domain.CursorPage[domain.Conversation]{}
	if len(conversations) > limit {
		page.HasMore = true
		page.NextCursor = conversations[limit-1].ID
		page.Items = conversations[:limit]
	} else {
		page.Items = conversations
	}
	if page.Items == nil {
		page.Items = []domain.Conversation{}
	}
	return page, nil
}

func (r *ConversationRepo) AddMember(ctx context.Context, conversationID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
		ConversationID: conversationID,
		UserID:         userID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if err := qtx.UpdateConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("update member count: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"user_id": userID})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		EventType:     domain.EventMemberJoined,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *ConversationRepo) RemoveMember(ctx context.Context, conversationID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	count, err := qtx.CountConversationMembers(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("count members: %w", err)
	}

	if err := qtx.RemoveConversationMember(ctx, sqlcgen.RemoveConversationMemberParams{
		ConversationID: conversationID,
		UserID:         userID,
	}); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	newCount, err := qtx.CountConversationMembers(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("count members: %w", err)
	}
	if newCount == count {
		return domain.ErrNotInChannel
	}

	if err := qtx.UpdateConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("update member count: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"user_id": userID})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		EventType:     domain.EventMemberLeft,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *ConversationRepo) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := r.q.ListConversationMembers(ctx, sqlcgen.ListConversationMembersParams{
		ConversationID: conversationID,
		UserID:         cursor,
		Limit:          int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	members := make([]domain.ConversationMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, domain.ConversationMember{
			ConversationID: row.ConversationID,
			UserID:         row.UserID,
			JoinedAt:       tsToTime(row.JoinedAt),
		})
	}

	page := &domain.CursorPage[domain.ConversationMember]{}
	if len(members) > limit {
		page.HasMore = true
		page.NextCursor = members[limit-1].UserID
		page.Items = members[:limit]
	} else {
		page.Items = members
	}
	if page.Items == nil {
		page.Items = []domain.ConversationMember{}
	}
	return page, nil
}

func (r *ConversationRepo) IsMember(ctx context.Context, conversationID, userID string) (bool, error) {
	exists, err := r.q.IsConversationMember(ctx, sqlcgen.IsConversationMemberParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return exists, nil
}
'''

files["message.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type MessageRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo {
	return &MessageRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *MessageRepo) Create(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error) {
	ts := fmt.Sprintf("%d.%06d", timeNow().Unix(), timeNow().Nanosecond()/1000)

	tx, err := r.pool.Begin(ctx)
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

	if params.ThreadTS != "" {
		if err := qtx.UpdateParentReplyStats(ctx, sqlcgen.UpdateParentReplyStatsParams{
			ChannelID:   params.ChannelID,
			ThreadTs:    stringToText(params.ThreadTS),
			LatestReply: stringToText(ts),
		}); err != nil {
			return nil, fmt.Errorf("update parent reply stats: %w", err)
		}
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateMessage,
		AggregateID:   params.ChannelID + ":" + ts,
		EventType:     domain.EventMessagePosted,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateMessage(ctx, sqlcgen.UpdateMessageParams{
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

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateMessage,
		AggregateID:   channelID + ":" + ts,
		EventType:     domain.EventMessageUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return msgToDomain(row), nil
}

func (r *MessageRepo) Delete(ctx context.Context, channelID, ts string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.SoftDeleteMessage(ctx, sqlcgen.SoftDeleteMessageParams{
		ChannelID: channelID,
		Ts:        ts,
	}); err != nil {
		return fmt.Errorf("soft delete message: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"channel_id": channelID, "ts": ts})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateMessage,
		AggregateID:   channelID + ":" + ts,
		EventType:     domain.EventMessageDeleted,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
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

func (r *MessageRepo) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.AddReaction(ctx, sqlcgen.AddReactionParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		UserID:    params.UserID,
		Emoji:     params.Emoji,
	}); err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateMessage,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		EventType:     domain.EventReactionAdded,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *MessageRepo) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.RemoveReaction(ctx, sqlcgen.RemoveReactionParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		UserID:    params.UserID,
		Emoji:     params.Emoji,
	}); err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateMessage,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		EventType:     domain.EventReactionRemoved,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
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
'''

files["usergroup.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type UsergroupRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewUsergroupRepo(pool *pgxpool.Pool) *UsergroupRepo {
	return &UsergroupRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *UsergroupRepo) Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	id := generateID("S")

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateUsergroup(ctx, sqlcgen.CreateUsergroupParams{
		ID:          id,
		TeamID:      params.TeamID,
		Name:        params.Name,
		Handle:      params.Handle,
		Description: params.Description,
		CreatedBy:   params.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("insert usergroup: %w", err)
	}

	for _, userID := range params.Users {
		if err := qtx.AddUsergroupMember(ctx, sqlcgen.AddUsergroupMemberParams{
			UsergroupID: id,
			UserID:      userID,
		}); err != nil {
			return nil, fmt.Errorf("add member: %w", err)
		}
	}
	if len(params.Users) > 0 {
		if err := qtx.UpdateUsergroupUserCount(ctx, id); err != nil {
			return nil, fmt.Errorf("update user count: %w", err)
		}
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	ug := usergroupToDomain(row)
	ug.UserCount = len(params.Users)
	return ug, nil
}

func (r *UsergroupRepo) Get(ctx context.Context, id string) (*domain.Usergroup, error) {
	row, err := r.q.GetUsergroup(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get usergroup: %w", err)
	}
	return usergroupToDomain(row), nil
}

func (r *UsergroupRepo) Update(ctx context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if params.Name != nil {
		name = *params.Name
	}
	handle := existing.Handle
	if params.Handle != nil {
		handle = *params.Handle
	}
	description := existing.Description
	if params.Description != nil {
		description = *params.Description
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateUsergroup(ctx, sqlcgen.UpdateUsergroupParams{
		ID:          id,
		Name:        name,
		Handle:      handle,
		Description: description,
		UpdatedBy:   params.UpdatedBy,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update usergroup: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return usergroupToDomain(row), nil
}

func (r *UsergroupRepo) List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	var rows []sqlcgen.Usergroup
	var err error

	if params.IncludeDisabled {
		rows, err = r.q.ListUsergroups(ctx, params.TeamID)
	} else {
		rows, err = r.q.ListUsergroupsIncludeDisabled(ctx, params.TeamID)
	}
	if err != nil {
		return nil, fmt.Errorf("list usergroups: %w", err)
	}

	result := make([]domain.Usergroup, 0, len(rows))
	for _, row := range rows {
		result = append(result, *usergroupToDomain(row))
	}
	return result, nil
}

func (r *UsergroupRepo) Enable(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.EnableUsergroup(ctx, id); err != nil {
		return fmt.Errorf("enable usergroup: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupEnabled,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *UsergroupRepo) Disable(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DisableUsergroup(ctx, id); err != nil {
		return fmt.Errorf("disable usergroup: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupDisabled,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *UsergroupRepo) AddUser(ctx context.Context, usergroupID, userID string) error {
	if err := r.q.AddUsergroupMember(ctx, sqlcgen.AddUsergroupMemberParams{
		UsergroupID: usergroupID,
		UserID:      userID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return r.q.UpdateUsergroupUserCount(ctx, usergroupID)
}

func (r *UsergroupRepo) ListUsers(ctx context.Context, usergroupID string) ([]string, error) {
	users, err := r.q.ListUsergroupMembers(ctx, usergroupID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return users, nil
}

func (r *UsergroupRepo) SetUsers(ctx context.Context, usergroupID string, userIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteUsergroupMembers(ctx, usergroupID); err != nil {
		return fmt.Errorf("delete members: %w", err)
	}

	for _, userID := range userIDs {
		if err := qtx.InsertUsergroupMember(ctx, sqlcgen.InsertUsergroupMemberParams{
			UsergroupID: usergroupID,
			UserID:      userID,
		}); err != nil {
			return fmt.Errorf("insert member: %w", err)
		}
	}

	if err := qtx.SetUsergroupUserCount(ctx, sqlcgen.SetUsergroupUserCountParams{
		ID:        usergroupID,
		UserCount: int32(len(userIDs)),
	}); err != nil {
		return fmt.Errorf("set user count: %w", err)
	}

	eventData, _ := json.Marshal(map[string]interface{}{"user_ids": userIDs})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   usergroupID,
		EventType:     domain.EventUsergroupUserSet,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
'''

files["pin.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type PinRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewPinRepo(pool *pgxpool.Pool) *PinRepo {
	return &PinRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *PinRepo) Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.AddPin(ctx, sqlcgen.AddPinParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		PinnedBy:  params.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("add pin: %w", err)
	}

	if row.ChannelID == "" {
		return nil, domain.ErrAlreadyPinned
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregatePin,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		EventType:     domain.EventPinAdded,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return pinToDomain(row), nil
}

func (r *PinRepo) Remove(ctx context.Context, params domain.PinParams) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.RemovePin(ctx, sqlcgen.RemovePinParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
	}); err != nil {
		return fmt.Errorf("remove pin: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregatePin,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		EventType:     domain.EventPinRemoved,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *PinRepo) List(ctx context.Context, params domain.ListPinsParams) ([]domain.Pin, error) {
	rows, err := r.q.ListPins(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("list pins: %w", err)
	}

	pins := make([]domain.Pin, 0, len(rows))
	for _, row := range rows {
		pins = append(pins, *pinToDomain(row))
	}
	return pins, nil
}
'''

files["bookmark.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type BookmarkRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewBookmarkRepo(pool *pgxpool.Pool) *BookmarkRepo {
	return &BookmarkRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *BookmarkRepo) Create(ctx context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error) {
	id := generateID("Bk")

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateBookmark(ctx, sqlcgen.CreateBookmarkParams{
		ID:        id,
		ChannelID: params.ChannelID,
		Title:     params.Title,
		Type:      params.Type,
		Link:      params.Link,
		Emoji:     params.Emoji,
		CreatedBy: params.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("insert bookmark: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		EventType:     domain.EventBookmarkCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return bookmarkToDomain(row), nil
}

func (r *BookmarkRepo) Get(ctx context.Context, id string) (*domain.Bookmark, error) {
	row, err := r.q.GetBookmark(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get bookmark: %w", err)
	}
	return bookmarkToDomain(row), nil
}

func (r *BookmarkRepo) Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	title := existing.Title
	if params.Title != nil {
		title = *params.Title
	}
	link := existing.Link
	if params.Link != nil {
		link = *params.Link
	}
	emoji := existing.Emoji
	if params.Emoji != nil {
		emoji = *params.Emoji
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateBookmark(ctx, sqlcgen.UpdateBookmarkParams{
		ID:        id,
		Title:     title,
		Link:      link,
		Emoji:     emoji,
		UpdatedBy: params.UpdatedBy,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update bookmark: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		EventType:     domain.EventBookmarkUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return bookmarkToDomain(row), nil
}

func (r *BookmarkRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteBookmark(ctx, id); err != nil {
		return fmt.Errorf("delete bookmark: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		EventType:     domain.EventBookmarkDeleted,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *BookmarkRepo) List(ctx context.Context, params domain.ListBookmarksParams) ([]domain.Bookmark, error) {
	rows, err := r.q.ListBookmarks(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks: %w", err)
	}

	bookmarks := make([]domain.Bookmark, 0, len(rows))
	for _, row := range rows {
		bookmarks = append(bookmarks, *bookmarkToDomain(row))
	}
	return bookmarks, nil
}
'''

files["file.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type FileRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewFileRepo(pool *pgxpool.Pool) *FileRepo {
	return &FileRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *FileRepo) Create(ctx context.Context, f *domain.File) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.CreateFile(ctx, sqlcgen.CreateFileParams{
		ID:                 f.ID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		S3Key:              "",
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalUrl:        f.ExternalURL,
		UploadComplete:     false,
	}); err != nil {
		return fmt.Errorf("insert file: %w", err)
	}

	eventData, _ := json.Marshal(f)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		EventType:     domain.EventFileCreated,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *FileRepo) Get(ctx context.Context, id string) (*domain.File, error) {
	row, err := r.q.GetFile(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get file: %w", err)
	}

	f := fileToDomain(row)

	channels, err := r.q.GetFileChannels(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get file channels: %w", err)
	}
	f.Channels = channels

	return f, nil
}

func (r *FileRepo) Update(ctx context.Context, f *domain.File) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.UpdateFileComplete(ctx, sqlcgen.UpdateFileCompleteParams{
		ID:                 f.ID,
		Title:              f.Title,
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
	}); err != nil {
		return fmt.Errorf("update file: %w", err)
	}

	eventData, _ := json.Marshal(f)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		EventType:     domain.EventFileUpdated,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *FileRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteFile(ctx, id); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   id,
		EventType:     domain.EventFileDeleted,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *FileRepo) List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var files []domain.File

	switch {
	case params.ChannelID != "" && params.UserID != "":
		rows, err := r.q.ListFilesByChannelAndUser(ctx, sqlcgen.ListFilesByChannelAndUserParams{
			ChannelID: params.ChannelID,
			UserID:    params.UserID,
			ID:        params.Cursor,
			Limit:     int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByChannelAndUserToDomain(row))
		}
	case params.ChannelID != "":
		rows, err := r.q.ListFilesByChannel(ctx, sqlcgen.ListFilesByChannelParams{
			ChannelID: params.ChannelID,
			ID:        params.Cursor,
			Limit:     int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByChannelToDomain(row))
		}
	case params.UserID != "":
		rows, err := r.q.ListFilesByUser(ctx, sqlcgen.ListFilesByUserParams{
			UserID: params.UserID,
			ID:     params.Cursor,
			Limit:  int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByUserToDomain(row))
		}
	default:
		rows, err := r.q.ListFiles(ctx, sqlcgen.ListFilesParams{
			ID:    params.Cursor,
			Limit: int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileListToDomain(row))
		}
	}

	page := &domain.CursorPage[domain.File]{}
	if len(files) > limit {
		page.HasMore = true
		page.NextCursor = files[limit-1].ID
		page.Items = files[:limit]
	} else {
		page.Items = files
	}
	if page.Items == nil {
		page.Items = []domain.File{}
	}
	return page, nil
}

func (r *FileRepo) ShareToChannel(ctx context.Context, fileID, channelID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.ShareFileToChannel(ctx, sqlcgen.ShareFileToChannelParams{
		FileID:    fileID,
		ChannelID: channelID,
	}); err != nil {
		return fmt.Errorf("share file: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"file_id": fileID, "channel_id": channelID})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   fileID,
		EventType:     domain.EventFileShared,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
'''

files["event.go"] = '''package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type EventRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewEventRepo(pool *pgxpool.Pool) *EventRepo {
	return &EventRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *EventRepo) CreateEvent(ctx context.Context, event *domain.Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	return r.q.CreateEventRecord(ctx, sqlcgen.CreateEventRecordParams{
		ID:      event.ID,
		Type:    event.Type,
		TeamID:  event.TeamID,
		Payload: payload,
	})
}

func (r *EventRepo) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	id := generateID("ES")

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateEventSubscription(ctx, sqlcgen.CreateEventSubscriptionParams{
		ID:         id,
		TeamID:     params.TeamID,
		Url:        params.URL,
		EventTypes: params.EventTypes,
		Secret:     params.Secret,
	})
	if err != nil {
		return nil, fmt.Errorf("insert subscription: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		EventType:     domain.EventSubscriptionCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return eventSubToDomain(row), nil
}

func (r *EventRepo) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	row, err := r.q.GetEventSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return eventSubToDomain(row), nil
}

func (r *EventRepo) UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error) {
	existing, err := r.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}

	url := existing.URL
	if params.URL != nil {
		url = *params.URL
	}
	eventTypes := existing.EventTypes
	if params.EventTypes != nil {
		eventTypes = params.EventTypes
	}
	enabled := existing.Enabled
	if params.Enabled != nil {
		enabled = *params.Enabled
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateEventSubscription(ctx, sqlcgen.UpdateEventSubscriptionParams{
		ID:         id,
		Url:        url,
		EventTypes: eventTypes,
		Enabled:    enabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update subscription: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		EventType:     domain.EventSubscriptionUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return eventSubToDomain(row), nil
}

func (r *EventRepo) DeleteSubscription(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteEventSubscription(ctx, id); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateSubscription,
		AggregateID:   id,
		EventType:     domain.EventSubscriptionDeleted,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *EventRepo) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	rows, err := r.q.ListEventSubscriptions(ctx, params.TeamID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	subs := make([]domain.EventSubscription, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, *eventSubToDomain(row))
	}
	return subs, nil
}

func (r *EventRepo) ListSubscriptionsByTeamAndEvent(ctx context.Context, teamID, eventType string) ([]domain.EventSubscription, error) {
	rows, err := r.q.ListEventSubscriptionsByTeamAndEvent(ctx, sqlcgen.ListEventSubscriptionsByTeamAndEventParams{
		TeamID:     teamID,
		EventTypes: []string{eventType},
	})
	if err != nil {
		return nil, fmt.Errorf("list subscriptions by event: %w", err)
	}

	subs := make([]domain.EventSubscription, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, *eventSubToDomain(row))
	}
	return subs, nil
}
'''

files["auth.go"] = '''package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type AuthRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAuthRepo(pool *pgxpool.Pool) *AuthRepo {
	return &AuthRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *AuthRepo) CreateToken(ctx context.Context, params domain.CreateTokenParams) (*domain.Token, error) {
	id := generateID("TK")

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	prefix := "xoxb-"
	if !params.IsBot {
		prefix = "xoxp-"
	}
	tokenStr := prefix + hex.EncodeToString(tokenBytes)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateToken(ctx, sqlcgen.CreateTokenParams{
		ID:     id,
		TeamID: params.TeamID,
		UserID: params.UserID,
		Token:  tokenStr,
		Scopes: params.Scopes,
		IsBot:  params.IsBot,
	})
	if err != nil {
		return nil, fmt.Errorf("insert token: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"token_id": id, "team_id": params.TeamID, "user_id": params.UserID})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateToken,
		AggregateID:   id,
		EventType:     domain.EventTokenCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return tokenToDomain(row), nil
}

func (r *AuthRepo) GetByToken(ctx context.Context, token string) (*domain.Token, error) {
	row, err := r.q.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, fmt.Errorf("get token: %w", err)
	}
	return tokenToDomain(row), nil
}

func (r *AuthRepo) RevokeToken(ctx context.Context, token string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.RevokeToken(ctx, token); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"token": token})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateToken,
		AggregateID:   token,
		EventType:     domain.EventTokenRevoked,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
'''

for name, content in files.items():
    path = os.path.join(BASE, name)
    with open(path, 'w') as f:
        f.write(content)
    print(f"Wrote {name}: {len(content)} bytes")

print("All repository files written successfully!")
