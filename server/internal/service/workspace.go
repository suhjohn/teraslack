package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// WorkspaceService contains business logic for Slack-style workspace APIs.
type WorkspaceService struct {
	repo               repository.WorkspaceRepository
	userRepo           repository.UserRepository
	accountRepo        repository.AccountRepository
	membershipRepo     repository.WorkspaceMembershipRepository
	externalMemberRepo repository.ExternalMemberRepository
	auditRepo          repository.AuthorizationAuditRepository
	recorder           EventRecorder
	db                 repository.TxBeginner
	logger             *slog.Logger
}

// NewWorkspaceService creates a new WorkspaceService.
func NewWorkspaceService(repo repository.WorkspaceRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *WorkspaceService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &WorkspaceService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

func (s *WorkspaceService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *WorkspaceService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMemberRepo = repo
}

func (s *WorkspaceService) SetIdentityRepositories(accountRepo repository.AccountRepository, membershipRepo repository.WorkspaceMembershipRepository) {
	s.accountRepo = accountRepo
	s.membershipRepo = membershipRepo
}

func (s *WorkspaceService) WorkspaceInfo(ctx context.Context, workspaceID string) (*domain.Workspace, error) {
	resolved, err := resolveWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, resolved)
}

func (s *WorkspaceService) WorkspacePreferences(ctx context.Context, workspaceID string) (map[string]any, error) {
	ws, err := s.WorkspaceInfo(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return decodeWorkspacePreferences(ws.Preferences)
}

func (s *WorkspaceService) WorkspaceBillingInfo(ctx context.Context, workspaceID string) (*domain.WorkspaceBilling, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ws, err := s.repo.Get(ctx, resolved)
	if err != nil {
		return nil, err
	}
	return &ws.Billing, nil
}

func (s *WorkspaceService) WorkspaceBillableInfo(ctx context.Context, workspaceID string) (map[string]domain.WorkspaceBillableInfo, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListBillableInfo(ctx, resolved)
	if err != nil {
		return nil, err
	}
	resp := make(map[string]domain.WorkspaceBillableInfo, len(rows))
	for _, row := range rows {
		resp[row.UserID] = row
	}
	return resp, nil
}

func (s *WorkspaceService) WorkspaceAccessLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceAccessLog, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListAccessLogs(ctx, resolved, limit)
}

func (s *WorkspaceService) WorkspaceIntegrationLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceIntegrationLog, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListIntegrationLogs(ctx, resolved, limit)
}

func (s *WorkspaceService) WorkspaceAuthorizationAuditLogs(ctx context.Context, workspaceID string, limit int) ([]domain.AuthorizationAuditLog, error) {
	if s.auditRepo == nil {
		return []domain.AuthorizationAuditLog{}, nil
	}
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.auditRepo.List(ctx, domain.ListAuthorizationAuditLogsParams{
		WorkspaceID: resolved,
		Limit:       limit,
	})
}

func (s *WorkspaceService) TeamExternalWorkspaces(ctx context.Context, workspaceID string) ([]domain.ExternalWorkspace, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListExternalWorkspaces(ctx, resolved)
}

func (s *WorkspaceService) CreateExternalWorkspace(ctx context.Context, workspaceID string, params domain.CreateExternalWorkspaceParams) (*domain.ExternalWorkspace, error) {
	if strings.TrimSpace(params.ExternalWorkspaceID) == "" {
		return nil, fmt.Errorf("external_workspace_id: %w", domain.ErrInvalidArgument)
	}
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = resolved
	params.ExternalWorkspaceID = strings.TrimSpace(params.ExternalWorkspaceID)
	params.Name = strings.TrimSpace(params.Name)
	params.ConnectionType = strings.TrimSpace(params.ConnectionType)
	return s.repo.CreateExternalWorkspace(ctx, params)
}

