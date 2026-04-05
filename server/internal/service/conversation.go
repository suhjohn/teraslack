package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// ConversationService contains business logic for conversation operations.
type ConversationService struct {
	repo            repository.ConversationRepository
	userRepo        repository.UserRepository
	externalMembers repository.ExternalMemberRepository
	access          *ConversationAccessService
	recorder        EventRecorder
	db              repository.TxBeginner
	logger          *slog.Logger
}

// NewConversationService creates a new ConversationService.
func NewConversationService(repo repository.ConversationRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *ConversationService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &ConversationService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

func (s *ConversationService) SetAccessService(access *ConversationAccessService) {
	s.access = access
}

func (s *ConversationService) SetIdentityRepositories(_ ...any) {}

func (s *ConversationService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMembers = repo
}

func (s *ConversationService) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsCreate); err != nil {
		return nil, err
	}
	if !domain.IsValidConversationOwnerType(params.OwnerType) {
		return nil, fmt.Errorf("owner_type: %w", domain.ErrInvalidArgument)
	}
	if params.Name == "" && (params.Type == domain.ConversationTypePublicChannel || params.Type == domain.ConversationTypePrivateChannel) {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	actorID := actorUserID(ctx)
	if actorID == "" {
		actorID = actorAccountID(ctx)
	}
	if params.Type == "" {
		params.Type = domain.ConversationTypePublicChannel
	}

	if params.OwnerType == domain.ConversationOwnerTypeWorkspace {
		resolvedActorID, err := resolveActorID(ctx, params.CreatorID)
		if err != nil {
			return nil, err
		}
		actorID = resolvedActorID
		params.CreatorID = resolvedActorID

		creator, err := loadUser(ctx, s.userRepo, params.CreatorID)
		if err != nil {
			return nil, fmt.Errorf("creator: %w", err)
		}
		if params.WorkspaceID == "" {
			params.WorkspaceID = creator.WorkspaceID
		}
		workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
		if err != nil {
			return nil, err
		}
		params.WorkspaceID = workspaceID
		if params.OwnerWorkspaceID == "" {
			params.OwnerWorkspaceID = workspaceID
		}
		params.OwnerAccountID = ""
		if creator.WorkspaceID != params.WorkspaceID {
			return nil, fmt.Errorf("creator: %w", domain.ErrForbidden)
		}
	} else {
		if len(params.UserIDs) > 0 {
			return nil, fmt.Errorf("user_ids: %w", domain.ErrInvalidArgument)
		}
		if strings.TrimSpace(params.WorkspaceID) != "" || strings.TrimSpace(params.OwnerWorkspaceID) != "" {
			return nil, fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
		}
		if strings.TrimSpace(params.CreatorID) != "" {
			return nil, fmt.Errorf("creator_id: %w", domain.ErrInvalidArgument)
		}
		if params.OwnerAccountID == "" {
			params.OwnerAccountID = actorAccountID(ctx)
		}
		if params.OwnerAccountID == "" {
			return nil, fmt.Errorf("owner_account_id: %w", domain.ErrInvalidArgument)
		}
		params.WorkspaceID = ""
		params.OwnerWorkspaceID = ""
		params.CreatorID = ""
		params.AccountIDs = append(params.AccountIDs, params.OwnerAccountID)
	}
	if len(params.AccountIDs) > 0 {
		for _, accountID := range params.AccountIDs {
			accountID = strings.TrimSpace(accountID)
			if accountID == "" {
				return nil, fmt.Errorf("account_ids: %w", domain.ErrInvalidArgument)
			}
			if params.OwnerType == domain.ConversationOwnerTypeWorkspace {
				if _, err := s.userRepo.GetWorkspaceMembershipID(ctx, params.WorkspaceID, accountID); err != nil {
					return nil, fmt.Errorf("account_id %s: %w", accountID, err)
				}
			}
		}
		params.AccountIDs = dedupeStrings(params.AccountIDs)
	}
	params.UserIDs = dedupeStrings(params.UserIDs)
	if params.OwnerType == domain.ConversationOwnerTypeWorkspace {
		for _, userID := range params.UserIDs {
			if userID == "" {
				return nil, fmt.Errorf("user_ids: %w", domain.ErrInvalidArgument)
			}
			if userID == params.CreatorID {
				return nil, fmt.Errorf("user_ids: %w", domain.ErrInvalidArgument)
			}
			user, err := loadUser(ctx, s.userRepo, userID)
			if err != nil {
				return nil, fmt.Errorf("user_id %s: %w", userID, err)
			}
			if user.WorkspaceID != params.WorkspaceID {
				return nil, fmt.Errorf("user_id %s: %w", userID, domain.ErrForbidden)
			}
		}
	}
	if params.OwnerType == domain.ConversationOwnerTypeWorkspace && params.Type == domain.ConversationTypeIM && len(params.UserIDs) == 1 {
		conv, err := s.repo.GetCanonicalDM(ctx, params.WorkspaceID, params.CreatorID, params.UserIDs[0])
		if err == nil {
			return conv, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	conv, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(conv)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationCreated,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conv.ID,
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record conversation.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return conv, nil
}

func (s *ConversationService) Get(ctx context.Context, id string) (*domain.Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.ensureConversationVisible(ctx, conv); err != nil {
			return nil, err
		}
	} else if isAccountOwnedConversation(conv) {
		if err := ensureAccountConversationAccess(ctx, s.repo, conv); err != nil {
			return nil, err
		}
	} else if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	return conv, nil
}

func (s *ConversationService) Update(ctx context.Context, id string, params domain.UpdateConversationParams) (*domain.Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.CanManageConversation(ctx, conv); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	conv, err = txRepo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(conv)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationUpdated,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conv.ID,
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record conversation.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return conv, nil
}

func (s *ConversationService) Archive(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if s.access != nil {
		if err := s.access.CanArchiveConversation(ctx, conv); err != nil {
			return err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.Archive(ctx, id); err != nil {
		return err
	}
	updatedConv, _ := txRepo.Get(ctx, id)
	payload, _ := json.Marshal(updatedConv)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationArchived,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record conversation.archived event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *ConversationService) Unarchive(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if s.access != nil {
		if err := s.access.CanArchiveConversation(ctx, conv); err != nil {
			return err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.Unarchive(ctx, id); err != nil {
		return err
	}
	updatedConv, _ := txRepo.Get(ctx, id)
	payload, _ := json.Marshal(updatedConv)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationUnarchived,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record conversation.unarchived event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *ConversationService) SetTopic(ctx context.Context, id string, params domain.SetTopicParams) (*domain.Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.CanManageConversation(ctx, conv); err != nil {
			return nil, err
		}
	}
	if conv.IsArchived {
		return nil, domain.ErrChannelArchived
	}
	var actorID string
	if isAccountOwnedConversation(conv) {
		actorID = actorAccountID(ctx)
		if actorID == "" {
			actorID = actorUserID(ctx)
		}
		if actorID == "" {
			return nil, fmt.Errorf("set_by_id: %w", domain.ErrInvalidAuth)
		}
	} else {
		resolvedActorID, err := resolveActorID(ctx, params.SetByID)
		if err != nil {
			return nil, err
		}
		actorID = resolvedActorID
	}
	params.SetByID = actorID

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := s.repo.WithTx(tx).SetTopic(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(result)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationTopicSet,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		WorkspaceID:   conversationWorkspaceID(result),
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record conversation.topic_set event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return result, nil
}

func (s *ConversationService) SetPurpose(ctx context.Context, id string, params domain.SetPurposeParams) (*domain.Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.CanManageConversation(ctx, conv); err != nil {
			return nil, err
		}
	}
	if conv.IsArchived {
		return nil, domain.ErrChannelArchived
	}
	var actorID string
	if isAccountOwnedConversation(conv) {
		actorID = actorAccountID(ctx)
		if actorID == "" {
			actorID = actorUserID(ctx)
		}
		if actorID == "" {
			return nil, fmt.Errorf("set_by_id: %w", domain.ErrInvalidAuth)
		}
	} else {
		resolvedActorID, err := resolveActorID(ctx, params.SetByID)
		if err != nil {
			return nil, err
		}
		actorID = resolvedActorID
	}
	params.SetByID = actorID

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := s.repo.WithTx(tx).SetPurpose(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(result)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationPurposeSet,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		WorkspaceID:   conversationWorkspaceID(result),
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record conversation.purpose_set event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return result, nil
}

func (s *ConversationService) List(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	workspaceID, _, err := s.resolveListWorkspace(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	params.AccountID = actorAccountID(ctx)
	if params.AccountID == "" {
		if actorID := actorUserID(ctx); actorID != "" {
			if actor, err := loadUser(ctx, s.userRepo, actorID); err == nil {
				params.AccountID = actor.AccountID
			}
		}
	}
	params.UserID = actorUserID(ctx)
	page, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	return page, nil
}

func (s *ConversationService) resolveListWorkspace(ctx context.Context, requested string) (string, bool, error) {
	ctxWorkspace := ctxutil.GetWorkspaceID(ctx)
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if ctxWorkspace != "" {
			return ctxWorkspace, false, nil
		}
		return "", false, nil
	}
	if ctxWorkspace == "" || requested == ctxWorkspace {
		if ctxWorkspace == "" {
			if !requiresAuthenticatedActor(ctx) {
				return requested, false, nil
			}
			accountID := actorAccountID(ctx)
			if accountID == "" {
				return "", false, domain.ErrForbidden
			}
			if _, err := s.userRepo.GetWorkspaceMembershipID(ctx, requested, accountID); err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					return "", false, domain.ErrForbidden
				}
				return "", false, err
			}
		}
		return requested, false, nil
	}
	accountID := actorAccountID(ctx)
	if accountID == "" {
		return "", false, domain.ErrForbidden
	}
	if _, err := s.userRepo.GetWorkspaceMembershipID(ctx, requested, accountID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", false, domain.ErrForbidden
		}
		return "", false, err
	}
	return requested, true, nil
}

func (s *ConversationService) listExternalMemberConversations(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	if s.externalMembers == nil {
		return nil, domain.ErrForbidden
	}
	items, err := s.externalMembers.ListActiveByAccountAndWorkspace(ctx, ctxutil.GetAccountID(ctx), params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	conversations := make([]domain.Conversation, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.ConversationID]; ok {
			continue
		}
		conv, err := s.repo.Get(ctx, item.ConversationID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if conversationWorkspaceID(conv) != params.WorkspaceID {
			continue
		}
		if params.ExcludeArchived && conv.IsArchived {
			continue
		}
		if len(params.Types) > 0 && !conversationTypeAllowed(conv.Type, params.Types) {
			continue
		}
		if params.Cursor != "" && conv.ID <= params.Cursor {
			continue
		}
		conversations = append(conversations, *conv)
		seen[item.ConversationID] = struct{}{}
	}
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].ID < conversations[j].ID
	})
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
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

func conversationTypeAllowed(target domain.ConversationType, allowed []domain.ConversationType) bool {
	for _, candidate := range allowed {
		if candidate == target {
			return true
		}
	}
	return false
}

func (s *ConversationService) resolveInviteSubject(ctx context.Context, workspaceID, rawMemberID string) (string, string) {
	if workspaceID == "" {
		return "", rawMemberID
	}

	if user, err := s.userRepo.Get(ctx, rawMemberID); err == nil {
		if user.AccountID != "" {
			return user.ID, user.AccountID
		}
		return "", ""
	}
	return "", rawMemberID
}

func (s *ConversationService) Invite(ctx context.Context, conversationID, userID string) error {
	if err := requirePermission(ctx, domain.PermissionConversationsMembersWrite); err != nil {
		return err
	}
	if conversationID == "" || userID == "" {
		return fmt.Errorf("conversation_id and user_id: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.repo.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	if conv.IsArchived {
		return domain.ErrChannelArchived
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if s.access != nil {
		if err := s.access.CanManageMembers(ctx, conv); err != nil {
			return err
		}
	}

	rawMemberID := strings.TrimSpace(userID)
	memberUserID, accountID := s.resolveInviteSubject(ctx, conversationWorkspaceID(conv), rawMemberID)
	if accountID == "" {
		return fmt.Errorf("account_id: %w", domain.ErrInvalidArgument)
	}
	isMember, err := s.repo.IsAccountMember(ctx, conversationID, accountID)
	if err != nil {
		return err
	}
	if isMember {
		return domain.ErrAlreadyInChannel
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.AddMemberByAccount(ctx, conversationID, accountID); err != nil {
		return err
	}
	updatedConv, _ := txRepo.Get(ctx, conversationID)
	payload, _ := json.Marshal(struct {
		UserID       string               `json:"user_id,omitempty"`
		AccountID    string               `json:"account_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{UserID: memberUserID, AccountID: accountID, Conversation: updatedConv})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMemberJoined,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record member_joined event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *ConversationService) InviteAccount(ctx context.Context, conversationID, accountID string) error {
	if conversationID == "" || accountID == "" {
		return fmt.Errorf("conversation_id and account_id: %w", domain.ErrInvalidArgument)
	}
	return s.Invite(ctx, conversationID, accountID)
}

func (s *ConversationService) Kick(ctx context.Context, conversationID, userID string) error {
	if err := requirePermission(ctx, domain.PermissionConversationsMembersWrite); err != nil {
		return err
	}
	if conversationID == "" || userID == "" {
		return fmt.Errorf("conversation_id and user_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if s.access != nil {
		if err := s.access.CanManageMembers(ctx, conv); err != nil {
			return err
		}
	}

	rawMemberID := strings.TrimSpace(userID)
	memberUserID, accountID := s.resolveInviteSubject(ctx, conversationWorkspaceID(conv), rawMemberID)
	if accountID == "" {
		return fmt.Errorf("account_id: %w", domain.ErrInvalidArgument)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.RemoveMemberByAccount(ctx, conversationID, accountID); err != nil {
		return err
	}
	updatedConv, _ := txRepo.Get(ctx, conversationID)
	payload, _ := json.Marshal(struct {
		UserID       string               `json:"user_id,omitempty"`
		AccountID    string               `json:"account_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{UserID: memberUserID, AccountID: accountID, Conversation: updatedConv})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMemberLeft,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record member_left event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *ConversationService) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.ensureConversationVisible(ctx, conv); err != nil {
			return nil, err
		}
	}
	return s.repo.ListMemberAccounts(ctx, conversationID, cursor, limit)
}
