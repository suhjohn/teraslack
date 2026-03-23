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

// UserService contains business logic for user operations.
type UserService struct {
	repo           repository.UserRepository
	externalAccess repository.ExternalPrincipalAccessRepository
	auditRepo      repository.AuthorizationAuditRepository
	recorder       EventRecorder
	db             repository.TxBeginner
	logger         *slog.Logger
}

// NewUserService creates a new UserService.
func NewUserService(repo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *UserService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &UserService{repo: repo, recorder: recorder, db: db, logger: logger}
}

func (s *UserService) SetExternalAccessRepository(repo repository.ExternalPrincipalAccessRepository) {
	s.externalAccess = repo
}

func (s *UserService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *UserService) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	if err := requirePermission(ctx, domain.PermissionUsersCreate); err != nil {
		return nil, err
	}
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	if params.Name == "" {
		return nil, fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	if params.PrincipalType == "" {
		return nil, fmt.Errorf("principal_type: %w", domain.ErrInvalidArgument)
	}
	if params.PrincipalType == domain.PrincipalTypeHuman && params.AccountType == domain.AccountTypePrimaryAdmin && requiresAuthenticatedActor(ctx) {
		return nil, domain.ErrForbidden
	}
	if err := validateAccountType(params.PrincipalType, params.AccountType); err != nil {
		return nil, err
	}
	if requiresAuthenticatedActor(ctx) {
		actor, err := requireWorkspaceAdminActor(ctx, s.repo)
		if err != nil {
			return nil, err
		}
		effectiveAccountType := domain.NormalizeAccountType(params.PrincipalType, params.AccountType)
		if !canAssignAccountType(actor, params.PrincipalType, effectiveAccountType) {
			return nil, domain.ErrForbidden
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	user, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return user, nil
}

func (s *UserService) Get(ctx context.Context, id string) (*domain.User, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	if external, err := isExternalSharedActor(ctx, s.externalAccess); err != nil {
		return nil, err
	} else if external {
		return nil, domain.ErrForbidden
	}
	user, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, user.TeamID); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if email == "" {
		return nil, fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}
	if external, err := isExternalSharedActor(ctx, s.externalAccess); err != nil {
		return nil, err
	} else if external {
		return nil, domain.ErrForbidden
	}
	teamID := ctxutil.GetTeamID(ctx)
	if teamID == "" {
		return nil, domain.ErrForbidden
	}
	user, err := s.repo.GetByTeamEmail(ctx, teamID, email)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}

	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, existing.TeamID); err != nil {
		return nil, err
	}
	if params.AccountType != nil {
		if err := validateAccountType(existing.PrincipalType, *params.AccountType); err != nil {
			return nil, err
		}
		if *params.AccountType == domain.AccountTypePrimaryAdmin && requiresAuthenticatedActor(ctx) {
			return nil, domain.ErrForbidden
		}
	}
	if existing.EffectiveAccountType() == domain.AccountTypePrimaryAdmin && params.AccountType != nil && *params.AccountType != domain.AccountTypePrimaryAdmin {
		return nil, domain.ErrForbidden
	}
	if requiresAuthenticatedActor(ctx) {
		if isSelfAction(ctx, existing.ID) && canSelfUpdateUser(params) {
			// Self-service profile edits are allowed.
		} else {
			actor, err := requireWorkspaceAdminActor(ctx, s.repo)
			if err != nil {
				return nil, err
			}
			if !canManagePrincipal(actor, existing) {
				return nil, domain.ErrForbidden
			}
			if !canAssignAccountType(actor, existing.PrincipalType, desiredAccountType(existing, params)) {
				return nil, domain.ErrForbidden
			}
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	user, err := s.repo.WithTx(tx).Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserUpdated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if existing.EffectiveAccountType() != user.EffectiveAccountType() {
		if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, user.TeamID, domain.AuditActionAccountTypeUpdated, "user", user.ID, map[string]any{
			"before": existing.EffectiveAccountType(),
			"after":  user.EffectiveAccountType(),
		}); err != nil {
			return nil, fmt.Errorf("record authorization audit log: %w", err)
		}
	}
	return user, nil
}

func (s *UserService) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if external, err := isExternalSharedActor(ctx, s.externalAccess); err != nil {
		return nil, err
	} else if external {
		return nil, domain.ErrForbidden
	}
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	return s.repo.List(ctx, params)
}

func validateAccountType(principalType domain.PrincipalType, accountType domain.AccountType) error {
	if accountType == "" {
		return nil
	}
	if principalType != domain.PrincipalTypeHuman {
		return fmt.Errorf("account_type: %w", domain.ErrInvalidArgument)
	}
	switch accountType {
	case domain.AccountTypePrimaryAdmin, domain.AccountTypeAdmin, domain.AccountTypeMember:
		return nil
	default:
		return fmt.Errorf("account_type: %w", domain.ErrInvalidArgument)
	}
}

func desiredAccountType(existing *domain.User, params domain.UpdateUserParams) domain.AccountType {
	if existing == nil || existing.PrincipalType != domain.PrincipalTypeHuman {
		return domain.AccountTypeNone
	}
	if params.AccountType != nil {
		return *params.AccountType
	}
	return existing.EffectiveAccountType()
}
