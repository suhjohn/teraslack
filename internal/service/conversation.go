package service

import (
	"context"
	"fmt"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// ConversationService contains business logic for conversation operations.
type ConversationService struct {
	repo     repository.ConversationRepository
	userRepo repository.UserRepository
}

// NewConversationService creates a new ConversationService.
func NewConversationService(repo repository.ConversationRepository, userRepo repository.UserRepository) *ConversationService {
	return &ConversationService{repo: repo, userRepo: userRepo}
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

	return s.repo.Create(ctx, params)
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
	return s.repo.Update(ctx, id, params)
}

func (s *ConversationService) Archive(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Archive(ctx, id)
}

func (s *ConversationService) Unarchive(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Unarchive(ctx, id)
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
	return s.repo.SetTopic(ctx, id, params)
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
	return s.repo.SetPurpose(ctx, id, params)
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

	return s.repo.AddMember(ctx, conversationID, userID)
}

func (s *ConversationService) Kick(ctx context.Context, conversationID, userID string) error {
	if conversationID == "" || userID == "" {
		return fmt.Errorf("conversation_id and user_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.RemoveMember(ctx, conversationID, userID)
}

func (s *ConversationService) ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListMembers(ctx, conversationID, cursor, limit)
}
