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

type FileRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewFileRepo(pool *pgxpool.Pool) *FileRepo {
	return &FileRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *FileRepo) Create(ctx context.Context, f *domain.File) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.CreateFile(ctx, sqlcgen.CreateFileParams{
		ID:                 f.ID,
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
	}); err != nil {
		return fmt.Errorf("insert file: %w", err)
	}

	eventData, _ := json.Marshal(f)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		EventType:     domain.EventFileCreated,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *FileRepo) Get(ctx context.Context, id string) (*domain.File, error) {
	row, err := r.q.GetFile(ctx, id)
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

func (r *FileRepo) Update(ctx context.Context, f *domain.File) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.UpdateFileComplete(ctx, sqlcgen.UpdateFileCompleteParams{
		ID:                 f.ID,
		Title:              f.Title,
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
	}); err != nil {
		return fmt.Errorf("update file: %w", err)
	}

	eventData, _ := json.Marshal(f)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		EventType:     domain.EventFileUpdated,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *FileRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteFile(ctx, id); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}

	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   id,
		EventType:     domain.EventFileDeleted,
		EventData:     []byte("{}"),
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
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
			ID:    params.Cursor,
			Limit: int32(limit + 1),
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.ShareFileToChannel(ctx, sqlcgen.ShareFileToChannelParams{
		FileID:    fileID,
		ChannelID: channelID,
	}); err != nil {
		return fmt.Errorf("share file: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{"file_id": fileID, "channel_id": channelID})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateFile,
		AggregateID:   fileID,
		EventType:     domain.EventFileShared,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
