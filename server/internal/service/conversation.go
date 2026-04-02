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
	membershipRepo  repository.WorkspaceMembershipRepository
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

func (s *ConversationService) SetIdentityRepositories(membershipRepo repository.WorkspaceMembershipRepository) {
	s.membershipRepo = membershipRepo
}

func (s *ConversationService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMembers = repo
}

func (s *ConversationService) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsCreate); err != nil {
		return nil, err
	}
	workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	if params.Name == "" && (params.Type == domain.ConversationTypePublicChannel || params.Type == domain.ConversationTypePrivateChannel) {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	actorID, err := resolveActorID(ctx, params.CreatorID)
	if err != nil {
		return nil, err
	}
	params.CreatorID = actorID
	if params.Type == "" {
		params.Type = domain.ConversationTypePublicChannel
	}

	// Verify creator exists
	creator, err := loadUserWithMembership(ctx, s.userRepo, s.membershipRepo, params.CreatorID)
	if err != nil {
		return nil, fmt.Errorf("creator: %w", err)
	}
	if creator.WorkspaceID != params.WorkspaceID {
		return nil, fmt.Errorf("creator: %w", domain.ErrForbidden)
	}
	for _, userID := range params.UserIDs {
		if userID == "" {
			return nil, fmt.Errorf("user_ids: %w", domain.ErrInvalidArgument)
		}
		if userID == params.CreatorID {
			return nil, fmt.Errorf("user_ids: %w", domain.ErrInvalidArgument)
		}
		user, err := loadUserWithMembership(ctx, s.userRepo, s.membershipRepo, userID)
		if err != nil {
			return nil, fmt.Errorf("user_id %s: %w", userID, err)
		}
		if user.WorkspaceID != params.WorkspaceID {
			return nil, fmt.Errorf("user_id %s: %w", userID, domain.ErrForbidden)
		}
	}
	if params.Type == domain.ConversationTypeIM && len(params.UserIDs) == 1 {
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
		WorkspaceID:   conv.WorkspaceID,
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
	externalActor, err := ensureConversationAccess(ctx, s.externalMembers, conv, "", false)
	if err != nil {
		return nil, err
	}
	if s.access != nil && !externalActor {
		if err := s.access.ensureConversationVisible(ctx, conv); err != nil {
			return nil, err
		}
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
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
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
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
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
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
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
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
	actorID, err := resolveActorID(ctx, params.SetByID)
	if err != nil {
		return nil, err
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
		WorkspaceID:   result.WorkspaceID,
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
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
	actorID, err := resolveActorID(ctx, params.SetByID)
	if err != nil {
		return nil, err
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
		WorkspaceID:   result.WorkspaceID,
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
	workspaceID, externalList, err := s.resolveListWorkspace(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	if externalList {
		return s.listExternalMemberConversations(ctx, params)
	}
	params.UserID = compatibilityActorID(ctx)
	page, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	filtered, err := filterExternalSharedConversations(ctx, s.externalMembers, page.Items)
	if err != nil {
		return nil, err
	}
	if len(filtered) == len(page.Items) {
		return page, nil
	}
	page.Items = filtered
	page.HasMore = false
	page.NextCursor = ""
	return page, nil
}

func (s *ConversationService) resolveListWorkspace(ctx context.Context, requested string) (string, bool, error) {
	ctxWorkspace := ctxutil.GetWorkspaceID(ctx)
	requested = strings.TrimSpace(requested)
	if requested == "" {
		workspaceID, err := resolveWorkspaceID(ctx, requested)
		return workspaceID, false, err
	}
	if ctxWorkspace == "" || requested == ctxWorkspace {
		workspaceID, err := resolveWorkspaceID(ctx, requested)
		return workspaceID, false, err
	}
	if s.externalMembers == nil || ctxutil.GetAccountID(ctx) == "" {
		return "", false, domain.ErrForbidden
	}
	items, err := s.externalMembers.ListActiveByAccountAndWorkspace(ctx, ctxutil.GetAccountID(ctx), requested)
	if err != nil {
		return "", false, err
	}
	if len(items) == 0 {
		return "", false, domain.ErrForbidden
	}
	return requested, true, nil
}

func (s *ConversationService) listExternalMemberConversations(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
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
		if conv.WorkspaceID != params.WorkspaceID {
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}
	if s.access != nil {
		if err := s.access.CanManageMembers(ctx, conv); err != nil {
			return err
		}
	}

	isMember, err := s.repo.IsMember(ctx, conversationID, userID)
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
	if err := txRepo.AddMember(ctx, conversationID, userID); err != nil {
		return err
	}
	updatedConv, _ := txRepo.Get(ctx, conversationID)
	payload, _ := json.Marshal(struct {
		UserID       string               `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{UserID: userID, Conversation: updatedConv})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMemberJoined,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record member_joined event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}
	if s.access != nil {
		if err := s.access.CanManageMembers(ctx, conv); err != nil {
			return err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.RemoveMember(ctx, conversationID, userID); err != nil {
		return err
	}
	updatedConv, _ := txRepo.Get(ctx, conversationID)
	payload, _ := json.Marshal(struct {
		UserID       string               `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{UserID: userID, Conversation: updatedConv})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMemberLeft,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
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
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.ensureConversationVisible(ctx, conv); err != nil {
			return nil, err
		}
	}
	return s.repo.ListMembers(ctx, conversationID, cursor, limit)
}
