package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// FileRepo implements repository.FileRepository using Postgres.
type FileRepo struct {
	pool *pgxpool.Pool
}

// NewFileRepo creates a new FileRepo.
func NewFileRepo(pool *pgxpool.Pool) *FileRepo {
	return &FileRepo{pool: pool}
}

func (r *FileRepo) Create(ctx context.Context, f *domain.File) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO files (id, name, title, mimetype, filetype, size, user_id, s3_key,
		                   url_private, url_private_download, permalink, is_external, external_url, upload_complete)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		f.ID, f.Name, f.Title, f.Mimetype, f.Filetype, f.Size, f.UserID, "",
		f.URLPrivate, f.URLPrivateDownload, f.Permalink, f.IsExternal, f.ExternalURL, false,
	)
	if err != nil {
		return fmt.Errorf("insert file: %w", err)
	}
	return nil
}

func (r *FileRepo) Get(ctx context.Context, id string) (*domain.File, error) {
	var f domain.File
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, title, mimetype, filetype, size, user_id,
		       url_private, url_private_download, permalink, is_external, external_url,
		       created_at, updated_at
		FROM files WHERE id = $1`, id,
	).Scan(
		&f.ID, &f.Name, &f.Title, &f.Mimetype, &f.Filetype, &f.Size, &f.UserID,
		&f.URLPrivate, &f.URLPrivateDownload, &f.Permalink, &f.IsExternal, &f.ExternalURL,
		&f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get file: %w", err)
	}

	// Get associated channels
	channels, err := r.getFileChannels(ctx, id)
	if err != nil {
		return nil, err
	}
	f.Channels = channels
	return &f, nil
}

func (r *FileRepo) Update(ctx context.Context, f *domain.File) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE files SET title = $2, url_private = $3, url_private_download = $4,
		                 permalink = $5, upload_complete = TRUE
		WHERE id = $1`,
		f.ID, f.Title, f.URLPrivate, f.URLPrivateDownload, f.Permalink,
	)
	if err != nil {
		return fmt.Errorf("update file: %w", err)
	}
	return nil
}

func (r *FileRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM files WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *FileRepo) List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	query := `
		SELECT f.id, f.name, f.title, f.mimetype, f.filetype, f.size, f.user_id,
		       f.url_private, f.url_private_download, f.permalink, f.is_external, f.external_url,
		       f.created_at, f.updated_at
		FROM files f`
	var args []any
	var where []string

	if params.ChannelID != "" {
		args = append(args, params.ChannelID)
		query = `
			SELECT f.id, f.name, f.title, f.mimetype, f.filetype, f.size, f.user_id,
			       f.url_private, f.url_private_download, f.permalink, f.is_external, f.external_url,
			       f.created_at, f.updated_at
			FROM files f
			INNER JOIN file_channels fc ON f.id = fc.file_id
			WHERE fc.channel_id = $1`
		where = append(where, "channel")
	}

	if params.UserID != "" {
		args = append(args, params.UserID)
		if len(where) == 0 {
			query += ` WHERE`
		} else {
			query += ` AND`
		}
		query += fmt.Sprintf(` f.user_id = $%d`, len(args))
	}

	if params.Cursor != "" {
		args = append(args, params.Cursor)
		if len(where) == 0 && params.UserID == "" {
			query += ` WHERE`
		} else {
			query += ` AND`
		}
		query += fmt.Sprintf(` f.id > $%d`, len(args))
	}

	query += ` ORDER BY f.id ASC`
	args = append(args, limit+1)
	query += fmt.Sprintf(` LIMIT $%d`, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()

	var files []domain.File
	for rows.Next() {
		var f domain.File
		if err := rows.Scan(
			&f.ID, &f.Name, &f.Title, &f.Mimetype, &f.Filetype, &f.Size, &f.UserID,
			&f.URLPrivate, &f.URLPrivateDownload, &f.Permalink, &f.IsExternal, &f.ExternalURL,
			&f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}

	page := &domain.CursorPage[domain.File]{}
	if len(files) > limit {
		page.HasMore = true
		page.NextCursor = files[limit-1].ID
		page.Items = files[:limit]
	} else {
		page.Items = files
	}
	if page.Items == nil {
		page.Items = []domain.File{}
	}
	return page, nil
}

func (r *FileRepo) ShareToChannel(ctx context.Context, fileID, channelID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO file_channels (file_id, channel_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, fileID, channelID)
	if err != nil {
		return fmt.Errorf("share file to channel: %w", err)
	}
	return nil
}

func (r *FileRepo) getFileChannels(ctx context.Context, fileID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT channel_id FROM file_channels WHERE file_id = $1 ORDER BY shared_at ASC`, fileID)
	if err != nil {
		return nil, fmt.Errorf("get file channels: %w", err)
	}
	defer rows.Close()

	var channels []string
	for rows.Next() {
		var ch string
		if err := rows.Scan(&ch); err != nil {
			return nil, fmt.Errorf("scan file channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if channels == nil {
		channels = []string{}
	}
	return channels, nil
}
