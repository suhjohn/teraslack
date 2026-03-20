package service

import (
	"context"
	"fmt"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// BookmarkService contains business logic for bookmark operations.
type BookmarkService struct {
	repo     repository.BookmarkRepository
	convRepo repository.ConversationRepository
}

// NewBookmarkService creates a new BookmarkService.
func NewBookmarkService(repo repository.BookmarkRepository, convRepo repository.ConversationRepository) *BookmarkService {
	return &BookmarkService{repo: repo, convRepo: convRepo}
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

	return s.repo.Create(ctx, params)
}

func (s *BookmarkService) Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error) {
	if id == "" {
		return nil, fmt.Errorf("bookmark_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Update(ctx, id, params)
}

func (s *BookmarkService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("bookmark_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Delete(ctx, id)
}

func (s *BookmarkService) List(ctx context.Context, channelID string) ([]domain.Bookmark, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, domain.ListBookmarksParams{ChannelID: channelID})
}