func (s *WorkspaceService) DisconnectExternalWorkspace(ctx context.Context, workspaceID, externalWorkspaceID string) error {
	if externalWorkspaceID == "" {
		return fmt.Errorf("external_workspace_id: %w", domain.ErrInvalidArgument)
	}
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return err
	}
	if err := s.repo.DisconnectExternalWorkspace(ctx, resolved, externalWorkspaceID); err != nil {
		return err
	}
	if s.externalMemberRepo != nil {
		if err := s.externalMemberRepo.RevokeByExternalWorkspace(ctx, resolved, externalWorkspaceID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkspaceService) TransferPrimaryAdmin(ctx context.Context, workspaceID, newPrimaryAdminID string) (*domain.User, error) {
	if newPrimaryAdminID == "" {
		return nil, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	actor, err := requirePrimaryAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	target, err := loadUserWithMembership(ctx, s.userRepo, s.membershipRepo, newPrimaryAdminID)
	if err != nil {
		return nil, err
	}
	if target.WorkspaceID != resolved || target.PrincipalType != domain.PrincipalTypeHuman {
		return nil, domain.ErrForbidden
	}
	if actor.ID == target.ID && actor.WorkspaceID == resolved {
		return target, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txUsers := s.userRepo.WithTx(tx)
	adminType := domain.AccountTypeAdmin
	primaryType := domain.AccountTypePrimaryAdmin

	previousPrimary, err := txUsers.Update(ctx, actor.ID, domain.UpdateUserParams{AccountType: &adminType})
	if err != nil {
		return nil, err
	}
	nextPrimary, err := txUsers.Update(ctx, target.ID, domain.UpdateUserParams{AccountType: &primaryType})
	if err != nil {
		return nil, err
	}
	if s.membershipRepo != nil {
		txMemberships := s.membershipRepo.WithTx(tx)
		if _, err := txMemberships.UpdateAccountTypeByLegacyUserID(ctx, previousPrimary.ID, previousPrimary.EffectiveAccountType()); err != nil && err != domain.ErrNotFound {
			return nil, fmt.Errorf("sync previous primary membership account type: %w", err)
		}
		if _, err := txMemberships.UpdateAccountTypeByLegacyUserID(ctx, nextPrimary.ID, nextPrimary.EffectiveAccountType()); err != nil && err != domain.ErrNotFound {
			return nil, fmt.Errorf("sync next primary membership account type: %w", err)
		}
	}

	for _, user := range []*domain.User{previousPrimary, nextPrimary} {
		payload, _ := json.Marshal(user)
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventUserUpdated,
			AggregateType: domain.AggregateUser,
			AggregateID:   user.ID,
			WorkspaceID:   user.WorkspaceID,
			ActorID:       actor.ID,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record user.updated event: %w", err)
		}
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, resolved, domain.AuditActionPrimaryAdminTransferred, "workspace", resolved, map[string]any{
		"from_user_id": actor.ID,
		"to_user_id":   target.ID,
	}); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return nextPrimary, nil
}

func (s *WorkspaceService) AdminCreate(ctx context.Context, params domain.CreateWorkspaceParams) (*domain.Workspace, error) {
	actor, err := s.requireWorkspaceAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	if params.Discoverability == "" {
		params.Discoverability = domain.WorkspaceDiscoverabilityInviteOnly
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ws, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(ws)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventWorkspaceCreated,
		AggregateType: domain.AggregateWorkspace,
		AggregateID:   ws.ID,
		WorkspaceID:   ws.ID,
		ActorID:       compatibilityActorID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record workspace.created event: %w", err)
	}
	createdUser, err := s.userRepo.WithTx(tx).Create(ctx, domain.CreateUserParams{
		WorkspaceID:   ws.ID,
		Name:          actor.Name,
		RealName:      actor.RealName,
		DisplayName:   actor.DisplayName,
		Email:         actor.Email,
		PrincipalType: actor.PrincipalType,
		OwnerID:       actor.OwnerID,
		AccountType:   domain.AccountTypePrimaryAdmin,
		IsBot:         actor.IsBot,
		Profile:       actor.Profile,
	})
	if err != nil {
		return nil, fmt.Errorf("create creator user: %w", err)
	}
	if s.accountRepo != nil && s.membershipRepo != nil {
		accountRepo := s.accountRepo
		membershipRepo := s.membershipRepo
		if accountRepo != nil {
			accountRepo = accountRepo.WithTx(tx)
		}
		if membershipRepo != nil {
			membershipRepo = membershipRepo.WithTx(tx)
		}
		if _, _, err := syncIdentityForUser(ctx, accountRepo, membershipRepo, createdUser); err != nil {
			return nil, fmt.Errorf("sync creator identity: %w", err)
		}
	}
	userPayload, _ := json.Marshal(createdUser)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   createdUser.ID,
		WorkspaceID:   createdUser.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
		Payload:       userPayload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return ws, nil
}

func (s *WorkspaceService) AdminList(ctx context.Context) ([]domain.Workspace, error) {
	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil || actor.PrincipalType != domain.PrincipalTypeHuman || strings.TrimSpace(actor.Email) == "" {
		resolved, resolveErr := s.resolveAdminTargetWorkspaceID(ctx, "")
		if resolveErr != nil {
			return nil, resolveErr
		}
		workspace, getErr := s.repo.Get(ctx, resolved)
		if getErr != nil {
			return nil, getErr
		}
		return []domain.Workspace{*workspace}, nil
	}

	if s.membershipRepo != nil {
		accountID := ctxutil.GetAccountID(ctx)
		if accountID != "" {
			memberships, membershipErr := s.membershipRepo.ListByAccount(ctx, accountID)
			if membershipErr != nil {
				return nil, fmt.Errorf("list workspace memberships: %w", membershipErr)
			}
			workspaces := make([]domain.Workspace, 0, len(memberships))
			seen := make(map[string]struct{}, len(memberships))
			for _, membership := range memberships {
				if membership.WorkspaceID == "" {
					continue
				}
				if _, ok := seen[membership.WorkspaceID]; ok {
					continue
				}
				workspace, getErr := s.repo.Get(ctx, membership.WorkspaceID)
				if getErr != nil {
					return nil, getErr
				}
				workspaces = append(workspaces, *workspace)
				seen[membership.WorkspaceID] = struct{}{}
			}
			if len(workspaces) > 0 {
				return workspaces, nil
			}
		}
	}
	return nil, domain.ErrForbidden
}

func (s *WorkspaceService) AdminListAdmins(ctx context.Context, workspaceID string) ([]domain.User, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListAdmins(ctx, resolved)
}

func (s *WorkspaceService) AdminListOwners(ctx context.Context, workspaceID string) ([]domain.User, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListOwners(ctx, resolved)
}

func (s *WorkspaceService) AdminSettingsInfo(ctx context.Context, workspaceID string) (*domain.Workspace, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, resolved)
}

func (s *WorkspaceService) AdminSetName(ctx context.Context, workspaceID, name string) (*domain.Workspace, error) {
	if name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	return s.updateWorkspace(ctx, workspaceID, domain.UpdateWorkspaceParams{Name: &name})
}

func (s *WorkspaceService) AdminSetDescription(ctx context.Context, workspaceID, description string) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, workspaceID, domain.UpdateWorkspaceParams{Description: &description})
}

