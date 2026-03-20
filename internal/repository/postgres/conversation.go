package postgres

import (
	"context"
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
		ID:             id,
		TeamID:         params.TeamID,
		Name:           params.Name,
		Type:           string(params.Type),
		CreatorID:      params.CreatorID,
		TopicValue:     params.Topic,
		TopicCreator:   params.CreatorID,
		PurposeValue:   params.Purpose,
		PurposeCreator: params.CreatorID,
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

	row, err := r.q.UpdateConversation(ctx, sqlcgen.UpdateConversationParams{
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

	return convToDomain(row), nil
}

func (r *ConversationRepo) SetTopic(ctx context.Context, id string, params domain.SetTopicParams) (*domain.Conversation, error) {
	row, err := r.q.SetConversationTopic(ctx, sqlcgen.SetConversationTopicParams{
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

	return convToDomain(row), nil
}

func (r *ConversationRepo) SetPurpose(ctx context.Context, id string, params domain.SetPurposeParams) (*domain.Conversation, error) {
	row, err := r.q.SetConversationPurpose(ctx, sqlcgen.SetConversationPurposeParams{
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

	return convToDomain(row), nil
}

func (r *ConversationRepo) Archive(ctx context.Context, id string) error {
	return r.q.ArchiveConversation(ctx, id)
}

func (r *ConversationRepo) Unarchive(ctx context.Context, id string) error {
	return r.q.UnarchiveConversation(ctx, id)
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
		page.NextCursor = conversations[limit].ID
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
		page.NextCursor = conversations[limit].ID
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

	// Lock the conversation row to prevent concurrent member count races
	if _, err := qtx.LockConversationForUpdate(ctx, conversationID); err != nil {
		return fmt.Errorf("lock conversation: %w", err)
	}

	if err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
		ConversationID: conversationID,
		UserID:         userID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if err := qtx.UpdateConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("update member count: %w", err)
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

	// Lock the conversation row to prevent concurrent member count races
	if _, err := qtx.LockConversationForUpdate(ctx, conversationID); err != nil {
		return fmt.Errorf("lock conversation: %w", err)
	}

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
		page.NextCursor = members[limit].UserID
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
