package ctxutil

import (
	"context"
	"testing"
)

func TestGetActingUserID(t *testing.T) {
	ctx := context.Background()
	if got := GetActingUserID(ctx); got != "" {
		t.Fatalf("expected empty acting user, got %q", got)
	}

	ctx = context.WithValue(ctx, ContextKeyUserID, "U123")
	if got := GetActingUserID(ctx); got != "U123" {
		t.Fatalf("expected U123, got %q", got)
	}

	ctx = context.WithValue(ctx, ContextKeyOnBehalfOf, "U999")
	if got := GetActingUserID(ctx); got != "U999" {
		t.Fatalf("expected delegated user U999, got %q", got)
	}
}

func TestWithIdentity(t *testing.T) {
	ctx := WithIdentity(context.Background(), "A123", "WM123")
	if got := GetAccountID(ctx); got != "A123" {
		t.Fatalf("expected A123, got %q", got)
	}
	if got := GetMembershipID(ctx); got != "WM123" {
		t.Fatalf("expected WM123, got %q", got)
	}
}
