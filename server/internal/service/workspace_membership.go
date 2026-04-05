package service

import (
	"context"
	"errors"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func actorWorkspaceMembership(ctx context.Context, userRepo repository.UserRepository, workspaceID string) (*domain.WorkspaceMembership, error) {
	if userRepo == nil {
		return nil, domain.ErrForbidden
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, domain.ErrInvalidArgument
	}
	accountID, err := resolveActorAccountID(ctx, userRepo)
	if err != nil {
		return nil, err
	}
	if accountID == "" {
		return nil, domain.ErrForbidden
	}
	membership, err := userRepo.GetWorkspaceMembership(ctx, workspaceID, accountID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}
	return membership, nil
}

func ensureWorkspaceMembershipAccess(ctx context.Context, userRepo repository.UserRepository, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	if !requiresAuthenticatedActor(ctx) {
		return nil
	}
	if ctxWorkspace := strings.TrimSpace(ctxutil.GetWorkspaceID(ctx)); ctxWorkspace == workspaceID {
		if ctxutil.GetWorkspaceMembershipID(ctx) != "" || ctxutil.GetUserID(ctx) != "" {
			return nil
		}
	}
	if userRepo == nil {
		return ensureWorkspaceAccess(ctx, workspaceID)
	}
	_, err := actorWorkspaceMembership(ctx, userRepo, workspaceID)
	return err
}

func actorIsWorkspaceAdmin(ctx context.Context, userRepo repository.UserRepository, workspaceID string) bool {
	if ctxutil.GetPrincipalType(ctx) == domain.PrincipalTypeSystem {
		return true
	}
	if userRepo != nil && strings.TrimSpace(workspaceID) != "" {
		membership, err := actorWorkspaceMembership(ctx, userRepo, workspaceID)
		if err == nil && membership != nil {
			return defaultAuthorizer.IsWorkspaceAdminAccount(membership.EffectiveAccountType())
		}
	}
	return false
}

func workspaceUserFromMembership(account *domain.Account, membership *domain.WorkspaceMembership, existingUser *domain.User) *domain.User {
	if membership == nil {
		return existingUser
	}
	var user domain.User
	if existingUser != nil {
		user = *existingUser
	}
	user.WorkspaceID = membership.WorkspaceID
	user.AccountID = membership.AccountID
	user.AccountType = membership.EffectiveAccountType()
	if account != nil {
		user.Email = account.Email
		user.PrincipalType = account.PrincipalType
		user.IsBot = account.IsBot
	}
	return &user
}
