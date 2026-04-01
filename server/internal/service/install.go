package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const installSessionTTL = 10 * time.Minute

var defaultInstallPermissions = []string{
	domain.PermissionMessagesRead,
	domain.PermissionMessagesWrite,
	domain.PermissionUsersCreate,
	domain.PermissionAPIKeysCreate,
	domain.PermissionConversationsCreate,
	domain.PermissionConversationsManagersWrite,
	domain.PermissionConversationsMembersWrite,
	domain.PermissionConversationsPostingPolicyWrite,
	domain.PermissionFilesRead,
	domain.PermissionFilesWrite,
}

type InstallConfig struct {
	BaseURL string
}

type InstallService struct {
	repo          repository.InstallSessionRepository
	userRepo      repository.UserRepository
	workspaceRepo repository.WorkspaceRepository
	apiKeySvc     *APIKeyService
	encryptor     *crypto.Encryptor
	logger        *slog.Logger
	baseURL       string
}

type installWorkspaceMembership struct {
	workspace domain.Workspace
	user      domain.User
}

func NewInstallService(repo repository.InstallSessionRepository, userRepo repository.UserRepository, workspaceRepo repository.WorkspaceRepository, apiKeySvc *APIKeyService, encryptor *crypto.Encryptor, logger *slog.Logger, cfg InstallConfig) *InstallService {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &InstallService{
		repo:          repo,
		userRepo:      userRepo,
		workspaceRepo: workspaceRepo,
		apiKeySvc:     apiKeySvc,
		encryptor:     encryptor,
		logger:        logger,
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
	}
}

func (s *InstallService) CreateSession(ctx context.Context, req domain.CreateInstallSessionRequest) (*domain.CreateInstallSessionResponse, error) {
	if err := s.expirePending(ctx); err != nil {
		return nil, err
	}

	session, rawPollToken, err := s.repo.Create(ctx, domain.CreateInstallSessionParams{
		DeviceName: strings.TrimSpace(req.DeviceName),
		ClientKind: normalizeInstallClientKind(req.ClientKind),
		ExpiresAt:  time.Now().UTC().Add(installSessionTTL),
	})
	if err != nil {
		return nil, err
	}

	return &domain.CreateInstallSessionResponse{
		InstallID:   session.ID,
		ApprovalURL: s.approvalURL(session.ID),
		PollToken:   rawPollToken,
		ExpiresAt:   session.ExpiresAt,
	}, nil
}

func (s *InstallService) BuildApprovalPrompt(ctx context.Context, auth *domain.AuthContext, installID string) (*domain.InstallApprovalPrompt, error) {
	if auth == nil || auth.UserID == "" || auth.WorkspaceID == "" {
		return nil, domain.ErrInvalidAuth
	}
	if err := s.expirePending(ctx); err != nil {
		return nil, err
	}

	session, err := s.repo.Get(ctx, strings.TrimSpace(installID))
	if err != nil {
		return nil, err
	}
	user, err := s.userRepo.Get(ctx, auth.UserID)
	if err != nil {
		return nil, err
	}
	memberships, err := s.availableWorkspaceMembershipsForUser(ctx, auth, user)
	if err != nil {
		return nil, err
	}
	selectedWorkspaceID := auth.WorkspaceID
	workspace, err := findWorkspaceByID(membershipsToWorkspaces(memberships), selectedWorkspaceID)
	if err != nil {
		return nil, err
	}
	return &domain.InstallApprovalPrompt{
		Session:             session,
		Workspace:           workspace,
		User:                user,
		AvailableWorkspaces: membershipsToWorkspaces(memberships),
		SelectedWorkspaceID: selectedWorkspaceID,
		ApprovalURL:         s.approvalURL(session.ID),
	}, nil
}

func (s *InstallService) ApproveSession(ctx context.Context, auth *domain.AuthContext, installID, requestedWorkspaceID string) (*domain.InstallSession, error) {
	if auth == nil || auth.UserID == "" || auth.WorkspaceID == "" {
		return nil, domain.ErrInvalidAuth
	}
	if err := s.expirePending(ctx); err != nil {
		return nil, err
	}

	session, err := s.repo.Get(ctx, strings.TrimSpace(installID))
	if err != nil {
		return nil, err
	}
	switch session.Status {
	case domain.InstallSessionStatusApproved, domain.InstallSessionStatusConsumed:
		return session, nil
	case domain.InstallSessionStatusExpired, domain.InstallSessionStatusCanceled:
		return session, nil
	case domain.InstallSessionStatusPending:
	default:
		return nil, domain.ErrInvalidArgument
	}

	user, err := s.userRepo.Get(ctx, auth.UserID)
	if err != nil {
		return nil, err
	}
	memberships, err := s.availableWorkspaceMembershipsForUser(ctx, auth, user)
	if err != nil {
		return nil, err
	}

	targetWorkspaceID := strings.TrimSpace(requestedWorkspaceID)
	if targetWorkspaceID == "" {
		targetWorkspaceID = auth.WorkspaceID
	}
	targetMembership, err := findMembershipByWorkspaceID(memberships, targetWorkspaceID)
	if err != nil {
		return nil, err
	}

	keyCtx := context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, targetWorkspaceID)
	keyCtx = context.WithValue(keyCtx, ctxutil.ContextKeyUserID, targetMembership.user.ID)
	keyCtx = ctxutil.WithPrincipal(keyCtx, targetMembership.user.PrincipalType, targetMembership.user.EffectiveAccountType(), targetMembership.user.IsBot)

	keyName := "local-mcp"
	if session.DeviceName != "" {
		keyName += " " + session.DeviceName
	}

	key, rawKey, err := s.apiKeySvc.Create(keyCtx, domain.CreateAPIKeyParams{
		Name:        keyName,
		WorkspaceID: targetWorkspaceID,
		UserID:      targetMembership.user.ID,
		CreatedBy:   targetMembership.user.ID,
		Permissions: append([]string(nil), defaultInstallPermissions...),
	})
	if err != nil {
		return nil, err
	}

	encrypted, err := s.encryptor.Encrypt(rawKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt install credential: %w", err)
	}

	return s.repo.Approve(ctx, domain.ApproveInstallSessionParams{
		ID:                     session.ID,
		WorkspaceID:            targetWorkspaceID,
		ApprovedByUserID:       auth.UserID,
		CredentialID:           key.ID,
		RawCredentialEncrypted: encrypted,
		ApprovedAt:             time.Now().UTC(),
	})
}

