package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// BookmarkService contains business logic for bookmark operations.
type BookmarkService struct {
	repo      repository.BookmarkRepository
	convRepo  repository.ConversationRepository
	publisher EventPublisher
	logger    *slog.Logger
}

// NewBookmarkService creates a new BookmarkService.
func NewBookmarkService(repo repository.BookmarkRepository, convRepo repository.ConversationRepository, publisher EventPublisher, logger *slog.Logger) *BookmarkService {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &BookmarkService{repo: repo, convRepo: convRepo, publisher: publisher, logger: logger}
}

func (s *BookmarkService) Create(ctx context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error) {
	if params.ChannelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}
	if params.Title == "" {
		return nil, fmt.Errorf("title: %w", domain.ErrInvalidArgument)
	}
	if params.Link == "" {
		return nil, fmt.Errorf("link: %w", domain.ErrInvalidArgument)
	}
	if params.Type == "" {
		params.Type = "link"
	}

	// Verify channel exists
	if _, err := s.convRepo.Get(ctx, params.ChannelID); err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}

	bm, err := s.repo.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventBookmarkCreated, bm); pubErr != nil {
		s.logger.Warn("publish bookmark.created event", "error", pubErr)
	}
	return bm, nil
}

func (s *BookmarkService) Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error) {
	if id == "" {
		return nil, fmt.Errorf("bookmark_id: %w", domain.ErrInvalidArgument)
	}
	bm, err := s.repo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventBookmarkUpdated, bm); pubErr != nil {
		s.logger.Warn("publish bookmark.updated event", "error", pubErr)
	}
	return bm, nil
}

func (s *BookmarkService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("bookmark_id: %w", domain.ErrInvalidArgument)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if pubErr := s.publisher.Publish(ctx, "", domain.EventBookmarkDeleted, map[string]string{"bookmark_id": id}); pubErr != nil {
		s.logger.Warn("publish bookmark.deleted event", "error", pubErr)
	}
	return nil
}

func (s *BookmarkService) List(ctx context.Context, channelID string) ([]domain.Bookmark, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, domain.ListBookmarksParams{ChannelID: channelID})
}
