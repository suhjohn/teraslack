package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// MessageService contains business logic for message operations.
type MessageService struct {
	repo      repository.MessageRepository
	convRepo  repository.ConversationRepository
	publisher EventPublisher
	logger    *slog.Logger
}

// NewMessageService creates a new MessageService.
func NewMessageService(repo repository.MessageRepository, convRepo repository.ConversationRepository, publisher EventPublisher, logger *slog.Logger) *MessageService {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &MessageService{repo: repo, convRepo: convRepo, publisher: publisher, logger: logger}
}

func (s *MessageService) PostMessage(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error) {
	if params.ChannelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
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

	// If replying to a thread, verify parent message exists
	if params.ThreadTS != "" {
		if _, err := s.repo.Get(ctx, params.ChannelID, params.ThreadTS); err != nil {
			return nil, fmt.Errorf("parent message: %w", err)
		}
	}

	msg, err := s.repo.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, conv.TeamID, domain.EventTypeMessage, msg); pubErr != nil {
		s.logger.Warn("publish message.posted event", "error", pubErr)
	}
	return msg, nil
}

func (s *MessageService) GetMessage(ctx context.Context, channelID, ts string) (*domain.Message, error) {
	if channelID == "" || ts == "" {
		return nil, fmt.Errorf("channel_id and ts: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Get(ctx, channelID, ts)
}

func (s *MessageService) UpdateMessage(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error) {
	if channelID == "" || ts == "" {
		return nil, fmt.Errorf("channel_id and ts: %w", domain.ErrInvalidArgument)
	}
	msg, err := s.repo.Update(ctx, channelID, ts, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventTypeMessage, msg); pubErr != nil {
		s.logger.Warn("publish message.updated event", "error", pubErr)
	}
	return msg, nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, channelID, ts string) error {
	if channelID == "" || ts == "" {
		return fmt.Errorf("channel_id and ts: %w", domain.ErrInvalidArgument)
	}
	if err := s.repo.Delete(ctx, channelID, ts); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventTypeMessage, map[string]string{"channel": channelID, "ts": ts, "subtype": "message_deleted"}); pubErr != nil {
		s.logger.Warn("publish message.deleted event", "error", pubErr)
	}
	return nil
}

func (s *MessageService) History(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error) {
	if params.ChannelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListHistory(ctx, params)
}

func (s *MessageService) Replies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error) {
	if params.ChannelID == "" || params.ThreadTS == "" {
		return nil, fmt.Errorf("channel_id and thread_ts: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListReplies(ctx, params)
}

func (s *MessageService) AddReaction(ctx context.Context, params domain.AddReactionParams) error {
	if params.ChannelID == "" || params.MessageTS == "" || params.UserID == "" || params.Emoji == "" {
		return fmt.Errorf("channel_id, message_ts, user_id, and emoji: %w", domain.ErrInvalidArgument)
	}
	// Verify message exists
	if _, err := s.repo.Get(ctx, params.ChannelID, params.MessageTS); err != nil {
		return fmt.Errorf("message: %w", err)
	}
	if err := s.repo.AddReaction(ctx, params); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventTypeReactionAdded, params); pubErr != nil {
		s.logger.Warn("publish reaction.added event", "error", pubErr)
	}
	return nil
}

func (s *MessageService) RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error {
	if params.ChannelID == "" || params.MessageTS == "" || params.UserID == "" || params.Emoji == "" {
		return fmt.Errorf("channel_id, message_ts, user_id, and emoji: %w", domain.ErrInvalidArgument)
	}
	if err := s.repo.RemoveReaction(ctx, params); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventTypeReactionRemoved, params); pubErr != nil {
		s.logger.Warn("publish reaction.removed event", "error", pubErr)
	}
	return nil
}

func (s *MessageService) GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error) {
	if channelID == "" || messageTS == "" {
		return nil, fmt.Errorf("channel_id and message_ts: %w", domain.ErrInvalidArgument)
	}
	return s.repo.GetReactions(ctx, channelID, messageTS)
}
