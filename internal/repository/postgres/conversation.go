package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/slackbackend/internal/domain"
)

// ConversationRepo implements repository.ConversationRepository using Postgres.
type ConversationRepo struct {
	pool *pgxpool.Pool
}

// NewConversationRepo creates a new ConversationRepo.
func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{pool: pool}
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

	var c domain.Conversation
	err := r.pool.QueryRow(ctx, `
		INSERT INTO conversations (id, team_id, name, type, creator_id, topic_value, topic_creator, purpose_value, purpose_creator)
		VALUES ($1, $2, $3, $4, $5, $6, $5, $7, $5)
		RETURNING id, team_id, name, type, creator_id, is_archived,
		          topic_value, topic_creator, topic_last_set,
		          purpose_value, purpose_creator, purpose_last_set,
		          num_members, created_at, updated_at`,
		id, params.TeamID, params.Name, string(params.Type), params.CreatorID,
		params.Topic, params.Purpose,
	).Scan(
		&c.ID, &c.TeamID, &c.Name, &c.Type, &c.CreatorID, &c.IsArchived,
		&c.Topic.Value, &c.Topic.Creator, &c.Topic.LastSet,
		&c.Purpose.Value, &c.Purpose.Creator, &c.Purpose.LastSet,
		&c.NumMembers, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert conversation: %w", err)
	}

	// Add creator as first member
	if err := r.AddMember(ctx, id, params.CreatorID); err != nil {
		return nil, fmt.Errorf("add creator as member: %w", err)
	}
	c.NumMembers = 1

	return &c, nil
}

func (r *ConversationRepo) Get(ctx context.Context, id string) (*domain.Conversation, error) {
	return r.scanConversation(ctx, `
		SELECT id, team_id, name, type, creator_id, is_archived,
		       topic_value, topic_creator, topic_last_set,
		       purpose_value, purpose_creator, purpose_last_set,
		       num_members, created_at, updated_at
		FROM conversations WHERE id = $1`, id)
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

	return r.scanConversation(ctx, `
		UPDATE conversations SET name = $2, is_archived = $3
		WHERE id = $1
		RETURNING id, team_id, name, type, creator_id, is_archived,
		          topic_value, topic_creator, topic_last_set,
		          purpose_value, purpose_creator, purpose_last_set,
		          num_members, created_at, updated_at`,
		id, name, isArchived)
}

func (r *ConversationRepo) SetTopic(ctx context.Context, id string, params domain.SetTopicParams) (*domain.Conversation, error) {
	return r.scanConversation(ctx, `
		UPDATE conversations SET topic_value = $2, topic_creator = $3, topic_last_set = NOW()
		WHERE id = $1
		RETURNING id, team_id, name, type, creator_id, is_archived,
		          topic_value, topic_creator, topic_last_set,
		          purpose_value, purpose_creator, purpose_last_set,
		          num_members, created_at, updated_at`,
		id, params.Topic, params.SetByID)
}

func (r *ConversationRepo) SetPurpose(ctx context.Context, id string, params domain.SetPurposeParams) (*domain.Conversation, error) {
	return r.scanConversation(ctx, `
		UPDATE conversations SET purpose_value = $2, purpose_creator = $3, purpose_last_set = NOW()
		WHERE id = $1
		RETURNING id, team_id, name, type, creator_id, is_archived,
		          topic_value, topic_creator, topic_last_set,
		          purpose_value, purpose_creator, purpose_last_set,
		          num_members, created_at, updated_at`,
		id, params.Purpose, params.SetByID)
}

func (r *ConversationRepo) Archive(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE conversations SET is_archived = TRUE WHERE id = $1 AND is_archived = FALSE`, id)
	if err != nil {
		return fmt.Errorf("archive conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ConversationRepo) Unarchive(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE conversations SET is_archived = FALSE WHERE id = $1 AND is_archived = TRUE`, id)
	if err != nil {
		return fmt.Errorf("unarchive conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ConversationRepo) List(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var args []any
	query := `
		SELECT id, team_id, name, type, creator_id, is_archived,
		       topic_value, topic_creator, topic_last_set,
		       purpose_value, purpose_creator, purpose_last_set,
		       num_members, created_at, updated_at
		FROM conversations
		WHERE team_id = $1`
	args = append(args, params.TeamID)

	if len(params.Types) > 0 {
		typeStrs := make([]string, len(params.Types))
		for i, t := range params.Types {
			typeStrs[i] = string(t)
		}
		args = append(args, typeStrs)
		query += fmt.Sprintf(` AND type = ANY($%d)`, len(args))
	}

	if params.ExcludeArchived {
		query += ` AND is_archived = FALSE`
	}

	if params.Cursor != "" {
		args = append(args, params.Cursor)
		query += fmt.Sprintf(` AND id > $%d`, len(args))
	}

	query += ` ORDER BY id ASC`
	args = append(args, limit+1)
	query += fmt.Sprintf(` LIMIT $%d`, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var conversations []domain.Conversation
	for rows.Next() {
		c, err := scanConversationRow(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, *c)
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
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversation_members (conversation_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, conversationID, userID)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	// Update num_members count
	_, err = r.pool.Exec(ctx, `
		UPDATE conversations SET num_members = (
			SELECT COUNT(*) FROM conversation_members WHERE conversation_id = $1
		) WHERE id = $1`, conversationID)
	if err != nil {
		return fmt.Errorf("update member count: %w", err)
	}
	return nil
}

func (r *ConversationRepo) RemoveMember(ctx context.Context, conversationID, userID string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM conversation_members WHERE conversation_id = $1 AND user_id = $2`,
		conversationID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotInChannel
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE conversations SET num_members = (
			SELECT COUNT(*) FROM conversation_members WHERE conversation_id = $1
		) WHERE id = $1`, conversationID)
	if err != nil {
		return fmt.Errorf("update member count: %w", err)
	}
	return nil
}

func (r *ConversationRepo) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var args []any
	query := `SELECT conversation_id, user_id, joined_at FROM conversation_members WHERE conversation_id = $1`
	args = append(args, conversationID)

	if cursor != "" {
		args = append(args, cursor)
		query += fmt.Sprintf(` AND user_id > $%d`, len(args))
	}

	query += ` ORDER BY user_id ASC`
	args = append(args, limit+1)
	query += fmt.Sprintf(` LIMIT $%d`, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	var members []domain.ConversationMember
	for rows.Next() {
		var m domain.ConversationMember
		if err := rows.Scan(&m.ConversationID, &m.UserID, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
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
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2)`,
		conversationID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return exists, nil
}

func (r *ConversationRepo) scanConversation(ctx context.Context, query string, args ...any) (*domain.Conversation, error) {
	row := r.pool.QueryRow(ctx, query, args...)
	var c domain.Conversation
	err := row.Scan(
		&c.ID, &c.TeamID, &c.Name, &c.Type, &c.CreatorID, &c.IsArchived,
		&c.Topic.Value, &c.Topic.Creator, &c.Topic.LastSet,
		&c.Purpose.Value, &c.Purpose.Creator, &c.Purpose.LastSet,
		&c.NumMembers, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	return &c, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanConversationRow(row scannable) (*domain.Conversation, error) {
	var c domain.Conversation
	err := row.Scan(
		&c.ID, &c.TeamID, &c.Name, &c.Type, &c.CreatorID, &c.IsArchived,
		&c.Topic.Value, &c.Topic.Creator, &c.Topic.LastSet,
		&c.Purpose.Value, &c.Purpose.Creator, &c.Purpose.LastSet,
		&c.NumMembers, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan conversation row: %w", err)
	}
	return &c, nil
}
