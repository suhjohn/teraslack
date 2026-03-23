package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// WorkspaceService contains business logic for Slack-style team APIs.
type WorkspaceService struct {
	repo      repository.WorkspaceRepository
	userRepo  repository.UserRepository
	auditRepo repository.AuthorizationAuditRepository
	recorder  EventRecorder
	db        repository.TxBeginner
	logger    *slog.Logger
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

func (s *WorkspaceService) TeamInfo(ctx context.Context, teamID string) (*domain.Workspace, error) {
	resolved, err := resolveTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, resolved)
}

func (s *WorkspaceService) TeamPreferences(ctx context.Context, teamID string) (map[string]any, error) {
	ws, err := s.TeamInfo(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return decodeWorkspacePreferences(ws.Preferences)
}

func (s *WorkspaceService) TeamProfile(ctx context.Context, teamID string) ([]domain.WorkspaceProfileField, error) {
	ws, err := s.TeamInfo(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return ws.ProfileFields, nil
}

func (s *WorkspaceService) TeamBillingInfo(ctx context.Context, teamID string) (*domain.WorkspaceBilling, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	ws, err := s.repo.Get(ctx, resolved)
	if err != nil {
		return nil, err
	}
	return &ws.Billing, nil
}

func (s *WorkspaceService) TeamBillableInfo(ctx context.Context, teamID string) (map[string]domain.WorkspaceBillableInfo, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
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

func (s *WorkspaceService) TeamAccessLogs(ctx context.Context, teamID string, limit int) ([]domain.WorkspaceAccessLog, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListAccessLogs(ctx, resolved, limit)
}

func (s *WorkspaceService) TeamIntegrationLogs(ctx context.Context, teamID string, limit int) ([]domain.WorkspaceIntegrationLog, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListIntegrationLogs(ctx, resolved, limit)
}

func (s *WorkspaceService) TeamAuthorizationAuditLogs(ctx context.Context, teamID string, limit int) ([]domain.AuthorizationAuditLog, error) {
	if s.auditRepo == nil {
		return []domain.AuthorizationAuditLog{}, nil
	}
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.auditRepo.List(ctx, domain.ListAuthorizationAuditLogsParams{
		TeamID: resolved,
		Limit:  limit,
	})
}

func (s *WorkspaceService) TeamExternalTeams(ctx context.Context, teamID string) ([]domain.ExternalTeam, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListExternalTeams(ctx, resolved)
}

func (s *WorkspaceService) DisconnectExternalTeam(ctx context.Context, teamID, externalTeamID string) error {
	if externalTeamID == "" {
		return fmt.Errorf("external_team_id: %w", domain.ErrInvalidArgument)
	}
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return err
	}
	return s.repo.DisconnectExternalTeam(ctx, resolved, externalTeamID)
}

func (s *WorkspaceService) TransferPrimaryAdmin(ctx context.Context, teamID, newPrimaryAdminID string) (*domain.User, error) {
	if newPrimaryAdminID == "" {
		return nil, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	actor, err := requirePrimaryAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	target, err := s.userRepo.Get(ctx, newPrimaryAdminID)
	if err != nil {
		return nil, err
	}
	if target.TeamID != resolved || target.PrincipalType != domain.PrincipalTypeHuman {
		return nil, domain.ErrForbidden
	}
	if actor.ID == target.ID && actor.TeamID == resolved {
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

	for _, user := range []*domain.User{previousPrimary, nextPrimary} {
		payload, _ := json.Marshal(user)
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventUserUpdated,
			AggregateType: domain.AggregateUser,
			AggregateID:   user.ID,
			TeamID:        user.TeamID,
			ActorID:       actor.ID,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record user.updated event: %w", err)
		}
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, resolved, domain.AuditActionPrimaryAdminTransferred, "team", resolved, map[string]any{
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
	if _, err := s.requireWorkspaceAdmin(ctx); err != nil {
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
		EventType:     domain.EventTeamCreated,
		AggregateType: domain.AggregateTeam,
		AggregateID:   ws.ID,
		TeamID:        ws.ID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record team.created event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return ws, nil
}

func (s *WorkspaceService) AdminList(ctx context.Context) ([]domain.Workspace, error) {
	if _, err := s.requireWorkspaceAdmin(ctx); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *WorkspaceService) AdminListAdmins(ctx context.Context, teamID string) ([]domain.User, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListAdmins(ctx, resolved)
}

func (s *WorkspaceService) AdminListOwners(ctx context.Context, teamID string) ([]domain.User, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListOwners(ctx, resolved)
}

func (s *WorkspaceService) AdminSettingsInfo(ctx context.Context, teamID string) (*domain.Workspace, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, resolved)
}

func (s *WorkspaceService) AdminSetName(ctx context.Context, teamID, name string) (*domain.Workspace, error) {
	if name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	return s.updateWorkspace(ctx, teamID, domain.UpdateWorkspaceParams{Name: &name})
}

func (s *WorkspaceService) AdminSetDescription(ctx context.Context, teamID, description string) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, teamID, domain.UpdateWorkspaceParams{Description: &description})
}

func (s *WorkspaceService) AdminSetDiscoverability(ctx context.Context, teamID string, discoverability domain.WorkspaceDiscoverability) (*domain.Workspace, error) {
	if discoverability != domain.WorkspaceDiscoverabilityOpen && discoverability != domain.WorkspaceDiscoverabilityInviteOnly {
		return nil, fmt.Errorf("discoverability: %w", domain.ErrInvalidArgument)
	}
	return s.updateWorkspace(ctx, teamID, domain.UpdateWorkspaceParams{Discoverability: &discoverability})
}

func (s *WorkspaceService) AdminSetIcon(ctx context.Context, teamID string, icon domain.WorkspaceIcon) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, teamID, domain.UpdateWorkspaceParams{Icon: &icon})
}

func (s *WorkspaceService) AdminSetDefaultChannels(ctx context.Context, teamID string, channels []string) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, teamID, domain.UpdateWorkspaceParams{DefaultChannels: &channels})
}

func (s *WorkspaceService) Update(ctx context.Context, teamID string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error) {
	return s.updateWorkspace(ctx, teamID, params)
}

func (s *WorkspaceService) updateWorkspace(ctx context.Context, teamID string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error) {
	resolved, err := s.resolveAdminTargetTeamID(ctx, teamID)
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
		EventType:     domain.EventTeamUpdated,
		AggregateType: domain.AggregateTeam,
		AggregateID:   ws.ID,
		TeamID:        ws.ID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record team.updated event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return ws, nil
}

func (s *WorkspaceService) requireWorkspaceAdmin(ctx context.Context) (*domain.User, error) {
	return requireWorkspaceAdminActor(ctx, s.userRepo)
}

func (s *WorkspaceService) resolveAdminTargetTeamID(ctx context.Context, requested string) (string, error) {
	if _, err := s.requireWorkspaceAdmin(ctx); err != nil {
		return "", err
	}
	if requested != "" {
		return requested, nil
	}
	if teamID := ctxutil.GetTeamID(ctx); teamID != "" {
		return teamID, nil
	}
	return "", fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
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
