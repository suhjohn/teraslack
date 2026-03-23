package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// ConversationService contains business logic for conversation operations.
type ConversationService struct {
	repo           repository.ConversationRepository
	userRepo       repository.UserRepository
	access         *ConversationAccessService
	externalAccess repository.ExternalPrincipalAccessRepository
	recorder       EventRecorder
	db             repository.TxBeginner
	logger         *slog.Logger
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

func (s *ConversationService) SetExternalAccessRepository(repo repository.ExternalPrincipalAccessRepository) {
	s.externalAccess = repo
}

func (s *ConversationService) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsCreate); err != nil {
		return nil, err
	}
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
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
	if _, err := s.userRepo.Get(ctx, params.CreatorID); err != nil {
		return nil, fmt.Errorf("creator: %w", err)
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
		TeamID:        conv.TeamID,
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return nil, err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.read", false); err != nil {
		return nil, err
	}
	if s.access != nil {
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return nil, err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.update", true); err != nil {
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
		TeamID:        conv.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.archive", true); err != nil {
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
		TeamID:        conv.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.archive", true); err != nil {
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
		TeamID:        conv.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return nil, err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.update", true); err != nil {
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
		TeamID:        result.TeamID,
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return nil, err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.update", true); err != nil {
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
		TeamID:        result.TeamID,
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
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	page, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	filtered, err := filterExternalSharedConversations(ctx, s.externalAccess, page.Items)
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, domain.PermissionConversationsMembersWrite, true); err != nil {
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
		TeamID:        conv.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, domain.PermissionConversationsMembersWrite, true); err != nil {
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
		TeamID:        conv.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
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
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return nil, err
	}
	if err := ensureExternalSharedConversationAccess(ctx, s.externalAccess, conv, "conversations.members.read", false); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.ensureConversationVisible(ctx, conv); err != nil {
			return nil, err
		}
	}
	return s.repo.ListMembers(ctx, conversationID, cursor, limit)
}
