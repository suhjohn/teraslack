package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type RoleService struct {
	repo      repository.RoleAssignmentRepository
	userRepo  repository.UserRepository
	auditRepo repository.AuthorizationAuditRepository
	recorder  EventRecorder
	db        repository.TxBeginner
	logger    *slog.Logger
}

func NewRoleService(repo repository.RoleAssignmentRepository, userRepo repository.UserRepository) *RoleService {
	return &RoleService{repo: repo, userRepo: userRepo, recorder: noopRecorder{}}
}

func (s *RoleService) SetRecorder(recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	s.recorder = recorder
	s.db = db
	s.logger = logger
}

func (s *RoleService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *RoleService) ListUserRoles(ctx context.Context, userID string) ([]domain.DelegatedRole, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	target, err := s.userRepo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, target.TeamID); err != nil {
		return nil, err
	}
	if err := s.authorizeRoleRead(ctx, target); err != nil {
		return nil, err
	}
	return s.repo.ListByUser(ctx, target.TeamID, target.ID)
}

func (s *RoleService) SetUserRoles(ctx context.Context, userID string, roles []domain.DelegatedRole) ([]domain.DelegatedRole, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
	}
	target, err := s.userRepo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, target.TeamID); err != nil {
		return nil, err
	}
	if target.PrincipalType != domain.PrincipalTypeHuman {
		return nil, fmt.Errorf("roles: %w", domain.ErrInvalidArgument)
	}
	for _, role := range roles {
		if !domain.IsValidDelegatedRole(role) {
			return nil, fmt.Errorf("delegated_roles: %w", domain.ErrInvalidArgument)
		}
	}
	if err := s.authorizeRoleWrite(ctx, target); err != nil {
		return nil, err
	}
	before, err := s.repo.ListByUser(ctx, target.TeamID, target.ID)
	if err != nil {
		return nil, err
	}
	assignedBy := target.ID
	if requiresAuthenticatedActor(ctx) {
		if actor, err := loadActingUser(ctx, s.userRepo); err == nil && actor != nil {
			assignedBy = actor.ID
		}
	}
	if s.db == nil {
		if err := s.repo.ReplaceForUser(ctx, target.TeamID, target.ID, roles, assignedBy); err != nil {
			return nil, err
		}
	} else {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)
		if err := s.repo.WithTx(tx).ReplaceForUser(ctx, target.TeamID, target.ID, roles, assignedBy); err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(map[string]any{
			"user_id":         target.ID,
			"delegated_roles": roles,
		})
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventUserRolesUpdated,
			AggregateType: domain.AggregateUser,
			AggregateID:   target.ID,
			TeamID:        target.TeamID,
			ActorID:       assignedBy,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record user.roles_updated event: %w", err)
		}
		if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, target.TeamID, domain.AuditActionDelegatedRolesUpdated, "user", target.ID, map[string]any{
			"before": before,
			"after":  roles,
		}); err != nil {
			return nil, fmt.Errorf("record authorization audit log: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}
	return s.repo.ListByUser(ctx, target.TeamID, target.ID)
}

func (s *RoleService) authorizeRoleRead(ctx context.Context, target *domain.User) error {
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil {
		return err
	}
	if actor.ID == target.ID {
		return nil
	}
	if canManagePrincipal(actor, target) {
		return nil
	}
	if hasDelegatedRole(ctx, s.repo, actor, domain.DelegatedRoleRolesAdmin) {
		return nil
	}
	return domain.ErrForbidden
}

func (s *RoleService) authorizeRoleWrite(ctx context.Context, target *domain.User) error {
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil {
		return err
	}
	if actor.EffectiveAccountType() == domain.AccountTypePrimaryAdmin && canManagePrincipal(actor, target) {
		return nil
	}
	if hasDelegatedRole(ctx, s.repo, actor, domain.DelegatedRoleRolesAdmin) && canManagePrincipal(actor, target) {
		return nil
	}
	return domain.ErrForbidden
}

func hasDelegatedRole(ctx context.Context, repo repository.RoleAssignmentRepository, actor *domain.User, role domain.DelegatedRole) bool {
	if actor == nil || repo == nil {
		return false
	}
	roles, err := repo.ListByUser(ctx, actor.TeamID, actor.ID)
	if err != nil {
		return false
	}
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}
