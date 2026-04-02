package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// UserService contains business logic for user operations.
type UserService struct {
	repo           repository.UserRepository
	accountRepo    repository.AccountRepository
	membershipRepo repository.WorkspaceMembershipRepository
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

func (s *UserService) SetIdentityRepositories(accountRepo repository.AccountRepository, membershipRepo repository.WorkspaceMembershipRepository) {
	s.accountRepo = accountRepo
	s.membershipRepo = membershipRepo
}

func (s *UserService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *UserService) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	if err := requirePermission(ctx, domain.PermissionUsersCreate); err != nil {
		return nil, err
	}
	workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
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
		actor, err := loadActingUser(ctx, s.repo)
		if err != nil {
			return nil, err
		}
		if err := ensureWorkspaceAccess(ctx, actor.WorkspaceID); err != nil {
			return nil, err
		}
		if !allowSelfOwnedAgentCreate(actor, &params) {
			if !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
				return nil, domain.ErrForbidden
			}
			effectiveAccountType := domain.NormalizeAccountType(params.PrincipalType, params.AccountType)
			if !canAssignAccountType(actor, params.PrincipalType, effectiveAccountType) {
				return nil, domain.ErrForbidden
			}
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
	if s.accountRepo != nil && s.membershipRepo != nil {
		if _, _, err := syncIdentityForUser(ctx, s.accountRepo.WithTx(tx), s.membershipRepo.WithTx(tx), user); err != nil {
			return nil, fmt.Errorf("sync identity for user: %w", err)
		}
	}
	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		WorkspaceID:   user.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
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
	if isExternalWorkspaceParticipant(ctx) {
		return nil, domain.ErrForbidden
	}
	if s.accountRepo != nil && s.membershipRepo != nil {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		memberRepo := s.membershipRepo.WithTx(tx)
		membership, err := memberRepo.GetByLegacyUserID(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := ensureWorkspaceAccess(ctx, membership.WorkspaceID); err != nil {
			return nil, err
		}

		user, _, err := loadMembershipBackedUser(ctx, tx, s.repo.WithTx(tx), s.accountRepo.WithTx(tx), memberRepo, membership, s.recorder)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
		return user, nil
	}
	user, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, user.WorkspaceID); err != nil {
		return nil, err
	}
	return decorateUserWithMembership(ctx, s.membershipRepo, user)
}

func (s *UserService) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if email == "" {
		return nil, fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}
	if isExternalWorkspaceParticipant(ctx) {
		return nil, domain.ErrForbidden
	}
	workspaceID := ctxutil.GetWorkspaceID(ctx)
	if workspaceID == "" {
		return nil, domain.ErrForbidden
	}
	if s.accountRepo != nil && s.membershipRepo != nil {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		account, err := s.accountRepo.WithTx(tx).GetByEmail(ctx, email)
		if err != nil {
			return nil, err
		}
		memberRepo := s.membershipRepo.WithTx(tx)
		membership, err := memberRepo.GetByWorkspaceAndAccount(ctx, workspaceID, account.ID)
		if err != nil {
			return nil, err
		}
		user, _, err := loadMembershipBackedUser(ctx, tx, s.repo.WithTx(tx), s.accountRepo.WithTx(tx), memberRepo, membership, s.recorder)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
		return user, nil
	}
	return nil, domain.ErrNotFound
}

func (s *UserService) Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	userRepo := s.repo.WithTx(tx)
	memberRepo := s.membershipRepo
	accountRepo := s.accountRepo
	if memberRepo != nil {
		memberRepo = memberRepo.WithTx(tx)
	}
	if accountRepo != nil {
		accountRepo = accountRepo.WithTx(tx)
	}

	existing := &domain.User{}
	if accountRepo != nil && memberRepo != nil {
		membership, err := memberRepo.GetByLegacyUserID(ctx, id)
		if err != nil {
			return nil, err
		}
		existing, membership, err = loadMembershipBackedUser(ctx, tx, userRepo, accountRepo, memberRepo, membership, s.recorder)
		if err != nil {
			return nil, err
		}
		if err := ensureWorkspaceAccess(ctx, membership.WorkspaceID); err != nil {
			return nil, err
		}
	} else {
		existing, err = s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := ensureWorkspaceAccess(ctx, existing.WorkspaceID); err != nil {
			return nil, err
		}
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

	user, err := userRepo.Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	if memberRepo != nil && params.AccountType != nil {
		updatedMembership, err := memberRepo.UpdateAccountTypeByLegacyUserID(ctx, user.ID, user.EffectiveAccountType())
		if err != nil && err != domain.ErrNotFound {
			return nil, fmt.Errorf("sync membership account type: %w", err)
		}
		if updatedMembership != nil {
			user.AccountType = updatedMembership.AccountType
			user.WorkspaceID = updatedMembership.WorkspaceID
		}
	}
	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserUpdated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		WorkspaceID:   user.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if existing.EffectiveAccountType() != user.EffectiveAccountType() {
		if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, user.WorkspaceID, domain.AuditActionAccountTypeUpdated, "user", user.ID, map[string]any{
			"before": existing.EffectiveAccountType(),
			"after":  user.EffectiveAccountType(),
		}); err != nil {
			return nil, fmt.Errorf("record authorization audit log: %w", err)
		}
	}
	return user, nil
}

func (s *UserService) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	if isExternalWorkspaceParticipant(ctx) {
		return nil, domain.ErrForbidden
	}
	workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	if s.accountRepo != nil && s.membershipRepo != nil {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		memberRepo := s.membershipRepo.WithTx(tx)
		memberships, err := memberRepo.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		type listedUser struct {
			key  string
			user domain.User
		}
		listed := make([]listedUser, 0, len(memberships))
		for i := range memberships {
			user, membership, err := loadMembershipBackedUser(ctx, tx, s.repo.WithTx(tx), s.accountRepo.WithTx(tx), memberRepo, &memberships[i], s.recorder)
			if err != nil {
				return nil, err
			}
			key := user.ID
			if key == "" {
				key = membership.ID
			}
			if key == "" {
				continue
			}
			listed = append(listed, listedUser{key: key, user: *user})
		}
		sort.Slice(listed, func(i, j int) bool {
			return listed[i].key < listed[j].key
		})

		limit := params.Limit
		if limit <= 0 || limit > 200 {
			limit = 100
		}
		filtered := make([]listedUser, 0, len(listed))
		for _, item := range listed {
			if params.Cursor != "" && item.key < params.Cursor {
				continue
			}
			filtered = append(filtered, item)
		}

		page := &domain.CursorPage[domain.User]{Items: []domain.User{}}
		if len(filtered) > limit {
			page.HasMore = true
			page.NextCursor = filtered[limit].key
			filtered = filtered[:limit]
		}
		for _, item := range filtered {
			page.Items = append(page.Items, item.user)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
		return page, nil
	}

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

func allowSelfOwnedAgentCreate(actor *domain.User, params *domain.CreateUserParams) bool {
	if actor == nil || params == nil {
		return false
	}
	if actor.PrincipalType != domain.PrincipalTypeHuman {
		return false
	}
	if actor.WorkspaceID == "" || params.WorkspaceID != actor.WorkspaceID {
		return false
	}
	if params.PrincipalType != domain.PrincipalTypeAgent || !params.IsBot {
		return false
	}
	if strings.TrimSpace(params.OwnerID) == "" {
		params.OwnerID = actor.ID
	}
	return params.OwnerID == actor.ID
}