func (s *WorkspaceService) AdminSetDiscoverability(ctx context.Context, workspaceID string, discoverability domain.WorkspaceDiscoverability) (*domain.Workspace, error) {
	if discoverability != domain.WorkspaceDiscoverabilityOpen && discoverability != domain.WorkspaceDiscoverabilityInviteOnly {
		return nil, fmt.Errorf("discoverability: %w", domain.ErrInvalidArgument)
	}
	return s.updateWorkspace(ctx, workspaceID, domain.UpdateWorkspaceParams{Discoverability: &discoverability})
}

func (s *WorkspaceService) AdminSetIcon(ctx context.Context, workspaceID string, icon domain.WorkspaceIcon) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, workspaceID, domain.UpdateWorkspaceParams{Icon: &icon})
}

func (s *WorkspaceService) AdminSetDefaultChannels(ctx context.Context, workspaceID string, channels []string) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, workspaceID, domain.UpdateWorkspaceParams{DefaultChannels: &channels})
}

func (s *WorkspaceService) Update(ctx context.Context, workspaceID string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, workspaceID, params)
}

func (s *WorkspaceService) updateWorkspace(ctx context.Context, workspaceID string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error) {
	resolved, err := s.resolveAdminTargetWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ws, err := s.repo.WithTx(tx).Update(ctx, resolved, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(ws)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventWorkspaceUpdated,
		AggregateType: domain.AggregateWorkspace,
		AggregateID:   ws.ID,
		WorkspaceID:   ws.ID,
		ActorID:       compatibilityActorID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record workspace.updated event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return ws, nil
}

func (s *WorkspaceService) requireWorkspaceAdmin(ctx context.Context) (*domain.User, error) {
	return requireWorkspaceAdminActor(ctx, s.userRepo)
}

func (s *WorkspaceService) resolveAdminTargetWorkspaceID(ctx context.Context, requested string) (string, error) {
	if _, err := s.requireWorkspaceAdmin(ctx); err != nil {
		return "", err
	}
	return resolveWorkspaceID(ctx, requested)
}

func decodeWorkspacePreferences(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var prefs map[string]any
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return nil, fmt.Errorf("decode preferences: %w", err)
	}
	if prefs == nil {
		prefs = map[string]any{}
	}
	return prefs, nil
}
