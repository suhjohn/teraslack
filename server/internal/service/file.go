package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	s3client "github.com/suhjohn/teraslack/internal/s3"
)

// FileService contains business logic for file operations.
type FileService struct {
	repo            repository.FileRepository
	externalMembers repository.ExternalMemberRepository
	userRepo        repository.UserRepository
	convRepo        repository.ConversationRepository
	access          *ConversationAccessService
	s3              *s3client.Client
	s3Prefix        string
	baseURL         string
	recorder        EventRecorder
	db              repository.TxBeginner
	logger          *slog.Logger
}

// NewFileService creates a new FileService.
func NewFileService(repo repository.FileRepository, s3 *s3client.Client, s3Prefix string, baseURL string, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *FileService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &FileService{
		repo:     repo,
		s3:       s3,
		s3Prefix: strings.Trim(strings.TrimSpace(s3Prefix), "/"),
		baseURL:  baseURL,
		recorder: recorder,
		db:       db,
		logger:   logger,
	}
}

func (s *FileService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMembers = repo
}

func (s *FileService) SetUserRepository(repo repository.UserRepository) {
	s.userRepo = repo
}

func (s *FileService) SetConversationRepository(repo repository.ConversationRepository) {
	s.convRepo = repo
}

func (s *FileService) SetAccessService(access *ConversationAccessService) {
	s.access = access
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
	workspaceID, userID, _, err := s.resolveCreateContext(ctx, params.ChannelID)
	if err != nil {
		return nil, err
	}

	fileID := generateFileID()
	ext := filepath.Ext(params.Filename)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	s3Key := s.objectKey(fileID, params.Filename)

	// Generate presigned upload URL first to avoid orphaned DB records
	uploadURL, err := s.s3.GeneratePresignedURL(ctx, s3Key, contentType, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("generate upload url: %w", err)
	}

	// Create file record in DB only after URL generation succeeds
	f := &domain.File{
		WorkspaceID: workspaceID,
		ID:          fileID,
		Name:        params.Filename,
		UserID:      userID,
		Mimetype:    contentType,
		Filetype:    ext,
		Size:        params.Length,
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Create(ctx, f); err != nil {
		return nil, fmt.Errorf("create file record: %w", err)
	}

	payload, _ := json.Marshal(f)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventFileCreated,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		WorkspaceID:   f.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record file.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	resp := &domain.GetUploadURLResponse{
		UploadURL: uploadURL,
		FileID:    fileID,
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
	workspaceID, _, _, err := s.resolveCreateContext(ctx, params.ChannelID)
	if err != nil {
		return nil, err
	}

	f, err := s.repo.Get(ctx, workspaceID, params.FileID)
	if err != nil {
		return nil, err
	}

	if params.Title != "" {
		f.Title = params.Title
	}

	s3Key := s.objectKey(f.ID, f.Name)

	// Generate download URLs
	downloadURL, err := s.s3.GenerateDownloadURL(ctx, s3Key, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("generate download url: %w", err)
	}

	f.URLPrivate = downloadURL
	f.URLPrivateDownload = downloadURL
	f.Permalink = fmt.Sprintf("%s/files/%s", s.baseURL, f.ID)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	txRecorder := s.recorder.WithTx(tx)
	sharedToChannel := false

	if err := txRepo.Update(ctx, workspaceID, f); err != nil {
		return nil, fmt.Errorf("update file: %w", err)
	}

	// Share to channel if specified
	if params.ChannelID != "" {
		if err := txRepo.ShareToChannel(ctx, workspaceID, f.ID, params.ChannelID); err != nil {
			if !errors.Is(err, domain.ErrAlreadyShared) {
				return nil, fmt.Errorf("share to channel: %w", err)
			}
		} else {
			f.Channels = append(f.Channels, params.ChannelID)
			sharedToChannel = true
		}
	}

	// Record file update event with full snapshot
	payload, _ := json.Marshal(f)
	if err := txRecorder.Record(ctx, domain.InternalEvent{
		EventType:     domain.EventFileUpdated,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		WorkspaceID:   f.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record file.updated event: %w", err)
	}

	// Record file.shared event with the {file_id, channel_id} format the projector expects
	if params.ChannelID != "" && sharedToChannel {
		sharePayload, _ := json.Marshal(map[string]string{"file_id": f.ID, "channel_id": params.ChannelID})
		if err := txRecorder.Record(ctx, domain.InternalEvent{
			EventType:     domain.EventFileShared,
			AggregateType: domain.AggregateFile,
			AggregateID:   f.ID,
			WorkspaceID:   f.WorkspaceID,
			Payload:       sharePayload,
		}); err != nil {
			return nil, fmt.Errorf("record file.shared event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return f, nil
}

func (s *FileService) Get(ctx context.Context, id string) (*domain.File, error) {
	if id == "" {
		return nil, fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensureFileAccess(ctx, f, false); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *FileService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}

	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.ensureFileAccess(ctx, f, true); err != nil {
		return err
	}
	if err := ensureFileOwnerOrAdmin(ctx, f.UserID); err != nil {
		return err
	}

	// Delete from S3 if configured
	if s.s3 != nil {
		s3Key := s.objectKey(f.ID, f.Name)
		if err := s.s3.Delete(ctx, s3Key); err != nil {
			// Log but don't fail - DB cleanup is more important
			_ = err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Delete(ctx, f.WorkspaceID, id); err != nil {
		return err
	}
	payload, _ := json.Marshal(f)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventFileDeleted,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		WorkspaceID:   f.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record file.deleted event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *FileService) List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	params.WorkspaceID = ctxutil.GetWorkspaceID(ctx)
	if params.WorkspaceID == "" && params.ChannelID == "" {
		return nil, fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
	}
	if params.ChannelID != "" {
		conv, err := s.resolveChannelAccess(ctx, params.ChannelID, false)
		if err != nil {
			return nil, err
		}
		params.WorkspaceID = conversationWorkspaceID(conv)
	}
	if params.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
	}
	if params.ChannelID == "" {
		if err := s.ensureWorkspaceFileAccess(ctx, params.WorkspaceID); err != nil {
			return nil, err
		}
	}
	return s.repo.List(ctx, params)
}

func (s *FileService) AddRemoteFile(ctx context.Context, params domain.AddRemoteFileParams) (*domain.File, error) {
	if params.ExternalURL == "" {
		return nil, fmt.Errorf("external_url: %w", domain.ErrInvalidArgument)
	}
	if params.Title == "" {
		return nil, fmt.Errorf("title: %w", domain.ErrInvalidArgument)
	}
	workspaceID, userID, _, err := s.resolveCreateContext(ctx, params.ChannelID)
	if err != nil {
		return nil, err
	}

	fileID := generateFileID()
	f := &domain.File{
		WorkspaceID: workspaceID,
		ID:          fileID,
		Name:        params.Title,
		Title:       params.Title,
		Filetype:    params.Filetype,
		UserID:      userID,
		IsExternal:  true,
		ExternalURL: params.ExternalURL,
		Permalink:   params.ExternalURL,
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Create(ctx, f); err != nil {
		return nil, fmt.Errorf("create remote file: %w", err)
	}

	payload, _ := json.Marshal(f)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventFileCreated,
		AggregateType: domain.AggregateFile,
		AggregateID:   f.ID,
		WorkspaceID:   f.WorkspaceID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record file.created event: %w", err)
	}

	if params.ChannelID != "" {
		if err := s.repo.WithTx(tx).ShareToChannel(ctx, workspaceID, f.ID, params.ChannelID); err != nil {
			if !errors.Is(err, domain.ErrAlreadyShared) {
				return nil, fmt.Errorf("share remote file to channel: %w", err)
			}
		} else {
			f.Channels = append(f.Channels, params.ChannelID)
			sharePayload, _ := json.Marshal(map[string]string{"file_id": f.ID, "channel_id": params.ChannelID})
			if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
				EventType:     domain.EventFileShared,
				AggregateType: domain.AggregateFile,
				AggregateID:   f.ID,
				WorkspaceID:   f.WorkspaceID,
				Payload:       sharePayload,
			}); err != nil {
				return nil, fmt.Errorf("record file.shared event: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return f, nil
}

// generateFileID creates a UUIDv7-based file ID with "F" prefix.
func generateFileID() string {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return fmt.Sprintf("F_%s", id.String())
}

func (s *FileService) ShareRemoteFile(ctx context.Context, params domain.ShareRemoteFileParams) error {
	if params.FileID == "" {
		return fmt.Errorf("file_id: %w", domain.ErrInvalidArgument)
	}
	f, err := s.repo.GetByID(ctx, params.FileID)
	if err != nil {
		return err
	}
	if err := s.ensureFileAccess(ctx, f, true); err != nil {
		return err
	}
	if err := ensureFileOwnerOrAdmin(ctx, f.UserID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	txRecorder := s.recorder.WithTx(tx)

	for _, ch := range params.Channels {
		if err := txRepo.ShareToChannel(ctx, f.WorkspaceID, params.FileID, ch); err != nil {
			if errors.Is(err, domain.ErrAlreadyShared) {
				continue
			}
			return fmt.Errorf("share to channel %s: %w", ch, err)
		}

		sharePayload, _ := json.Marshal(map[string]string{"file_id": params.FileID, "channel_id": ch})
		if err := txRecorder.Record(ctx, domain.InternalEvent{
			EventType:     domain.EventFileShared,
			AggregateType: domain.AggregateFile,
			AggregateID:   params.FileID,
			WorkspaceID:   f.WorkspaceID,
			Payload:       sharePayload,
		}); err != nil {
			return fmt.Errorf("record file.shared event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func fileTeamContext(ctx context.Context) (string, error) {
	workspaceID := ctxutil.GetWorkspaceID(ctx)
	if workspaceID == "" {
		return "", fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
	}
	return workspaceID, nil
}

func fileContext(ctx context.Context) (workspaceID, userID string, err error) {
	workspaceID, err = fileTeamContext(ctx)
	if err != nil {
		return "", "", err
	}
	userID = actorUserID(ctx)
	if userID == "" {
		return "", "", fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	return workspaceID, userID, nil
}

func (s *FileService) resolveCreateContext(ctx context.Context, channelID string) (workspaceID, userID string, external bool, err error) {
	userID = actorUserID(ctx)
	if userID == "" {
		return "", "", false, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	if channelID != "" {
		conv, err := s.resolveChannelAccess(ctx, channelID, true)
		if err != nil {
			return "", "", true, err
		}
		return conversationWorkspaceID(conv), userID, false, nil
	}
	workspaceID, err = fileTeamContext(ctx)
	if err != nil {
		return "", "", false, err
	}
	if err := s.ensureWorkspaceFileAccess(ctx, workspaceID); err != nil {
		return "", "", false, err
	}
	return workspaceID, userID, false, nil
}

func ensureFileOwnerOrAdmin(ctx context.Context, ownerUserID string) error {
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	actorID := actorUserID(ctx)
	if actorID != "" && actorID == ownerUserID {
		return nil
	}
	if contextIsWorkspaceAdmin(ctx) {
		return nil
	}
	return domain.ErrForbidden
}

func (s *FileService) objectKey(fileID, filename string) string {
	base := fmt.Sprintf("files/%s/%s", fileID, filename)
	if s.s3Prefix == "" {
		return base
	}
	return s.s3Prefix + "/" + base
}

func (s *FileService) ensureWorkspaceFileAccess(ctx context.Context, workspaceID string) error {
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, workspaceID); err != nil {
		return err
	}
	if s.userRepo == nil || actorAccountID(ctx) == "" {
		return nil
	}
	membership, err := s.userRepo.GetWorkspaceMembership(ctx, workspaceID, actorAccountID(ctx))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrForbidden
		}
		return err
	}
	if membership.IsGuest() && !membership.HasWorkspaceWideAccess() {
		return domain.ErrForbidden
	}
	return nil
}

func (s *FileService) resolveChannelAccess(ctx context.Context, channelID string, requireWrite bool) (*domain.Conversation, error) {
	if s.convRepo == nil {
		return nil, domain.ErrForbidden
	}
	conv, err := s.convRepo.Get(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if s.access != nil {
		if err := s.access.ensureConversationVisible(ctx, conv); err != nil {
			return nil, err
		}
		if requireWrite {
			if err := s.access.CanPost(ctx, conv, actorUserID(ctx)); err != nil {
				return nil, err
			}
		}
		return conv, nil
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if conv.Type == domain.ConversationTypePrivateChannel || conv.Type == domain.ConversationTypeIM || conv.Type == domain.ConversationTypeMPIM {
		accountID := actorAccountID(ctx)
		if accountID == "" {
			return nil, domain.ErrForbidden
		}
		isMember, err := s.convRepo.IsAccountMember(ctx, conv.ID, accountID)
		if err != nil {
			return nil, err
		}
		if !isMember {
			return nil, domain.ErrForbidden
		}
	}
	return conv, nil
}

func (s *FileService) ensureFileAccess(ctx context.Context, f *domain.File, requireWrite bool) error {
	if f == nil {
		return domain.ErrNotFound
	}
	if len(f.Channels) == 0 {
		return s.ensureWorkspaceFileAccess(ctx, f.WorkspaceID)
	}
	for _, channelID := range f.Channels {
		if _, err := s.resolveChannelAccess(ctx, channelID, requireWrite); err == nil {
			return nil
		}
	}
	return domain.ErrForbidden
}
