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
