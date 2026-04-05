package postgres

import (
	"context"
	"encoding/json"
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
	if params.OwnerType == domain.ConversationOwnerTypeWorkspace && params.Type == domain.ConversationTypeIM && len(params.UserIDs) == 1 {
		return r.createCanonicalDM(ctx, params, params.UserIDs[0])
	}
	if params.OwnerType == "" {
		params.OwnerType = domain.ConversationOwnerTypeWorkspace
	}
	if params.OwnerType == domain.ConversationOwnerTypeWorkspace && params.OwnerWorkspaceID == "" {
		params.OwnerWorkspaceID = params.WorkspaceID
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
	topicCreator := params.CreatorID
	if params.OwnerType == domain.ConversationOwnerTypeAccount {
		topicCreator = params.OwnerAccountID
	}

	row, err := qtx.CreateConversation(ctx, sqlcgen.CreateConversationParams{
		ID:               id,
		WorkspaceID:      stringToText(params.WorkspaceID),
		Name:             params.Name,
		Type:             string(params.Type),
		CreatorID:        stringToText(params.CreatorID),
		OwnerType:        string(params.OwnerType),
		OwnerAccountID:   stringToText(params.OwnerAccountID),
		OwnerWorkspaceID: stringToText(params.OwnerWorkspaceID),
		TopicValue:       params.Topic,
		TopicCreator:     topicCreator,
		PurposeValue:     params.Purpose,
		PurposeCreator:   topicCreator,
		LastMessageTs:    pgText(""),
		LastActivityTs:   pgText(""),
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("insert conversation: %w", err)
	}
	createdConv, err := qtx.GetConversation(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load created conversation: %w", err)
	}

	memberCount := 0
	if row.OwnerType == "" || row.OwnerType == string(domain.ConversationOwnerTypeWorkspace) {
		rowsAffected, err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
			ConversationID: id,
			UserID:         params.CreatorID,
		})
		if err != nil {
			return nil, fmt.Errorf("add creator as member: %w", err)
		}
		if _, err := qtx.AddConversationMemberV2ByUser(ctx, sqlcgen.AddConversationMemberV2ByUserParams{
			ConversationID: id,
			AddedByUserID:  params.CreatorID,
			UserID:         params.CreatorID,
		}); err != nil {
			return nil, fmt.Errorf("add creator as member v2: %w", err)
		}
		if rowsAffected == 1 {
			memberCount++
			if err := qtx.IncrementConversationMemberCount(ctx, id); err != nil {
				return nil, fmt.Errorf("increment member count: %w", err)
			}
		}
		for _, userID := range params.UserIDs {
			rowsAffected, err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
				ConversationID: id,
				UserID:         userID,
			})
			if err != nil {
				return nil, fmt.Errorf("add initial member: %w", err)
			}
			if _, err := qtx.AddConversationMemberV2ByUser(ctx, sqlcgen.AddConversationMemberV2ByUserParams{
				ConversationID: id,
				AddedByUserID:  params.CreatorID,
				UserID:         userID,
			}); err != nil {
				return nil, fmt.Errorf("add initial member v2: %w", err)
			}
			if rowsAffected == 1 {
				memberCount++
				if err := qtx.IncrementConversationMemberCount(ctx, id); err != nil {
					return nil, fmt.Errorf("increment member count: %w", err)
				}
			}
		}
		for _, accountID := range params.AccountIDs {
			if accountID == "" {
				continue
			}
			if err := r.addWorkspaceMemberByAccountInTx(ctx, tx, createdConv, accountID, ""); err != nil {
				if errors.Is(err, domain.ErrAlreadyInChannel) {
					continue
				}
				return nil, fmt.Errorf("add initial account member: %w", err)
			}
			memberCount++
		}
	} else {
		for _, accountID := range params.AccountIDs {
			rowsAffected, err := qtx.AddConversationMemberV2(ctx, sqlcgen.AddConversationMemberV2Params{
				ConversationID: id,
				AccountID:      accountID,
				Column3:        params.OwnerAccountID,
			})
			if err != nil {
				return nil, fmt.Errorf("add initial account member: %w", err)
			}
			if rowsAffected == 1 {
				memberCount++
				if err := qtx.IncrementConversationMemberCount(ctx, id); err != nil {
					return nil, fmt.Errorf("increment member count: %w", err)
				}
			}
		}
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}

	c := convToDomain(row)
	c.NumMembers = memberCount
	return c, nil
}

