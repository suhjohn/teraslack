package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type Authorizer struct{}

var defaultAuthorizer = Authorizer{}

func resolveWorkspaceID(ctx context.Context, requested string) (string, error) {
	if ctxWorkspace := ctxutil.GetWorkspaceID(ctx); ctxWorkspace != "" {
		if requested != "" && requested != ctxWorkspace {
			return "", domain.ErrForbidden
		}
		return ctxWorkspace, nil
	}
	if requested != "" {
		return requested, nil
	}
	return "", fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
}

func resolveActorID(ctx context.Context, requested string) (string, error) {
	return requireActorUserID(ctx, requested, "user_id")
}

func ensureWorkspaceAccess(ctx context.Context, resourceWorkspaceID string) error {
	if resourceWorkspaceID == "" {
		return nil
	}
	if ctxWorkspace := ctxutil.GetWorkspaceID(ctx); ctxWorkspace != "" && ctxWorkspace != resourceWorkspaceID {
		return domain.ErrForbidden
	}
	return nil
}

func requirePermission(ctx context.Context, permission string) error {
	if permission == "" {
		return nil
	}
	if ctxutil.GetAPIKeyID(ctx) == "" {
		return nil
	}
	perms := ctxutil.GetPermissions(ctx)
	for _, candidate := range perms {
		if candidate == "*" || candidate == permission {
			return nil
		}
		if strings.HasSuffix(candidate, ".*") {
			prefix := strings.TrimSuffix(candidate, "*")
			if strings.HasPrefix(permission, prefix) {
				return nil
			}
		}
	}
	return domain.ErrForbidden
}

func requiresAuthenticatedActor(ctx context.Context) bool {
	return actorFromContext(ctx).IsAuthenticated()
}

func isInternalCallWithoutAuth(ctx context.Context) bool {
	return !requiresAuthenticatedActor(ctx)
}

func isExternalWorkspaceParticipant(ctx context.Context) bool {
	return ctxutil.GetAccountID(ctx) != "" && ctxutil.GetUserID(ctx) == ""
}

func hasWorkspaceUserContext(ctx context.Context, workspaceID string) bool {
	if ctxutil.GetUserID(ctx) == "" && ctxutil.GetWorkspaceMembershipID(ctx) == "" {
		return false
	}
	if workspaceID == "" {
		return true
	}
	return ctxutil.GetWorkspaceID(ctx) == workspaceID
}

func authenticatedAccountType(ctx context.Context) domain.AccountType {
	return ctxutil.GetAccountType(ctx)
}

func authenticatedPrincipalType(ctx context.Context) domain.PrincipalType {
	return ctxutil.GetPrincipalType(ctx)
}

func contextIsWorkspaceAdmin(ctx context.Context) bool {
	return defaultAuthorizer.IsWorkspaceAdminAccount(authenticatedAccountType(ctx))
}

func loadActingUser(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	actorIdentity := actorFromContext(ctx)
	if actorIdentity.UserID == "" {
		if actor, ok := syntheticSystemActor(ctx); ok {
			return actor, nil
		}
		if actor := actorIdentity.syntheticUser(); actor != nil {
			return actor, nil
		}
		return nil, domain.ErrForbidden
	}
	if userRepo == nil {
		if actor := actorIdentity.syntheticUser(); actor != nil {
			return actor, nil
		}
		return nil, domain.ErrForbidden
	}
	actor, err := userRepo.Get(ctx, actorIdentity.UserID)
	if err != nil {
		if err != domain.ErrNotFound || actorIdentity.AccountID == "" {
			return nil, err
		}
		actor = actorIdentity.syntheticUser()
		if actor == nil {
			return nil, domain.ErrForbidden
		}
		if workspaceID := ctxutil.GetWorkspaceID(ctx); workspaceID != "" {
			actor.WorkspaceID = workspaceID
		}
		if accountID := ctxutil.GetAccountID(ctx); accountID != "" {
			actor.AccountID = accountID
		}
		if accountType := ctxutil.GetAccountType(ctx); accountType != "" {
			actor.AccountType = accountType
		}
		if principalType := ctxutil.GetPrincipalType(ctx); principalType != "" {
			actor.PrincipalType = principalType
		}
		if ctxutil.GetIsBot(ctx) {
			actor.IsBot = true
		}
		return actor, nil
	}
	if workspaceID := ctxutil.GetWorkspaceID(ctx); workspaceID != "" {
		actor.WorkspaceID = workspaceID
	}
	if strings.TrimSpace(actor.AccountID) == "" {
		if accountID := ctxutil.GetAccountID(ctx); accountID != "" {
			actor.AccountID = accountID
		}
	}
	return actor, nil
}

