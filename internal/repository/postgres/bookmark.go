package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// BookmarkRepo implements repository.BookmarkRepository using Postgres.
type BookmarkRepo struct {
	pool *pgxpool.Pool
}

// NewBookmarkRepo creates a new BookmarkRepo.
func NewBookmarkRepo(pool *pgxpool.Pool) *BookmarkRepo {
	return &BookmarkRepo{pool: pool}
}

func (r *BookmarkRepo) Create(ctx context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error) {
	id := generateID("Bk")

	var b domain.Bookmark
	err := r.pool.QueryRow(ctx, `
		INSERT INTO bookmarks (id, channel_id, title, type, link, emoji, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at`,
		id, params.ChannelID, params.Title, params.Type, params.Link, params.Emoji, params.CreatedBy,
	).Scan(
		&b.ID, &b.ChannelID, &b.Title, &b.Type, &b.Link, &b.Emoji,
		&b.CreatedBy, &b.UpdatedBy, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert bookmark: %w", err)
	}
	return &b, nil
}

func (r *BookmarkRepo) Get(ctx context.Context, id string) (*domain.Bookmark, error) {
	var b domain.Bookmark
	err := r.pool.QueryRow(ctx, `
		SELECT id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at
		FROM bookmarks WHERE id = $1`, id,
	).Scan(
		&b.ID, &b.ChannelID, &b.Title, &b.Type, &b.Link, &b.Emoji,
		&b.CreatedBy, &b.UpdatedBy, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get bookmark: %w", err)
	}
	return &b, nil
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

	var b domain.Bookmark
	err = r.pool.QueryRow(ctx, `
		UPDATE bookmarks SET title = $2, link = $3, emoji = $4, updated_by = $5
		WHERE id = $1
		RETURNING id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at`,
		id, title, link, emoji, params.UpdatedBy,
	).Scan(
		&b.ID, &b.ChannelID, &b.Title, &b.Type, &b.Link, &b.Emoji,
		&b.CreatedBy, &b.UpdatedBy, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update bookmark: %w", err)
	}
	return &b, nil
}

func (r *BookmarkRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM bookmarks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete bookmark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *BookmarkRepo) List(ctx context.Context, params domain.ListBookmarksParams) ([]domain.Bookmark, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at
		FROM bookmarks WHERE channel_id = $1
		ORDER BY created_at ASC`, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks: %w", err)
	}
	defer rows.Close()

	var bookmarks []domain.Bookmark
	for rows.Next() {
		var b domain.Bookmark
		if err := rows.Scan(
			&b.ID, &b.ChannelID, &b.Title, &b.Type, &b.Link, &b.Emoji,
			&b.CreatedBy, &b.UpdatedBy, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan bookmark: %w", err)
		}
		bookmarks = append(bookmarks, b)
	}
	if bookmarks == nil {
		bookmarks = []domain.Bookmark{}
	}
	return bookmarks, nil
}
