package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// ConversationService contains business logic for conversation operations.
type ConversationService struct {
	repo      repository.ConversationRepository
	userRepo  repository.UserRepository
	publisher EventPublisher
	logger    *slog.Logger
}

// NewConversationService creates a new ConversationService.
func NewConversationService(repo repository.ConversationRepository, userRepo repository.UserRepository, publisher EventPublisher, logger *slog.Logger) *ConversationService {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &ConversationService{repo: repo, userRepo: userRepo, publisher: publisher, logger: logger}
}

func (s *ConversationService) Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.Name == "" && (params.Type == domain.ConversationTypePublicChannel || params.Type == domain.ConversationTypePrivateChannel) {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	if params.CreatorID == "" {
		return nil, fmt.Errorf("creator_id: %w", domain.ErrInvalidArgument)
	}
	if params.Type == "" {
		params.Type = domain.ConversationTypePublicChannel
	}

	// Verify creator exists
	if _, err := s.userRepo.Get(ctx, params.CreatorID); err != nil {
		return nil, fmt.Errorf("creator: %w", err)
	}

	conv, err := s.repo.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, params.TeamID, domain.EventTypeChannelCreated, conv); pubErr != nil {
		s.logger.Warn("publish conversation.created event", "error", pubErr)
	}
	return conv, nil
}

func (s *ConversationService) Get(ctx context.Context, id string) (*domain.Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Get(ctx, id)
}

func (s *ConversationService) Update(ctx context.Context, id string, params domain.UpdateConversationParams) (*domain.Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, conv.TeamID, domain.EventTypeChannelRename, conv); pubErr != nil {
		s.logger.Warn("publish conversation.updated event", "error", pubErr)
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
	if err := s.repo.Archive(ctx, id); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, conv.TeamID, domain.EventTypeChannelArchive, map[string]string{"channel": id}); pubErr != nil {
		s.logger.Warn("publish conversation.archived event", "error", pubErr)
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
	if err := s.repo.Unarchive(ctx, id); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, conv.TeamID, domain.EventTypeChannelUnarchive, map[string]string{"channel": id}); pubErr != nil {
		s.logger.Warn("publish conversation.unarchived event", "error", pubErr)
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
	if conv.IsArchived {
		return nil, domain.ErrChannelArchived
	}
	result, err := s.repo.SetTopic(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, result.TeamID, domain.EventConversationTopicSet, result); pubErr != nil {
		s.logger.Warn("publish conversation.topic_set event", "error", pubErr)
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
	if conv.IsArchived {
		return nil, domain.ErrChannelArchived
	}
	result, err := s.repo.SetPurpose(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, result.TeamID, domain.EventConversationPurposeSet, result); pubErr != nil {
		s.logger.Warn("publish conversation.purpose_set event", "error", pubErr)
	}
	return result, nil
}

func (s *ConversationService) List(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}

func (s *ConversationService) Invite(ctx context.Context, conversationID, userID string) error {
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

	isMember, err := s.repo.IsMember(ctx, conversationID, userID)
	if err != nil {
		return err
	}
	if isMember {
		return domain.ErrAlreadyInChannel
	}

	if err := s.repo.AddMember(ctx, conversationID, userID); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, conv.TeamID, domain.EventTypeMemberJoinedChannel, map[string]string{"channel": conversationID, "user": userID}); pubErr != nil {
		s.logger.Warn("publish member_joined_channel event", "error", pubErr)
	}
	return nil
}

func (s *ConversationService) Kick(ctx context.Context, conversationID, userID string) error {
	if conversationID == "" || userID == "" {
		return fmt.Errorf("conversation_id and user_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.repo.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := s.repo.RemoveMember(ctx, conversationID, userID); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, conv.TeamID, domain.EventTypeMemberLeftChannel, map[string]string{"channel": conversationID, "user": userID}); pubErr != nil {
		s.logger.Warn("publish member_left_channel event", "error", pubErr)
	}
	return nil
}

func (s *ConversationService) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListMembers(ctx, conversationID, cursor, limit)
}
