package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type ConversationRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewConversationRepo(db DBTX) *ConversationRepo {
	return &ConversationRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new ConversationRepo that operates within the given transaction.
func (r *ConversationRepo) WithTx(tx pgx.Tx) repository.ConversationRepository {
	return &ConversationRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ConversationRepo) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	if params.Type == domain.ConversationTypeIM && len(params.UserIDs) == 1 {
		return r.createCanonicalDM(ctx, params, params.UserIDs[0])
	}

	prefix := "C"
	switch params.Type {
	case domain.ConversationTypePrivateChannel, domain.ConversationTypeMPIM:
		prefix = "G"
	case domain.ConversationTypeIM:
		prefix = "D"
	}
	id := generateID(prefix)

	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateConversation(ctx, sqlcgen.CreateConversationParams{
		ID:             id,
		WorkspaceID:         params.WorkspaceID,
		Name:           params.Name,
		Type:           string(params.Type),
		CreatorID:      params.CreatorID,
		TopicValue:     params.Topic,
		TopicCreator:   params.CreatorID,
		PurposeValue:   params.Purpose,
		PurposeCreator: params.CreatorID,
		LastMessageTs:  pgText(""),
		LastActivityTs: pgText(""),
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert conversation: %w", err)
	}

	if _, err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
		ConversationID: id,
		UserID:         params.CreatorID,
	}); err != nil {
		return nil, fmt.Errorf("add creator as member: %w", err)
	}
	if err := qtx.IncrementConversationMemberCount(ctx, id); err != nil {
		return nil, fmt.Errorf("increment member count: %w", err)
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}

	c := convToDomain(row)
	c.NumMembers = 1
	return c, nil
}

