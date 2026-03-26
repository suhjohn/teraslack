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

// BookmarkService contains business logic for bookmark operations.
type BookmarkService struct {
	repo     repository.BookmarkRepository
	convRepo repository.ConversationRepository
	recorder EventRecorder
	db       repository.TxBeginner
	logger   *slog.Logger
}

// NewBookmarkService creates a new BookmarkService.
func NewBookmarkService(repo repository.BookmarkRepository, convRepo repository.ConversationRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *BookmarkService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &BookmarkService{repo: repo, convRepo: convRepo, recorder: recorder, db: db, logger: logger}
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
	actorID, err := resolveActorID(ctx, params.CreatedBy)
	if err != nil {
		return nil, err
	}
	params.CreatedBy = actorID

	// Verify channel exists
	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	bm, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(bm)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventBookmarkCreated,
		AggregateType: domain.AggregateBookmark,
		AggregateID:   bm.ID,
		WorkspaceID:        conv.WorkspaceID,
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record bookmark.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return bm, nil
}

func (s *BookmarkService) Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error) {
	if id == "" {
		return nil, fmt.Errorf("bookmark_id: %w", domain.ErrInvalidArgument)
	}

	actorID, err := resolveActorID(ctx, params.UpdatedBy)
	if err != nil {
		return nil, err
	}
	params.UpdatedBy = actorID

	bm, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	conv, err := s.convRepo.Get(ctx, bm.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	bm, err = s.repo.WithTx(tx).Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(bm)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventBookmarkUpdated,
		AggregateType: domain.AggregateBookmark,
		AggregateID:   bm.ID,
		WorkspaceID:        conv.WorkspaceID,
		ActorID:       actorID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record bookmark.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return bm, nil
}

func (s *BookmarkService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("bookmark_id: %w", domain.ErrInvalidArgument)
	}

	bm, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	conv, err := s.convRepo.Get(ctx, bm.ChannelID)
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Delete(ctx, id); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{
		"id":         id,
		"channel_id": bm.ChannelID,
	})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventBookmarkDeleted,
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		WorkspaceID:        conv.WorkspaceID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record bookmark.deleted event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *BookmarkService) List(ctx context.Context, channelID string) ([]domain.Bookmark, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, domain.ListBookmarksParams{ChannelID: channelID})
}
