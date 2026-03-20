package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type BookmarkRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewBookmarkRepo(pool *pgxpool.Pool) *BookmarkRepo {
	return &BookmarkRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *BookmarkRepo) Create(ctx context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error) {
	id := generateID("Bk")

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateBookmark(ctx, sqlcgen.CreateBookmarkParams{
		ID:        id,
		ChannelID: params.ChannelID,
		Title:     params.Title,
		Type:      params.Type,
		Link:      params.Link,
		Emoji:     params.Emoji,
		CreatedBy: params.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("insert bookmark: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		EventType:     domain.EventBookmarkCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return bookmarkToDomain(row), nil
}

func (r *BookmarkRepo) Get(ctx context.Context, id string) (*domain.Bookmark, error) {
	row, err := r.q.GetBookmark(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get bookmark: %w", err)
	}
	return bookmarkToDomain(row), nil
}

func (r *BookmarkRepo) Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	title := existing.Title
	if params.Title != nil {
		title = *params.Title
	}
	link := existing.Link
	if params.Link != nil {
		link = *params.Link
	}
	emoji := existing.Emoji
	if params.Emoji != nil {
		emoji = *params.Emoji
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateBookmark(ctx, sqlcgen.UpdateBookmarkParams{
		ID:        id,
		Title:     title,
		Link:      link,
		Emoji:     emoji,
		UpdatedBy: params.UpdatedBy,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update bookmark: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		EventType:     domain.EventBookmarkUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return bookmarkToDomain(row), nil
}

func (r *BookmarkRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteBookmark(ctx, id); err != nil {
		return fmt.Errorf("delete bookmark: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateBookmark,
		AggregateID:   id,
		EventType:     domain.EventBookmarkDeleted,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *BookmarkRepo) List(ctx context.Context, params domain.ListBookmarksParams) ([]domain.Bookmark, error) {
	rows, err := r.q.ListBookmarks(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks: %w", err)
	}

	bookmarks := make([]domain.Bookmark, 0, len(rows))
	for _, row := range rows {
		bookmarks = append(bookmarks, *bookmarkToDomain(row))
	}
	return bookmarks, nil
}
