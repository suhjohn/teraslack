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