func (s *InstallService) PollSession(ctx context.Context, installID, rawPollToken string) (*domain.PollInstallSessionResponse, error) {
	if strings.TrimSpace(rawPollToken) == "" {
		return nil, domain.ErrInvalidArgument
	}
	if err := s.expirePending(ctx); err != nil {
		return nil, err
	}

	session, err := s.repo.GetByPollTokenHash(ctx, strings.TrimSpace(installID), crypto.HashToken(strings.TrimSpace(rawPollToken)))
	if err != nil {
		return nil, err
	}

	resp := &domain.PollInstallSessionResponse{
		Status: session.Status,
	}
	switch session.Status {
	case domain.InstallSessionStatusPending, domain.InstallSessionStatusExpired, domain.InstallSessionStatusCanceled, domain.InstallSessionStatusConsumed:
		return resp, nil
	case domain.InstallSessionStatusApproved:
	default:
		return resp, nil
	}

	rawKey, err := s.encryptor.Decrypt(session.RawCredentialEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt install credential: %w", err)
	}

	if _, err := s.repo.Consume(ctx, domain.ConsumeInstallSessionParams{
		ID:         session.ID,
		ConsumedAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}

	resp.Status = domain.InstallSessionStatusApproved
	resp.BaseURL = s.baseURL
	resp.WorkspaceID = session.WorkspaceID
	resp.UserID = session.ApprovedByUserID
	resp.APIKey = rawKey
	return resp, nil
}

func (s *InstallService) approvalURL(installID string) string {
	base := strings.TrimRight(s.baseURL, "/")
	if base == "" {
		return "/cli/install/" + url.PathEscape(installID)
	}
	return base + "/cli/install/" + url.PathEscape(installID)
}

func (s *InstallService) expirePending(ctx context.Context) error {
	return s.repo.ExpirePending(ctx, time.Now().UTC())
}

func normalizeInstallClientKind(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "local_mcp"
	}
	return raw
}

func (s *InstallService) availableWorkspaceMembershipsForUser(ctx context.Context, auth *domain.AuthContext, user *domain.User) ([]installWorkspaceMembership, error) {
	if user == nil {
		return nil, domain.ErrInvalidArgument
	}
	if auth == nil || auth.WorkspaceID == "" {
		return nil, domain.ErrInvalidAuth
	}
	if user.PrincipalType != domain.PrincipalTypeHuman || strings.TrimSpace(user.Email) == "" {
		workspace, err := s.workspaceRepo.Get(ctx, auth.WorkspaceID)
		if err != nil {
			return nil, err
		}
		return []installWorkspaceMembership{{
			workspace: *workspace,
			user:      *user,
		}}, nil
	}

	users, err := s.userRepo.ListByEmail(ctx, user.Email)
	if err != nil {
		return nil, fmt.Errorf("list workspace memberships: %w", err)
	}

	memberships := make([]installWorkspaceMembership, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	for _, member := range users {
		if member.WorkspaceID == "" || member.Deleted {
			continue
		}
		if _, ok := seen[member.WorkspaceID]; ok {
			continue
		}
		workspace, getErr := s.workspaceRepo.Get(ctx, member.WorkspaceID)
		if getErr != nil {
			return nil, getErr
		}
		memberships = append(memberships, installWorkspaceMembership{
			workspace: *workspace,
			user:      member,
		})
		seen[member.WorkspaceID] = struct{}{}
	}
	if len(memberships) == 0 {
		workspace, err := s.workspaceRepo.Get(ctx, auth.WorkspaceID)
		if err != nil {
			return nil, err
		}
		return []installWorkspaceMembership{{
			workspace: *workspace,
			user:      *user,
		}}, nil
	}
	return memberships, nil
}

func findWorkspaceByID(workspaces []domain.Workspace, workspaceID string) (*domain.Workspace, error) {
	for i := range workspaces {
		if workspaces[i].ID == workspaceID {
			return &workspaces[i], nil
		}
	}
	return nil, domain.ErrForbidden
}

func findMembershipByWorkspaceID(memberships []installWorkspaceMembership, workspaceID string) (*installWorkspaceMembership, error) {
	for i := range memberships {
		if memberships[i].workspace.ID == workspaceID {
			return &memberships[i], nil
		}
	}
	return nil, domain.ErrForbidden
}

func membershipsToWorkspaces(memberships []installWorkspaceMembership) []domain.Workspace {
	workspaces := make([]domain.Workspace, 0, len(memberships))
	for _, membership := range memberships {
		workspaces = append(workspaces, membership.workspace)
	}
	return workspaces
}
