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

func resolveTeamID(ctx context.Context, requested string) (string, error) {
	if ctxTeam := ctxutil.GetTeamID(ctx); ctxTeam != "" {
		if requested != "" && requested != ctxTeam {
			return "", domain.ErrForbidden
		}
		return ctxTeam, nil
	}
	if requested != "" {
		return requested, nil
	}
	return "", fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
}

func resolveActorID(ctx context.Context, requested string) (string, error) {
	if actor := ctxutil.GetActingUserID(ctx); actor != "" {
		return actor, nil
	}
	if requested != "" {
		return requested, nil
	}
	return "", fmt.Errorf("user_id: %w", domain.ErrInvalidArgument)
}

func ensureTeamAccess(ctx context.Context, resourceTeamID string) error {
	if resourceTeamID == "" {
		return nil
	}
	if ctxTeam := ctxutil.GetTeamID(ctx); ctxTeam != "" && ctxTeam != resourceTeamID {
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
	return ctxutil.GetActingUserID(ctx) != "" || ctxutil.GetAPIKeyID(ctx) != "" || ctxutil.GetTeamID(ctx) != ""
}

func isInternalCallWithoutAuth(ctx context.Context) bool {
	return !requiresAuthenticatedActor(ctx)
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
	actorID := ctxutil.GetActingUserID(ctx)
	if actorID == "" {
		return nil, domain.ErrForbidden
	}
	return userRepo.Get(ctx, actorID)
}

func requireWorkspaceAdminActor(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	return defaultAuthorizer.RequireWorkspaceAdminActor(ctx, userRepo)
}

func requirePrimaryAdminActor(ctx context.Context, userRepo repository.UserRepository) (*domain.User, error) {
	return defaultAuthorizer.RequirePrimaryAdminActor(ctx, userRepo)
}

func isSelfAction(ctx context.Context, userID string) bool {
	return userID != "" && ctxutil.GetActingUserID(ctx) == userID
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
	if err := ensureTeamAccess(ctx, actor.TeamID); err != nil {
		return nil, err
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