func (r *ConversationRepo) createCanonicalDM(ctx context.Context, params domain.CreateConversationParams, otherUserID string) (*domain.Conversation, error) {
	userLowID, userHighID := sortPair(params.CreatorID, otherUserID)

	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	qtx := r.q.WithTx(tx)
	existing, err := qtx.GetCanonicalDMConversation(ctx, sqlcgen.GetCanonicalDMConversationParams{
		WorkspaceID:     params.WorkspaceID,
		UserLowID:  userLowID,
		UserHighID: userHighID,
	})
	if err == nil {
		return convToDomain(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get canonical dm: %w", err)
	}

	id := generateID("D")
	row, err := qtx.CreateConversation(ctx, sqlcgen.CreateConversationParams{
		ID:             id,
		WorkspaceID:         params.WorkspaceID,
		Name:           params.Name,
		Type:           string(domain.ConversationTypeIM),
		CreatorID:      params.CreatorID,
		TopicValue:     params.Topic,
		TopicCreator:   params.CreatorID,
		PurposeValue:   params.Purpose,
		PurposeCreator: params.CreatorID,
		LastMessageTs:  pgText(""),
		LastActivityTs: pgText(""),
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert conversation: %w", err)
	}

	memberIDs := []string{params.CreatorID, otherUserID}
	for _, memberID := range memberIDs {
		rowsAffected, err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
			ConversationID: id,
			UserID:         memberID,
		})
		if err != nil {
			return nil, fmt.Errorf("add dm member: %w", err)
		}
		if rowsAffected == 1 {
			if err := qtx.IncrementConversationMemberCount(ctx, id); err != nil {
				return nil, fmt.Errorf("increment dm member count: %w", err)
			}
		}
	}

	rowsAffected, err := qtx.CreateCanonicalDM(ctx, sqlcgen.CreateCanonicalDMParams{
		WorkspaceID:         params.WorkspaceID,
		UserLowID:      userLowID,
		UserHighID:     userHighID,
		ConversationID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("create canonical dm mapping: %w", err)
	}
	if rowsAffected == 0 {
		existing, err := qtx.GetCanonicalDMConversation(ctx, sqlcgen.GetCanonicalDMConversationParams{
			WorkspaceID:     params.WorkspaceID,
			UserLowID:  userLowID,
			UserHighID: userHighID,
		})
		if err != nil {
			return nil, fmt.Errorf("reload canonical dm: %w", err)
		}
		return convToDomain(existing), nil
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}

	conv := convToDomain(row)
	conv.NumMembers = 2
	return conv, nil
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

func (r *ConversationRepo) GetCanonicalDM(ctx context.Context, workspaceID, userAID, userBID string) (*domain.Conversation, error) {
	userLowID, userHighID := sortPair(userAID, userBID)
	row, err := r.q.GetCanonicalDMConversation(ctx, sqlcgen.GetCanonicalDMConversationParams{
		WorkspaceID:     workspaceID,
		UserLowID:  userLowID,
		UserHighID: userHighID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get canonical dm: %w", err)
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

	typeStrs := make([]string, len(params.Types))
	for i, t := range params.Types {
		typeStrs[i] = string(t)
	}
	cursorActivity, cursorID := parseConversationCursor(params.Cursor)

	rows, err := r.q.ListVisibleConversations(ctx, sqlcgen.ListVisibleConversationsParams{
		UserID:          params.UserID,
		WorkspaceID:          params.WorkspaceID,
		ExcludeArchived: params.ExcludeArchived,
		Types:           typeStrs,
		CursorActivity:  cursorActivity,
		CursorID:        pgText(cursorID),
		LimitCount:      int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	conversations := make([]domain.Conversation, 0, len(rows))
	for _, row := range rows {
		conversations = append(conversations, *convToDomain(row))
	}

	page := &domain.CursorPage[domain.Conversation]{}
	if len(conversations) > limit {
		page.HasMore = true
		page.NextCursor = formatConversationCursor(conversations[limit].LastActivityTS, conversations[limit].ID)
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
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

	// Lock the conversation row to prevent concurrent member count races
	if _, err := qtx.LockConversationForUpdate(ctx, conversationID); err != nil {
		return fmt.Errorf("lock conversation: %w", err)
	}
	conv, err := qtx.GetConversation(ctx, conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("get conversation: %w", err)
	}
	if conv.Type == string(domain.ConversationTypeIM) && conv.NumMembers >= 2 {
		return domain.ErrInvalidArgument
	}

	existingMembers, err := qtx.ListConversationMembers(ctx, sqlcgen.ListConversationMembersParams{
		ConversationID: conversationID,
		UserID:         "",
		Limit:          2,
	})
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	rowsAffected, err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrAlreadyInChannel
	}
	if err := qtx.IncrementConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("increment member count: %w", err)
	}

	if conv.Type == string(domain.ConversationTypeIM) && len(existingMembers) == 1 {
		existingUserID := existingMembers[0].UserID
		userLowID, userHighID := sortPair(existingUserID, userID)
		mappingRows, err := qtx.CreateCanonicalDM(ctx, sqlcgen.CreateCanonicalDMParams{
			WorkspaceID:         conv.WorkspaceID,
			UserLowID:      userLowID,
			UserHighID:     userHighID,
			ConversationID: conversationID,
		})
		if err != nil {
			return fmt.Errorf("create canonical dm mapping: %w", err)
		}
		if mappingRows == 0 {
			return domain.ErrAlreadyExists
		}
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
}

func (r *ConversationRepo) RemoveMember(ctx context.Context, conversationID, userID string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

	// Lock the conversation row to prevent concurrent member count races
	if _, err := qtx.LockConversationForUpdate(ctx, conversationID); err != nil {
		return fmt.Errorf("lock conversation: %w", err)
	}
	conv, err := qtx.GetConversation(ctx, conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("get conversation: %w", err)
	}

	rowsAffected, err := qtx.RemoveConversationMember(ctx, sqlcgen.RemoveConversationMemberParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotInChannel
	}

	if err := qtx.DecrementConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("decrement member count: %w", err)
	}
	if conv.Type == string(domain.ConversationTypeIM) && conv.NumMembers == 2 {
		if _, err := qtx.DeleteCanonicalDMByConversation(ctx, conversationID); err != nil {
			return fmt.Errorf("delete canonical dm mapping: %w", err)
		}
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
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
			JoinedAt:       row.JoinedAt,
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

func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func sortPair(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}

func parseConversationCursor(cursor string) (string, string) {
	if cursor == "" {
		return "", ""
	}
	left, right, ok := strings.Cut(cursor, "|")
	if !ok {
		return cursor, ""
	}
	return left, right
}

func formatConversationCursor(activityTS *string, id string) string {
	if activityTS == nil {
		return "|" + id
	}
	return *activityTS + "|" + id
}
