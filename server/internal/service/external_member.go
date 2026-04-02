package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ExternalMemberService struct {
	repo          repository.ExternalMemberRepository
	accountRepo   repository.AccountRepository
	convRepo      repository.ConversationRepository
	workspaceRepo repository.WorkspaceRepository
	accessSvc     *ConversationAccessService
}

func NewExternalMemberService(
	repo repository.ExternalMemberRepository,
	accountRepo repository.AccountRepository,
	convRepo repository.ConversationRepository,
	workspaceRepo repository.WorkspaceRepository,
) *ExternalMemberService {
	return &ExternalMemberService{
		repo:          repo,
		accountRepo:   accountRepo,
		convRepo:      convRepo,
		workspaceRepo: workspaceRepo,
	}
}

func (s *ExternalMemberService) SetConversationAccessService(accessSvc *ConversationAccessService) {
	s.accessSvc = accessSvc
}

func (s *ExternalMemberService) Create(ctx context.Context, conversationID string, params domain.CreateExternalMemberParams) (*domain.ExternalMember, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsMembersWrite); err != nil {
		return nil, err
	}
	conv, err := s.authorizeConversationMutation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	params.ConversationID = conversationID
	params.ExternalWorkspaceID = strings.TrimSpace(params.ExternalWorkspaceID)
	if params.ExternalWorkspaceID == "" {
		return nil, fmt.Errorf("external_workspace_id: %w", domain.ErrInvalidArgument)
	}
	if !domain.IsValidExternalPrincipalAccessMode(params.AccessMode) {
		return nil, fmt.Errorf("access_mode: %w", domain.ErrInvalidArgument)
	}
	if params.PrincipalType == "" {
		params.PrincipalType = domain.PrincipalTypeHuman
	}
	if _, err := s.requireConnectedExternalWorkspace(ctx, conv.WorkspaceID, params.ExternalWorkspaceID); err != nil {
		return nil, err
	}
	account, err := resolveOrCreateAccount(ctx, s.accountRepo, params)
	if err != nil {
		return nil, err
	}
	params.AccountID = account.ID
	params.InvitedBy = actorUserID(ctx)
	params.AllowedCapabilities = normalizeExternalMemberCapabilities(params.AccessMode, params.AllowedCapabilities)
	item, err := s.repo.Create(ctx, params, conv.WorkspaceID)
	if err != nil {
		return nil, err
	}
	item.Account = account
	return item, nil
}

func (s *ExternalMemberService) ListByConversation(ctx context.Context, conversationID string) ([]domain.ExternalMember, error) {
	if _, err := s.authorizeConversationMutation(ctx, conversationID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListByConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		account, err := s.accountRepo.Get(ctx, items[i].AccountID)
		if err != nil {
			return nil, err
		}
		items[i].Account = account
	}
	return items, nil
}

func (s *ExternalMemberService) Update(ctx context.Context, conversationID, id string, params domain.UpdateExternalMemberParams) (*domain.ExternalMember, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsMembersWrite); err != nil {
		return nil, err
	}
	if _, err := s.authorizeConversationMutation(ctx, conversationID); err != nil {
		return nil, err
	}
	if params.AccessMode != nil && !domain.IsValidExternalPrincipalAccessMode(*params.AccessMode) {
		return nil, fmt.Errorf("access_mode: %w", domain.ErrInvalidArgument)
	}
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.ConversationID != conversationID {
		return nil, domain.ErrForbidden
	}
	if params.AllowedCapabilities == nil || len(*params.AllowedCapabilities) == 0 {
		mode := item.AccessMode
		if params.AccessMode != nil {
			mode = *params.AccessMode
		}
		caps := normalizeExternalMemberCapabilities(mode, nil)
		params.AllowedCapabilities = &caps
	}
	updated, err := s.repo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	account, err := s.accountRepo.Get(ctx, updated.AccountID)
	if err != nil {
		return nil, err
	}
	updated.Account = account
	return updated, nil
}

func (s *ExternalMemberService) Revoke(ctx context.Context, conversationID, id string) error {
	if err := requirePermission(ctx, domain.PermissionConversationsMembersWrite); err != nil {
		return err
	}
	if _, err := s.authorizeConversationMutation(ctx, conversationID); err != nil {
		return err
	}
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.ConversationID != conversationID {
		return domain.ErrForbidden
	}
	return s.repo.Revoke(ctx, id, time.Now().UTC())
}

func (s *ExternalMemberService) RevokeByExternalWorkspace(ctx context.Context, hostWorkspaceID, externalWorkspaceID string) error {
	return s.repo.RevokeByExternalWorkspace(ctx, hostWorkspaceID, externalWorkspaceID, time.Now().UTC())
}

func (s *ExternalMemberService) authorizeConversationMutation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}
	if s.accessSvc != nil {
		if err := s.accessSvc.CanManageMembers(ctx, conv); err != nil {
			return nil, err
		}
	}
	return conv, nil
}

func (s *ExternalMemberService) requireConnectedExternalWorkspace(ctx context.Context, workspaceID, externalWorkspaceID string) (*domain.ExternalWorkspace, error) {
	if s.workspaceRepo == nil {
		return nil, domain.ErrInvalidArgument
	}
	item, err := s.workspaceRepo.GetExternalWorkspace(ctx, workspaceID, externalWorkspaceID)
	if err != nil {
		return nil, err
	}
	if !item.Connected {
		return nil, domain.ErrForbidden
	}
	return item, nil
}

func normalizeExternalMemberCapabilities(mode domain.ExternalPrincipalAccessMode, capabilities []string) []string {
	if len(capabilities) > 0 {
		return capabilities
	}
	switch mode {
	case domain.ExternalPrincipalAccessModeSharedReadOnly:
		return []string{
			domain.PermissionMessagesRead,
			domain.PermissionFilesRead,
		}
	default:
		return []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionFilesRead,
		}
	}
}
