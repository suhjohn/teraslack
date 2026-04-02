package service

import (
	"context"
	"errors"
	"testing"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
)

func TestRequirePermission_AllowsBearerTokens(t *testing.T) {
	err := requirePermission(context.Background(), domain.PermissionMessagesRead)
	if err != nil {
		t.Fatalf("requirePermission() error = %v", err)
	}
}

func TestRequirePermission_DeniesEmptyAPIKeyPermissions(t *testing.T) {
	ctx := ctxutil.WithDelegation(context.Background(), "", "AK123")
	err := requirePermission(ctx, domain.PermissionMessagesRead)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("requirePermission() error = %v, want forbidden", err)
	}
}

func TestRequirePermission_AllowsWildcardAPIKeyPermissions(t *testing.T) {
	ctx := ctxutil.WithDelegation(context.Background(), "", "AK123")
	ctx = ctxutil.WithPermissions(ctx, []string{"*"})
	err := requirePermission(ctx, domain.PermissionMessagesRead)
	if err != nil {
		t.Fatalf("requirePermission() error = %v", err)
	}
}

func TestRequireWorkspaceAdminActor_AllowsSystemPrincipal(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeSystem, domain.AccountTypePrimaryAdmin, true)

	actor, err := requireWorkspaceAdminActor(ctx, newMockUserRepoTenant())
	if err != nil {
		t.Fatalf("requireWorkspaceAdminActor() error = %v", err)
	}
	if actor.PrincipalType != domain.PrincipalTypeSystem {
		t.Fatalf("actor principal_type = %q, want %q", actor.PrincipalType, domain.PrincipalTypeSystem)
	}
}

func TestRequirePrimaryAdminActor_AllowsSystemPrincipal(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyWorkspaceID, "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeSystem, domain.AccountTypePrimaryAdmin, true)

	actor, err := requirePrimaryAdminActor(ctx, newMockUserRepoTenant())
	if err != nil {
		t.Fatalf("requirePrimaryAdminActor() error = %v", err)
	}
	if actor.PrincipalType != domain.PrincipalTypeSystem {
		t.Fatalf("actor principal_type = %q, want %q", actor.PrincipalType, domain.PrincipalTypeSystem)
	}
}

func TestRequireWorkspaceAdminActorUsesCanonicalWorkspaceUserOverContextClaims(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)

	if _, err := requireWorkspaceAdminActor(ctx, userRepo); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("requireWorkspaceAdminActor() error = %v, want forbidden", err)
	}
}

func TestRequireWorkspaceAdminActorAllowsCanonicalWorkspaceAdminUserDespiteStaleContext(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	actor, err := requireWorkspaceAdminActor(ctx, userRepo)
	if err != nil {
		t.Fatalf("requireWorkspaceAdminActor() error = %v", err)
	}
	if actor.EffectiveAccountType() != domain.AccountTypeAdmin {
		t.Fatalf("actor effective_account_type = %q, want %q", actor.EffectiveAccountType(), domain.AccountTypeAdmin)
	}
}
