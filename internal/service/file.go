package service

import (
	"context"
	"fmt"
	"mime"
	"path/filepath"
	"time"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
	s3client "github.com/suhjohn/workspace/internal/s3"
)

// FileService contains business logic for file operations.
type FileService struct {
	repo   repository.FileRepository
	s3     *s3client.Client
	baseURL string
}

// NewFileService creates a new FileService.
func NewFileService(repo repository.FileRepository, s3 *s3client.Client, baseURL string) *FileService {
	return &FileService{repo: repo, s3: s3, baseURL: baseURL}
}

func (s *FileService) GetUploadURL(ctx context.Context, params domain.GetUploadURLParams) (*domain.GetUploadURLResponse, error) {
	if params.Filename == "" {
		return nil, fmt.Errorf("filename: %w", domain.ErrInvalidArgument)
	}
	if params.Length <= 0 {
		return nil, fmt.Errorf("length: %w", domain.ErrInvalidArgument)
	}

	fileID := fmt.Sprintf("F%d", time.Now().UnixNano())
	ext := filepath.Ext(params.Filename)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	s3Key := fmt.Sprintf("files/%s/%s", fileID, params.Filename)

	// Create file record in DB
	f := &domain.File{
		ID:       fileID,
		Name:     params.Filename,
		Mimetype: contentType,
		Filetype: ext,
		Size:     params.Length,
	}
	if err := s.repo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("create file record: %w", err)
	}

	// Generate presigned upload URL
	uploadURL, err := s.s3.GeneratePresignedURL(ctx, s3Key, contentType, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("generate upload url: %w", err)
	}

	return &domain.GetUploadURLResponse{
		UploadURL: uploadURL,
		FileID:    fileID,
	}, nil
}

func (s *FileService) CompleteUpload(ctx context.Context, params domain.CompleteUploadParams) (*domain.File, error) {
	if params.FileID == "" {
		return nil, fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}

	f, err := s.repo.Get(ctx, params.FileID)
	if err != nil {
		return nil, err
	}

	if params.Title != "" {
		f.Title = params.Title
	}

	s3Key := fmt.Sprintf("files/%s/%s", f.ID, f.Name)

	// Generate download URLs
	downloadURL, err := s.s3.GenerateDownloadURL(ctx, s3Key, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("generate download url: %w", err)
	}

	f.URLPrivate = downloadURL
	f.URLPrivateDownload = downloadURL
	f.Permalink = fmt.Sprintf("%s/files/%s", s.baseURL, f.ID)

	if err := s.repo.Update(ctx, f); err != nil {
		return nil, fmt.Errorf("update file: %w", err)
	}

	// Share to channel if specified
	if params.ChannelID != "" {
		if err := s.repo.ShareToChannel(ctx, f.ID, params.ChannelID); err != nil {
			return nil, fmt.Errorf("share to channel: %w", err)
		}
		f.Channels = append(f.Channels, params.ChannelID)
	}

	return f, nil
}

func (s *FileService) Get(ctx context.Context, id string) (*domain.File, error) {
	if id == "" {
		return nil, fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Get(ctx, id)
}

func (s *FileService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}

	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete from S3
	s3Key := fmt.Sprintf("files/%s/%s", f.ID, f.Name)
	if err := s.s3.Delete(ctx, s3Key); err != nil {
		// Log but don't fail - DB cleanup is more important
		_ = err
	}

	return s.repo.Delete(ctx, id)
}

func (s *FileService) List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	return s.repo.List(ctx, params)
}

func (s *FileService) AddRemoteFile(ctx context.Context, params domain.AddRemoteFileParams) (*domain.File, error) {
	if params.ExternalURL == "" {
		return nil, fmt.Errorf("external_url: %w", domain.ErrInvalidArgument)
	}
	if params.Title == "" {
		return nil, fmt.Errorf("title: %w", domain.ErrInvalidArgument)
	}

	fileID := fmt.Sprintf("F%d", time.Now().UnixNano())
	f := &domain.File{
		ID:          fileID,
		Name:        params.Title,
		Title:       params.Title,
		Filetype:    params.Filetype,
		UserID:      params.UserID,
		IsExternal:  true,
		ExternalURL: params.ExternalURL,
		Permalink:   params.ExternalURL,
	}

	if err := s.repo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("create remote file: %w", err)
	}
	return f, nil
}

func (s *FileService) ShareRemoteFile(ctx context.Context, params domain.ShareRemoteFileParams) error {
	if params.FileID == "" {
		return fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}
	for _, ch := range params.Channels {
		if err := s.repo.ShareToChannel(ctx, params.FileID, ch); err != nil {
			return fmt.Errorf("share to channel %s: %w", ch, err)
		}
	}
	return nil
}
