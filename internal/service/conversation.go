package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// ConversationService contains business logic for conversation operations.
type ConversationService struct {
	repo     repository.ConversationRepository
	userRepo repository.UserRepository
	recorder EventRecorder
	logger   *slog.Logger
}

// NewConversationService creates a new ConversationService.
func NewConversationService(repo repository.ConversationRepository, userRepo repository.UserRepository, recorder EventRecorder, logger *slog.Logger) *ConversationService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &ConversationService{repo: repo, userRepo: userRepo, recorder: recorder, logger: logger}
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
	payload, _ := json.Marshal(conv)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventConversationCreated,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conv.ID,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record conversation.created event", "error", recErr)
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
	payload, _ := json.Marshal(conv)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventConversationUpdated,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conv.ID,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record conversation.updated event", "error", recErr)
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
	// Re-fetch to get updated state for projector.
	updatedConv, _ := s.repo.Get(ctx, id)
	payload, _ := json.Marshal(updatedConv)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventConversationArchived,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record conversation.archived event", "error", recErr)
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
	// Re-fetch to get updated state for projector.
	updatedConv, _ := s.repo.Get(ctx, id)
	payload, _ := json.Marshal(updatedConv)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventConversationUnarchived,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record conversation.unarchived event", "error", recErr)
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
	payload, _ := json.Marshal(result)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventConversationTopicSet,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		TeamID:        result.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record conversation.topic_set event", "error", recErr)
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
	payload, _ := json.Marshal(result)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventConversationPurposeSet,
		AggregateType: domain.AggregateConversation,
		AggregateID:   id,
		TeamID:        result.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record conversation.purpose_set event", "error", recErr)
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
	// Re-fetch conversation to get updated num_members for projector replay.
	updatedConv, _ := s.repo.Get(ctx, conversationID)
	payload, _ := json.Marshal(struct {
		UserID       string              `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{UserID: userID, Conversation: updatedConv})
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventMemberJoined,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record member_joined event", "error", recErr)
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
	// Re-fetch conversation to get updated num_members for projector replay.
	updatedConv, _ := s.repo.Get(ctx, conversationID)
	payload, _ := json.Marshal(struct {
		UserID       string              `json:"user_id"`
		Conversation *domain.Conversation `json:"conversation"`
	}{UserID: userID, Conversation: updatedConv})
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventMemberLeft,
		AggregateType: domain.AggregateConversation,
		AggregateID:   conversationID,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record member_left event", "error", recErr)
	}
	return nil
}

func (s *ConversationService) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListMembers(ctx, conversationID, cursor, limit)
}
