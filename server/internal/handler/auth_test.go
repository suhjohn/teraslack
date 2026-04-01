package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suhjohn/teraslack/internal/ctxutil"
)

func TestGetWorkspaceID(t *testing.T) {
	ctx := context.Background()
	if got := GetWorkspaceID(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, "T123")
	if got := GetWorkspaceID(ctx); got != "T123" {
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

	middleware := AuthMiddleware(nil, nil, nil)(next)

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
	for _, path := range []string{"/healthz", "/oauth/authorize", "/oauth/token", "/.well-known/oauth-authorization-server", "/auth/signup", "/auth/verify"} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := AuthMiddleware(nil, nil, nil)(next)

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

	// OAuth routes should bypass auth
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middleware := AuthMiddleware(nil, nil, nil)(next)
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/github/start", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if !called {
		t.Fatal("expected oauth start to bypass auth")
	}

	// GET /auth/me should NOT bypass auth
	called = false
	next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middleware = AuthMiddleware(nil, nil, nil)(next)
	req = httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	w = httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if called {
		t.Fatal("expected GET /auth/me to require auth")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for GET /auth/me, got %d", w.Code)
	}
}

func TestAuthCredentialFromRequest_DetectsAPIKeys(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
		wantAPI   bool
	}{
		{name: "plain sk prefix", header: "Bearer sk_abc123", wantToken: "sk_abc123", wantAPI: true},
		{name: "live key prefix", header: "Bearer sk_live_abc123", wantToken: "sk_live_abc123", wantAPI: true},
		{name: "test key prefix", header: "Bearer sk_test_abc123", wantToken: "sk_test_abc123", wantAPI: true},
		{name: "session token", header: "Bearer session-token", wantToken: "session-token", wantAPI: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", tc.header)

			gotToken, gotAPI := authCredentialFromRequest(req)
			if gotToken != tc.wantToken {
				t.Fatalf("token = %q, want %q", gotToken, tc.wantToken)
			}
			if gotAPI != tc.wantAPI {
				t.Fatalf("isAPI = %v, want %v", gotAPI, tc.wantAPI)
			}
		})
	}
}
