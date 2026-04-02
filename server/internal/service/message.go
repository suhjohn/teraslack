package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// MessageService contains business logic for message operations.
type MessageService struct {
	repo            repository.MessageRepository
	convRepo        repository.ConversationRepository
	access          *ConversationAccessService
	externalMembers repository.ExternalMemberRepository
	recorder        EventRecorder
	db              repository.TxBeginner
	logger          *slog.Logger
}

// NewMessageService creates a new MessageService.
func NewMessageService(repo repository.MessageRepository, convRepo repository.ConversationRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *MessageService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &MessageService{repo: repo, convRepo: convRepo, recorder: recorder, db: db, logger: logger}
}

func (s *MessageService) SetAccessService(access *ConversationAccessService) {
	s.access = access
}

func (s *MessageService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMembers = repo
}

func (s *MessageService) PostMessage(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error) {
	if err := requirePermission(ctx, domain.PermissionMessagesWrite); err != nil {
		return nil, err
	}
	if params.ChannelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}
	if params.UserID == "" {
		params.UserID = actorUserID(ctx)
	}
	if params.UserID == "" {
		return nil, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	if params.Text == "" && len(params.Blocks) == 0 {
		return nil, fmt.Errorf("text or blocks: %w", domain.ErrInvalidArgument)
	}

	// Verify conversation exists and is not archived
	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if conv.IsArchived {
		return nil, domain.ErrChannelArchived
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesWrite, true); err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv, params.UserID); err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.CanPost(ctx, conv, params.UserID); err != nil {
			return nil, err
		}
	}

	// If replying to a thread, verify parent message exists
	if params.ThreadTS != "" {
		if _, err := s.repo.GetRow(ctx, params.ChannelID, params.ThreadTS); err != nil {
			return nil, fmt.Errorf("parent message: %w", err)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	msg, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(msg)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMessagePosted,
		AggregateType: domain.AggregateMessage,
		AggregateID:   msg.TS,
		WorkspaceID:   conv.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record message.posted event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return msg, nil
}

func (s *MessageService) GetMessage(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	if err := requirePermission(ctx, domain.PermissionMessagesRead); err != nil {
		return nil, err
	}
	if channelID == "" || ts == "" {
		return nil, fmt.Errorf("channel_id and ts: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.convRepo.Get(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesRead, false); err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}

	return s.repo.Get(ctx, channelID, ts)
}

func (s *MessageService) UpdateMessage(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error) {
	if err := requirePermission(ctx, domain.PermissionMessagesWrite); err != nil {
		return nil, err
	}
	if channelID == "" || ts == "" {
		return nil, fmt.Errorf("channel_id and ts: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.convRepo.Get(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesWrite, true); err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetRow(ctx, channelID, ts)
	if err != nil {
		return nil, fmt.Errorf("message: %w", err)
	}
	if err := ensureMessageEditor(ctx, existing); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	msg, err := s.repo.WithTx(tx).Update(ctx, channelID, ts, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(msg)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMessageUpdated,
		AggregateType: domain.AggregateMessage,
		AggregateID:   msg.TS,
		WorkspaceID:   conv.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record message.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return msg, nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, channelID, ts string) error {
	if err := requirePermission(ctx, domain.PermissionMessagesWrite); err != nil {
		return err
	}
	if channelID == "" || ts == "" {
		return fmt.Errorf("channel_id and ts: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.convRepo.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesWrite, true); err != nil {
		return err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return err
	}

	msg, err := s.repo.GetRow(ctx, channelID, ts)
	if err != nil {
		return fmt.Errorf("message: %w", err)
	}
	if err := ensureMessageDeleter(ctx, msg); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Delete(ctx, channelID, ts); err != nil {
		return err
	}
	payload, _ := json.Marshal(msg)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventMessageDeleted,
		AggregateType: domain.AggregateMessage,
		AggregateID:   ts,
		WorkspaceID:   conv.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record message.deleted event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *MessageService) History(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error) {
	if err := requirePermission(ctx, domain.PermissionMessagesRead); err != nil {
		return nil, err
	}
	if params.ChannelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesRead, false); err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}

	return s.repo.ListHistory(ctx, params)
}

func (s *MessageService) Replies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error) {
	if err := requirePermission(ctx, domain.PermissionMessagesRead); err != nil {
		return nil, err
	}
	if params.ChannelID == "" || params.ThreadTS == "" {
		return nil, fmt.Errorf("channel_id and thread_ts: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesRead, false); err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}

	return s.repo.ListReplies(ctx, params)
}

func (s *MessageService) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	if params.ChannelID == "" || params.MessageTS == "" || params.UserID == "" || params.Emoji == "" {
		return fmt.Errorf("channel_id, message_ts, user_id, and emoji: %w", domain.ErrInvalidArgument)
	}
	actorID, err := resolveActorID(ctx, params.UserID)
	if err != nil {
		return err
	}
	params.UserID = actorID
	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesWrite, true); err != nil {
		return err
	}
	if err := s.ensureConversationVisible(ctx, conv, params.UserID); err != nil {
		return err
	}
	// Verify message exists
	if _, err := s.repo.GetRow(ctx, params.ChannelID, params.MessageTS); err != nil {
		return fmt.Errorf("message: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).AddReaction(ctx, params); err != nil {
		if errors.Is(err, domain.ErrAlreadyReacted) {
			return nil
		}
		return err
	}
	payload, _ := json.Marshal(struct {
		Reaction domain.AddReactionParams `json:"reaction"`
	}{Reaction: params})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventReactionAdded,
		AggregateType: domain.AggregateMessage,
		AggregateID:   params.MessageTS,
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record reaction.added event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *MessageService) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	if params.ChannelID == "" || params.MessageTS == "" || params.UserID == "" || params.Emoji == "" {
		return fmt.Errorf("channel_id, message_ts, user_id, and emoji: %w", domain.ErrInvalidArgument)
	}
	actorID, err := resolveActorID(ctx, params.UserID)
	if err != nil {
		return err
	}
	params.UserID = actorID
	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesWrite, true); err != nil {
		return err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).RemoveReaction(ctx, params); err != nil {
		if errors.Is(err, domain.ErrNoReaction) {
			return nil
		}
		return err
	}
	payload, _ := json.Marshal(struct {
		Reaction domain.RemoveReactionParams `json:"reaction"`
	}{Reaction: params})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventReactionRemoved,
		AggregateType: domain.AggregateMessage,
		AggregateID:   params.MessageTS,
		WorkspaceID:   conv.WorkspaceID,
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record reaction.removed event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *MessageService) GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error) {
	if channelID == "" || messageTS == "" {
		return nil, fmt.Errorf("channel_id and message_ts: %w", domain.ErrInvalidArgument)
	}

	conv, err := s.convRepo.Get(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if _, err := ensureConversationAccess(ctx, s.externalMembers, conv, domain.PermissionMessagesRead, false); err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}

	return s.repo.GetReactions(ctx, channelID, messageTS)
}

func (s *MessageService) ensureConversationVisible(ctx context.Context, conv *domain.Conversation, fallbackActorIDs ...string) error {
	if conv == nil {
		return domain.ErrNotFound
	}
	externalActor, err := isConversationExternalActor(ctx, s.externalMembers, conv)
	if err != nil {
		return err
	}
	if !externalActor {
		if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
			return err
		}
	}

	switch conv.Type {
	case domain.ConversationTypePrivateChannel, domain.ConversationTypeIM, domain.ConversationTypeMPIM:
		if contextIsWorkspaceAdmin(ctx) {
			return nil
		}
		if externalActor {
			return nil
		}
		actorID := actorUserID(ctx)
		if actorID == "" {
			for _, fallbackActorID := range fallbackActorIDs {
				if fallbackActorID != "" {
					actorID = fallbackActorID
					break
				}
			}
		}
		if actorID == "" {
			return domain.ErrForbidden
		}
		isMember, err := s.convRepo.IsMember(ctx, conv.ID, actorID)
		if err != nil {
			return err
		}
		if !isMember {
			return domain.ErrForbidden
		}
	}

	return nil
}

func ensureMessageEditor(ctx context.Context, msg *domain.Message) error {
	if msg == nil {
		return domain.ErrNotFound
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actorUserID(ctx) == msg.UserID {
		return nil
	}
	if contextIsWorkspaceAdmin(ctx) {
		return nil
	}
	return domain.ErrForbidden
}

func ensureMessageDeleter(ctx context.Context, msg *domain.Message) error {
	if msg == nil {
		return domain.ErrNotFound
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actorUserID(ctx) == msg.UserID {
		return nil
	}
	if contextIsWorkspaceAdmin(ctx) {
		return nil
	}
	return domain.ErrForbidden
}