func (r *ConversationRepo) createCanonicalDM(ctx context.Context, params domain.CreateConversationParams, otherUserID string) (*domain.Conversation, error) {
	if params.OwnerType == "" {
		params.OwnerType = domain.ConversationOwnerTypeWorkspace
	}
	if params.OwnerType == domain.ConversationOwnerTypeWorkspace && params.OwnerWorkspaceID == "" {
		params.OwnerWorkspaceID = params.WorkspaceID
	}
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
		WorkspaceID: params.WorkspaceID,
		UserLowID:   userLowID,
		UserHighID:  userHighID,
	})
	if err == nil {
		return convToDomain(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get canonical dm: %w", err)
	}

	id := generateID("D")
	row, err := qtx.CreateConversation(ctx, sqlcgen.CreateConversationParams{
		ID:               id,
		WorkspaceID:      stringToText(params.WorkspaceID),
		Name:             params.Name,
		Type:             string(domain.ConversationTypeIM),
		CreatorID:        stringToText(params.CreatorID),
		OwnerType:        string(params.OwnerType),
		OwnerAccountID:   stringToText(params.OwnerAccountID),
		OwnerWorkspaceID: stringToText(params.OwnerWorkspaceID),
		TopicValue:       params.Topic,
		TopicCreator:     params.CreatorID,
		PurposeValue:     params.Purpose,
		PurposeCreator:   params.CreatorID,
		LastMessageTs:    pgText(""),
		LastActivityTs:   pgText(""),
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
		if _, err := qtx.AddConversationMemberV2ByUser(ctx, sqlcgen.AddConversationMemberV2ByUserParams{
			ConversationID: id,
			UserID:         memberID,
			AddedByUserID:  params.CreatorID,
		}); err != nil {
			return nil, fmt.Errorf("add dm member v2: %w", err)
		}
		if rowsAffected == 1 {
			if err := qtx.IncrementConversationMemberCount(ctx, id); err != nil {
				return nil, fmt.Errorf("increment dm member count: %w", err)
			}
		}
	}

	rowsAffected, err := qtx.CreateCanonicalDM(ctx, sqlcgen.CreateCanonicalDMParams{
		WorkspaceID:    params.WorkspaceID,
		UserLowID:      userLowID,
		UserHighID:     userHighID,
		ConversationID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("create canonical dm mapping: %w", err)
	}
	if rowsAffected == 0 {
		existing, err := qtx.GetCanonicalDMConversation(ctx, sqlcgen.GetCanonicalDMConversationParams{
			WorkspaceID: params.WorkspaceID,
			UserLowID:   userLowID,
			UserHighID:  userHighID,
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
		WorkspaceID: workspaceID,
		UserLowID:   userLowID,
		UserHighID:  userHighID,
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
		AccountID:       params.AccountID,
		WorkspaceID:     pgText(params.WorkspaceID),
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
	if err := r.addWorkspaceMemberInTx(ctx, tx, conv, userID); err != nil {
		return err
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
}

func (r *ConversationRepo) AddMemberByAccount(ctx context.Context, conversationID, accountID string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

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

	isAccountMember, err := r.IsAccountMember(ctx, conversationID, accountID)
	if err != nil {
		return err
	}
	if isAccountMember {
		return domain.ErrAlreadyInChannel
	}

	if conv.OwnerType == "" || conv.OwnerType == string(domain.ConversationOwnerTypeWorkspace) {
		user, err := r.ensureWorkspaceGuestUserInTx(ctx, qtx, textToString(conv.WorkspaceID), conv.ID, accountID)
		if err != nil {
			return err
		}
		if err := r.addWorkspaceMemberByAccountInTx(ctx, tx, conv, accountID, user.ID); err != nil {
			return err
		}
		if ownTx {
			return tx.Commit(ctx)
		}
		return nil
	}

	rowsAffected, err := qtx.AddConversationMemberV2(ctx, sqlcgen.AddConversationMemberV2Params{
		ConversationID: conversationID,
		AccountID:      accountID,
		Column3:        accountID,
	})
	if err != nil {
		return fmt.Errorf("add account member v2: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrAlreadyInChannel
	}
	if err := qtx.IncrementConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("increment member count: %w", err)
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
	if _, err := qtx.RemoveConversationMemberV2ByUser(ctx, sqlcgen.RemoveConversationMemberV2ByUserParams{
		ConversationID: conversationID,
		UserID:         userID,
	}); err != nil {
		return fmt.Errorf("remove member v2: %w", err)
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

func (r *ConversationRepo) RemoveMemberByAccount(ctx context.Context, conversationID, accountID string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

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

	if conv.OwnerType == "" || conv.OwnerType == string(domain.ConversationOwnerTypeWorkspace) {
		user, err := qtx.GetUserByWorkspaceAndAccount(ctx, sqlcgen.GetUserByWorkspaceAndAccountParams{
			WorkspaceID: textToString(conv.WorkspaceID),
			AccountID:   stringToText(accountID),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotInChannel
			}
			return fmt.Errorf("get workspace user by account: %w", err)
		}
		if err := r.removeWorkspaceMemberInTx(ctx, tx, conv, user.ID); err != nil {
			return err
		}
		if ownTx {
			return tx.Commit(ctx)
		}
		return nil
	}

	rowsAffected, err := qtx.RemoveConversationMemberV2(ctx, sqlcgen.RemoveConversationMemberV2Params{
		ConversationID: conversationID,
		AccountID:      accountID,
	})
	if err != nil {
		return fmt.Errorf("remove member by account: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotInChannel
	}
	if err := qtx.DecrementConversationMemberCount(ctx, conversationID); err != nil {
		return fmt.Errorf("decrement member count: %w", err)
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

	rows, err := r.q.ListConversationMembersV2(ctx, sqlcgen.ListConversationMembersV2Params{
		ConversationID: conversationID,
		AccountID:      cursor,
		Limit:          int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	members := make([]domain.ConversationMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, domain.ConversationMember{
			ConversationID: row.ConversationID,
			AccountID:      row.AccountID,
			JoinedAt:       row.CreatedAt,
		})
	}

	page := &domain.CursorPage[domain.ConversationMember]{}
	if len(members) > limit {
		page.HasMore = true
		page.NextCursor = members[limit].AccountID
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
	user, err := r.q.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("get user: %w", err)
	}
	if !user.AccountID.Valid || user.AccountID.String == "" {
		return false, nil
	}
	exists, err := r.q.IsConversationAccountMember(ctx, sqlcgen.IsConversationAccountMemberParams{
		ConversationID: conversationID,
		AccountID:      user.AccountID.String,
	})
	if err != nil {
		return false, fmt.Errorf("check account membership: %w", err)
	}
	return exists, nil
}

func (r *ConversationRepo) ListMemberAccounts(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := r.q.ListConversationMembersV2(ctx, sqlcgen.ListConversationMembersV2Params{
		ConversationID: conversationID,
		AccountID:      cursor,
		Limit:          int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list member accounts: %w", err)
	}

	members := make([]domain.ConversationMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, domain.ConversationMember{
			ConversationID: row.ConversationID,
			AccountID:      row.AccountID,
			JoinedAt:       row.CreatedAt,
		})
	}

	page := &domain.CursorPage[domain.ConversationMember]{}
	if len(members) > limit {
		page.HasMore = true
		page.NextCursor = members[limit].AccountID
		page.Items = members[:limit]
	} else {
		page.Items = members
	}
	if page.Items == nil {
		page.Items = []domain.ConversationMember{}
	}
	return page, nil
}

func (r *ConversationRepo) IsAccountMember(ctx context.Context, conversationID, accountID string) (bool, error) {
	exists, err := r.q.IsConversationAccountMember(ctx, sqlcgen.IsConversationAccountMemberParams{
		ConversationID: conversationID,
		AccountID:      accountID,
	})
	if err != nil {
		return false, fmt.Errorf("check account membership: %w", err)
	}
	return exists, nil
}

func (r *ConversationRepo) addWorkspaceMemberInTx(ctx context.Context, tx pgx.Tx, conv sqlcgen.GetConversationRow, userID string) error {
	qtx := r.q.WithTx(tx)
	if conv.Type == string(domain.ConversationTypeIM) && conv.NumMembers >= 2 {
		return domain.ErrInvalidArgument
	}

	existingMembers, err := qtx.ListConversationMembersV2(ctx, sqlcgen.ListConversationMembersV2Params{
		ConversationID: conv.ID,
		AccountID:      "",
		Limit:          2,
	})
	if err != nil {
		return fmt.Errorf("list member accounts: %w", err)
	}

	rowsAffected, err := qtx.AddConversationMember(ctx, sqlcgen.AddConversationMemberParams{
		ConversationID: conv.ID,
		UserID:         userID,
	})
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrAlreadyInChannel
	}
	if _, err := qtx.AddConversationMemberV2ByUser(ctx, sqlcgen.AddConversationMemberV2ByUserParams{
		ConversationID: conv.ID,
		AddedByUserID:  userID,
		UserID:         userID,
	}); err != nil {
		return fmt.Errorf("add member v2: %w", err)
	}
	if err := qtx.IncrementConversationMemberCount(ctx, conv.ID); err != nil {
		return fmt.Errorf("increment member count: %w", err)
	}

	if conv.Type == string(domain.ConversationTypeIM) && len(existingMembers) == 1 {
		newUser, err := qtx.GetUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("get new member user: %w", err)
		}
		if !newUser.AccountID.Valid || newUser.AccountID.String == "" {
			return domain.ErrInvalidArgument
		}
		existingUser, err := qtx.GetUserByWorkspaceAndAccount(ctx, sqlcgen.GetUserByWorkspaceAndAccountParams{
			WorkspaceID: textToString(conv.WorkspaceID),
			AccountID:   stringToText(existingMembers[0].AccountID),
		})
		if err != nil {
			return fmt.Errorf("get existing dm member user: %w", err)
		}
		userLowID, userHighID := sortPair(existingUser.ID, userID)
		mappingRows, err := qtx.CreateCanonicalDM(ctx, sqlcgen.CreateCanonicalDMParams{
			WorkspaceID:    textToString(conv.WorkspaceID),
			UserLowID:      userLowID,
			UserHighID:     userHighID,
			ConversationID: conv.ID,
		})
		if err != nil {
			return fmt.Errorf("create canonical dm mapping: %w", err)
		}
		if mappingRows == 0 {
			return domain.ErrAlreadyExists
		}
	}
	return nil
}

func (r *ConversationRepo) addWorkspaceMemberByAccountInTx(ctx context.Context, tx pgx.Tx, conv sqlcgen.GetConversationRow, accountID, userID string) error {
	qtx := r.q.WithTx(tx)
	if conv.Type == string(domain.ConversationTypeIM) && conv.NumMembers >= 2 {
		return domain.ErrInvalidArgument
	}
	if conv.Type == string(domain.ConversationTypeIM) && userID == "" {
		return domain.ErrInvalidArgument
	}

	existingMembers, err := qtx.ListConversationMembersV2(ctx, sqlcgen.ListConversationMembersV2Params{
		ConversationID: conv.ID,
		AccountID:      "",
		Limit:          2,
	})
	if err != nil {
		return fmt.Errorf("list member accounts: %w", err)
	}

	rowsAffected, err := qtx.AddConversationMemberByAccount(ctx, sqlcgen.AddConversationMemberByAccountParams{
		ID:        conv.ID,
		AccountID: stringToText(accountID),
	})
	if err != nil {
		return fmt.Errorf("add member by account: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrAlreadyInChannel
	}
	if _, err := qtx.AddConversationMemberV2(ctx, sqlcgen.AddConversationMemberV2Params{
		ConversationID: conv.ID,
		AccountID:      accountID,
		Column3:        accountID,
	}); err != nil {
		return fmt.Errorf("add member v2: %w", err)
	}
	if err := qtx.IncrementConversationMemberCount(ctx, conv.ID); err != nil {
		return fmt.Errorf("increment member count: %w", err)
	}

	if conv.Type == string(domain.ConversationTypeIM) && len(existingMembers) == 1 {
		existingUser, err := qtx.GetUserByWorkspaceAndAccount(ctx, sqlcgen.GetUserByWorkspaceAndAccountParams{
			WorkspaceID: textToString(conv.WorkspaceID),
			AccountID:   stringToText(existingMembers[0].AccountID),
		})
		if err != nil {
			return fmt.Errorf("get existing dm member user: %w", err)
		}
		userLowID, userHighID := sortPair(existingUser.ID, userID)
		mappingRows, err := qtx.CreateCanonicalDM(ctx, sqlcgen.CreateCanonicalDMParams{
			WorkspaceID:    textToString(conv.WorkspaceID),
			UserLowID:      userLowID,
			UserHighID:     userHighID,
			ConversationID: conv.ID,
		})
		if err != nil {
			return fmt.Errorf("create canonical dm mapping: %w", err)
		}
		if mappingRows == 0 {
			return domain.ErrAlreadyExists
		}
	}
	return nil
}

func (r *ConversationRepo) removeWorkspaceMemberInTx(ctx context.Context, tx pgx.Tx, conv sqlcgen.GetConversationRow, userID string) error {
	qtx := r.q.WithTx(tx)
	rowsAffected, err := qtx.RemoveConversationMember(ctx, sqlcgen.RemoveConversationMemberParams{
		ConversationID: conv.ID,
		UserID:         userID,
	})
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotInChannel
	}
	if _, err := qtx.RemoveConversationMemberV2ByUser(ctx, sqlcgen.RemoveConversationMemberV2ByUserParams{
		ConversationID: conv.ID,
		UserID:         userID,
	}); err != nil {
		return fmt.Errorf("remove member v2: %w", err)
	}
	if err := qtx.DecrementConversationMemberCount(ctx, conv.ID); err != nil {
		return fmt.Errorf("decrement member count: %w", err)
	}
	if conv.Type == string(domain.ConversationTypeIM) && conv.NumMembers == 2 {
		if _, err := qtx.DeleteCanonicalDMByConversation(ctx, conv.ID); err != nil {
			return fmt.Errorf("delete canonical dm mapping: %w", err)
		}
	}
	return nil
}

func (r *ConversationRepo) ensureWorkspaceGuestUserInTx(ctx context.Context, qtx *sqlcgen.Queries, workspaceID, conversationID, accountID string) (sqlcgen.GetUserByWorkspaceAndAccountRow, error) {
	user, err := qtx.GetUserByWorkspaceAndAccount(ctx, sqlcgen.GetUserByWorkspaceAndAccountParams{
		WorkspaceID: workspaceID,
		AccountID:   stringToText(accountID),
	})
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("get workspace user by account: %w", err)
	}

	if err := qtx.UpsertGuestWorkspaceMembership(ctx, sqlcgen.UpsertGuestWorkspaceMembershipParams{
		ID:          generateID("WM"),
		WorkspaceID: workspaceID,
		AccountID:   accountID,
	}); err != nil {
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("upsert guest workspace membership: %w", err)
	}
	membershipID, err := qtx.ProjectorGetWorkspaceMembershipByWorkspaceAndAccount(ctx, sqlcgen.ProjectorGetWorkspaceMembershipByWorkspaceAndAccountParams{
		WorkspaceID: workspaceID,
		AccountID:   accountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.GetUserByWorkspaceAndAccountRow{}, domain.ErrNotFound
		}
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("get workspace membership: %w", err)
	}
	if err := qtx.UpsertWorkspaceMembershipConversationAccess(ctx, sqlcgen.UpsertWorkspaceMembershipConversationAccessParams{
		WorkspaceMembershipID: membershipID,
		ConversationID:        conversationID,
	}); err != nil {
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("upsert workspace membership conversation access: %w", err)
	}
	if err := qtx.UpsertWorkspaceProfileFromAccount(ctx, sqlcgen.UpsertWorkspaceProfileFromAccountParams{
		WorkspaceID: workspaceID,
		AccountID:   accountID,
	}); err != nil {
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("upsert workspace profile: %w", err)
	}

	account, err := qtx.GetAccount(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.GetUserByWorkspaceAndAccountRow{}, domain.ErrNotFound
		}
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("get account: %w", err)
	}
	profileJSON, err := json.Marshal(domain.UserProfile{})
	if err != nil {
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("marshal guest user profile: %w", err)
	}
	if _, err := qtx.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:            generateID("U"),
		AccountID:     stringToText(accountID),
		WorkspaceID:   workspaceID,
		Name:          guestWorkspaceUserName(account.Email, accountID),
		RealName:      "",
		DisplayName:   "",
		Email:         account.Email,
		PrincipalType: account.PrincipalType,
		OwnerID:       "",
		IsBot:         account.IsBot,
		AccountType:   string(domain.NormalizeAccountType(domain.PrincipalType(account.PrincipalType), domain.AccountTypeMember)),
		Profile:       profileJSON,
	}); err != nil {
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("create guest workspace user: %w", err)
	}
	user, err = qtx.GetUserByWorkspaceAndAccount(ctx, sqlcgen.GetUserByWorkspaceAndAccountParams{
		WorkspaceID: workspaceID,
		AccountID:   stringToText(accountID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.GetUserByWorkspaceAndAccountRow{}, domain.ErrNotFound
		}
		return sqlcgen.GetUserByWorkspaceAndAccountRow{}, fmt.Errorf("reload guest workspace user: %w", err)
	}
	return user, nil
}

func guestWorkspaceUserName(email, accountID string) string {
	email = strings.TrimSpace(email)
	if local, _, ok := strings.Cut(email, "@"); ok && strings.TrimSpace(local) != "" {
		return strings.TrimSpace(local)
	}
	accountID = strings.TrimSpace(accountID)
	if accountID != "" {
		return "guest-" + strings.ToLower(strings.ReplaceAll(accountID, "_", "-"))
	}
	return "guest"
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
