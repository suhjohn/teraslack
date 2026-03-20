package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetTeamID(t *testing.T) {
	ctx := context.Background()
	if got := GetTeamID(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = context.WithValue(ctx, contextKeyTeamID, "T123")
	if got := GetTeamID(ctx); got != "T123" {
		t.Errorf("expected T123, got %q", got)
	}
}

func TestGetUserID(t *testing.T) {
	ctx := context.Background()
	if got := GetUserID(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = context.WithValue(ctx, contextKeyUserID, "U456")
	if got := GetUserID(ctx); got != "U456" {
		t.Errorf("expected U456, got %q", got)
	}
}

func TestAuthMiddleware_NoHeader(t *testing.T) {
	// AuthMiddleware should pass through requests without Authorization header
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := AuthMiddleware(nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected next handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
