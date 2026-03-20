package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"path/filepath"
	"time"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
	s3client "github.com/suhjohn/workspace/internal/s3"
)

// FileService contains business logic for file operations.
type FileService struct {
	repo     repository.FileRepository
	s3       *s3client.Client
	baseURL  string
	recorder EventRecorder
	logger   *slog.Logger
}

// NewFileService creates a new FileService.
func NewFileService(repo repository.FileRepository, s3 *s3client.Client, baseURL string, recorder EventRecorder, logger *slog.Logger) *FileService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &FileService{repo: repo, s3: s3, baseURL: baseURL, recorder: recorder, logger: logger}
}

func (s *FileService) GetUploadURL(ctx context.Context, params domain.GetUploadURLParams) (*domain.GetUploadURLResponse, error) {
	if s.s3 == nil {
		return nil, fmt.Errorf("file uploads not configured: %w", domain.ErrInvalidArgument)
	}
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

	// Generate presigned upload URL first to avoid orphaned DB records
	uploadURL, err := s.s3.GeneratePresignedURL(ctx, s3Key, contentType, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("generate upload url: %w", err)
	}

	// Create file record in DB only after URL generation succeeds
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

	resp := &domain.GetUploadURLResponse{
		UploadURL: uploadURL,
		FileID:    fileID,
	}
	payload, _ := json.Marshal(f)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventFileCreated,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		TeamID:        "",
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record file.created event", "error", recErr)
	}
	return resp, nil
}

func (s *FileService) CompleteUpload(ctx context.Context, params domain.CompleteUploadParams) (*domain.File, error) {
	if s.s3 == nil {
		return nil, fmt.Errorf("file uploads not configured: %w", domain.ErrInvalidArgument)
	}
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

	// Record file update event with full snapshot
	payload, _ := json.Marshal(f)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventFileUpdated,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		TeamID:        "",
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record file.updated event", "error", recErr)
	}

	// Record file.shared event with the {file_id, channel_id} format the projector expects
	if params.ChannelID != "" {
		sharePayload, _ := json.Marshal(map[string]string{"file_id": f.ID, "channel_id": params.ChannelID})
		if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
			EventType:     domain.EventFileShared,
			AggregateType: domain.AggregateFile,
			AggregateID:   f.ID,
			TeamID:        "",
			Payload:       sharePayload,
		}); recErr != nil {
			s.logger.Warn("record file.shared event", "error", recErr)
		}
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

	// Delete from S3 if configured
	if s.s3 != nil {
		s3Key := fmt.Sprintf("files/%s/%s", f.ID, f.Name)
		if err := s.s3.Delete(ctx, s3Key); err != nil {
			// Log but don't fail - DB cleanup is more important
			_ = err
		}
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	payload, _ := json.Marshal(f)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventFileDeleted,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		TeamID:        "",
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record file.deleted event", "error", recErr)
	}
	return nil
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
