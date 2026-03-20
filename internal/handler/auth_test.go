package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suhjohn/workspace/internal/ctxutil"
)

func TestGetTeamID(t *testing.T) {
	ctx := context.Background()
	if got := GetTeamID(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = context.WithValue(ctx, ctxutil.ContextKeyTeamID, "T123")
	if got := GetTeamID(ctx); got != "T123" {
		t.Errorf("expected T123, got %q", got)
	}
}

func TestGetUserID(t *testing.T) {
	ctx := context.Background()
	if got := GetUserID(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = context.WithValue(ctx, ctxutil.ContextKeyUserID, "U456")
	if got := GetUserID(ctx); got != "U456" {
		t.Errorf("expected U456, got %q", got)
	}
}

func TestAuthMiddleware_NoHeader(t *testing.T) {
	// AuthMiddleware should reject requests without Authorization header with 401
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := AuthMiddleware(nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if called {
		t.Fatal("expected next handler NOT to be called for unauthenticated request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_BypassPaths(t *testing.T) {
	// Path-only bypasses (any method)
	for _, path := range []string{"/auth/test", "/healthz"} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := AuthMiddleware(nil)(next)

		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if !called {
			t.Fatalf("expected next handler to be called for bypass path %s", path)
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", path, w.Code)
		}
	}

	// Method+path bypass: POST /tokens should bypass
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middleware := AuthMiddleware(nil)(next)
	req := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if !called {
		t.Fatal("expected POST /tokens to bypass auth")
	}

	// DELETE /tokens should NOT bypass auth
	called = false
	next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middleware = AuthMiddleware(nil)(next)
	req = httptest.NewRequest(http.MethodDelete, "/tokens", nil)
	w = httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if called {
		t.Fatal("expected DELETE /tokens to require auth")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for DELETE /tokens, got %d", w.Code)
	}
}