func syntheticSystemActor(ctx context.Context) (*domain.User, bool) {
	if ctxutil.GetPrincipalType(ctx) != domain.PrincipalTypeSystem {
		return nil, false
	}
	workspaceID := ctxutil.GetWorkspaceID(ctx)
	if workspaceID == "" {
		return nil, false
	}
	return &domain.User{
		WorkspaceID:   workspaceID,
		PrincipalType: domain.PrincipalTypeSystem,
		AccountType:   domain.AccountTypePrimaryAdmin,
		IsBot:         true,
	}, true
}

func requireWorkspaceAdminActor(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	return defaultAuthorizer.RequireWorkspaceAdminActor(ctx, userRepo)
}

func requirePrimaryAdminActor(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	return defaultAuthorizer.RequirePrimaryAdminActor(ctx, userRepo)
}

func isSelfAction(ctx context.Context, userID string) bool {
	return userID != "" && actorUserID(ctx) == userID
}

func canSelfUpdateUser(params domain.UpdateUserParams) bool {
	return defaultAuthorizer.CanSelfUpdateUser(params)
}

func canManagePrincipal(actor, target *domain.User) bool {
	return defaultAuthorizer.CanManagePrincipal(actor, target)
}

func canAssignAccountType(actor *domain.User, principalType domain.PrincipalType, accountType domain.AccountType) bool {
	return defaultAuthorizer.CanAssignAccountType(actor, principalType, accountType)
}

func (Authorizer) IsWorkspaceAdminAccount(accountType domain.AccountType) bool {
	switch accountType {
	case domain.AccountTypePrimaryAdmin, domain.AccountTypeAdmin:
		return true
	default:
		return false
	}
}

func (a Authorizer) RequireWorkspaceAdminActor(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	actor, err := loadActingUser(ctx, userRepo)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, actor.WorkspaceID); err != nil {
		return nil, err
	}
	if actor.PrincipalType == domain.PrincipalTypeSystem {
		return actor, nil
	}
	if !a.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
		return nil, domain.ErrForbidden
	}
	return actor, nil
}

func (a Authorizer) RequirePrimaryAdminActor(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	actor, err := a.RequireWorkspaceAdminActor(ctx, userRepo)
	if err != nil {
		return nil, err
	}
	if actor.PrincipalType == domain.PrincipalTypeSystem {
		return actor, nil
	}
	if actor.EffectiveAccountType() != domain.AccountTypePrimaryAdmin {
		return nil, domain.ErrForbidden
	}
	return actor, nil
}

func (Authorizer) CanSelfUpdateUser(params domain.UpdateUserParams) bool {
	return params.AccountType == nil && params.Deleted == nil
}

func (a Authorizer) CanManagePrincipal(actor, target *domain.User) bool {
	if actor == nil || target == nil {
		return false
	}
	if actor.PrincipalType == domain.PrincipalTypeSystem {
		return true
	}
	switch actor.EffectiveAccountType() {
	case domain.AccountTypePrimaryAdmin:
		return true
	case domain.AccountTypeAdmin:
		if target.PrincipalType != domain.PrincipalTypeHuman {
			return true
		}
		return target.EffectiveAccountType() == domain.AccountTypeMember
	default:
		return false
	}
}

func (Authorizer) CanAssignAccountType(actor *domain.User, principalType domain.PrincipalType, accountType domain.AccountType) bool {
	if actor == nil {
		return false
	}
	if actor.PrincipalType == domain.PrincipalTypeSystem {
		return true
	}
	if principalType != domain.PrincipalTypeHuman {
		return accountType == domain.AccountTypeNone
	}
	switch actor.EffectiveAccountType() {
	case domain.AccountTypePrimaryAdmin:
		return accountType == domain.AccountTypeAdmin || accountType == domain.AccountTypeMember
	case domain.AccountTypeAdmin:
		return accountType == domain.AccountTypeMember
	default:
		return false
	}
}
