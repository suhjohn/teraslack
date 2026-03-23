package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type FileRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewFileRepo(db DBTX) *FileRepo {
	return &FileRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new FileRepo that operates within the given transaction.
func (r *FileRepo) WithTx(tx pgx.Tx) repository.FileRepository {
	return &FileRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *FileRepo) Create(ctx context.Context, f *domain.File) error {
	return r.q.CreateFile(ctx, sqlcgen.CreateFileParams{
		ID:                 f.ID,
		TeamID:             f.TeamID,
		Name:               f.Name,
		Title:              f.Title,
		Mimetype:           f.Mimetype,
		Filetype:           f.Filetype,
		Size:               f.Size,
		UserID:             f.UserID,
		S3Key:              "",
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
		IsExternal:         f.IsExternal,
		ExternalUrl:        f.ExternalURL,
		UploadComplete:     false,
	})
}

func (r *FileRepo) Get(ctx context.Context, teamID, id string) (*domain.File, error) {
	row, err := r.q.GetFile(ctx, sqlcgen.GetFileParams{TeamID: teamID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get file: %w", err)
	}

	f := fileToDomain(row)

	channels, err := r.q.GetFileChannels(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get file channels: %w", err)
	}
	f.Channels = channels

	return f, nil
}

func (r *FileRepo) Update(ctx context.Context, teamID string, f *domain.File) error {
	return r.q.UpdateFileComplete(ctx, sqlcgen.UpdateFileCompleteParams{
		TeamID:             teamID,
		ID:                 f.ID,
		Title:              f.Title,
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
	})
}

func (r *FileRepo) Delete(ctx context.Context, teamID, id string) error {
	return r.q.DeleteFile(ctx, sqlcgen.DeleteFileParams{TeamID: teamID, ID: id})
}

func (r *FileRepo) List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var files []domain.File

	switch {
	case params.ChannelID != "" && params.UserID != "":
		rows, err := r.q.ListFilesByChannelAndUser(ctx, sqlcgen.ListFilesByChannelAndUserParams{
			TeamID:    params.TeamID,
			ChannelID: params.ChannelID,
			UserID:    params.UserID,
			ID:        params.Cursor,
			Limit:     int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByChannelAndUserToDomain(row))
		}
	case params.ChannelID != "":
		rows, err := r.q.ListFilesByChannel(ctx, sqlcgen.ListFilesByChannelParams{
			TeamID:    params.TeamID,
			ChannelID: params.ChannelID,
			ID:        params.Cursor,
			Limit:     int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByChannelToDomain(row))
		}
	case params.UserID != "":
		rows, err := r.q.ListFilesByUser(ctx, sqlcgen.ListFilesByUserParams{
			TeamID: params.TeamID,
			UserID: params.UserID,
			ID:     params.Cursor,
			Limit:  int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByUserToDomain(row))
		}
	default:
		rows, err := r.q.ListFiles(ctx, sqlcgen.ListFilesParams{
			TeamID: params.TeamID,
			ID:     params.Cursor,
			Limit:  int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileListToDomain(row))
		}
	}

	page := &domain.CursorPage[domain.File]{}
	if len(files) > limit {
		page.HasMore = true
		page.NextCursor = files[limit].ID
		page.Items = files[:limit]
	} else {
		page.Items = files
	}
	if page.Items == nil {
		page.Items = []domain.File{}
	}
	return page, nil
}

func (r *FileRepo) ShareToChannel(ctx context.Context, teamID, fileID, channelID string) error {
	if _, err := r.q.GetFile(ctx, sqlcgen.GetFileParams{TeamID: teamID, ID: fileID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("get file: %w", err)
	}

	conv, err := r.q.GetConversation(ctx, channelID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("get conversation: %w", err)
	}
	if conv.TeamID != teamID {
		return domain.ErrNotFound
	}

	tag, err := r.db.Exec(ctx,
		`INSERT INTO file_channels (file_id, channel_id)
		 VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		fileID, channelID,
	)
	if err != nil {
		return fmt.Errorf("share file to channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlreadyShared
	}
	return nil
}
