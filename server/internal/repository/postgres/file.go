package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
		WorkspaceID:        f.WorkspaceID,
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

func (r *FileRepo) Get(ctx context.Context, workspaceID, id string) (*domain.File, error) {
	row, err := r.q.GetFile(ctx, sqlcgen.GetFileParams{WorkspaceID: workspaceID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get file: %w", err)
	}
	return r.inflateFile(ctx, fileToDomain(row))
}

func (r *FileRepo) GetByID(ctx context.Context, id string) (*domain.File, error) {
	row, err := r.q.GetFileByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get file: %w", err)
	}
	return r.inflateFile(ctx, fileByIDToDomain(row))
}

func (r *FileRepo) inflateFile(ctx context.Context, f *domain.File) (*domain.File, error) {
	channels, err := r.q.GetFileChannels(ctx, f.ID)
	if err != nil {
		return nil, fmt.Errorf("get file channels: %w", err)
	}
	f.Channels = channels

	return f, nil
}

func (r *FileRepo) Update(ctx context.Context, workspaceID string, f *domain.File) error {
	return r.q.UpdateFileComplete(ctx, sqlcgen.UpdateFileCompleteParams{
		WorkspaceID:        workspaceID,
		ID:                 f.ID,
		Title:              f.Title,
		UrlPrivate:         f.URLPrivate,
		UrlPrivateDownload: f.URLPrivateDownload,
		Permalink:          f.Permalink,
	})
}

func (r *FileRepo) Delete(ctx context.Context, workspaceID, id string) error {
	return r.q.DeleteFile(ctx, sqlcgen.DeleteFileParams{WorkspaceID: workspaceID, ID: id})
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
			WorkspaceID: params.WorkspaceID,
			ChannelID:   params.ChannelID,
			UserID:      params.UserID,
			ID:          params.Cursor,
			Limit:       int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByChannelAndUserToDomain(row))
		}
	case params.ChannelID != "":
		rows, err := r.q.ListFilesByChannel(ctx, sqlcgen.ListFilesByChannelParams{
			WorkspaceID: params.WorkspaceID,
			ChannelID:   params.ChannelID,
			ID:          params.Cursor,
			Limit:       int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByChannelToDomain(row))
		}
	case params.UserID != "":
		rows, err := r.q.ListFilesByUser(ctx, sqlcgen.ListFilesByUserParams{
			WorkspaceID: params.WorkspaceID,
			UserID:      params.UserID,
			ID:          params.Cursor,
			Limit:       int32(limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
		for _, row := range rows {
			files = append(files, *fileByUserToDomain(row))
		}
	default:
		rows, err := r.q.ListFiles(ctx, sqlcgen.ListFilesParams{
			WorkspaceID: params.WorkspaceID,
			ID:          params.Cursor,
			Limit:       int32(limit + 1),
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

func (r *FileRepo) ShareToChannel(ctx context.Context, workspaceID, fileID, channelID string) error {
	if _, err := r.q.GetFile(ctx, sqlcgen.GetFileParams{WorkspaceID: workspaceID, ID: fileID}); err != nil {
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
	if conversationWorkspaceAnchor(textToString(conv.OwnerWorkspaceID), textToString(conv.WorkspaceID)) != workspaceID {
		return domain.ErrNotFound
	}

	rowsAffected, err := r.q.ShareFileToChannel(ctx, sqlcgen.ShareFileToChannelParams{
		WorkspaceID: workspaceID,
		FileID:      fileID,
		ChannelID:   channelID,
	})
	if err != nil {
		return fmt.Errorf("share file to channel: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrAlreadyShared
	}
	return nil
}

func conversationWorkspaceAnchor(ownerWorkspaceID, workspaceID string) string {
	if ownerWorkspaceID = strings.TrimSpace(ownerWorkspaceID); ownerWorkspaceID != "" {
		return ownerWorkspaceID
	}
	return workspaceID
}
