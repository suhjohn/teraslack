package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/service"
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

	middleware := AuthMiddleware(nil, nil)(next)

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
	for _, path := range []string{"/healthz", "/auth/signup", "/auth/verify"} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := AuthMiddleware(nil, nil)(next)

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
	middleware := AuthMiddleware(nil, nil)(next)
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/github/start", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if !called {
		t.Fatal("expected oauth start to bypass auth")
	}

	called = false
	next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middleware = AuthMiddleware(nil, nil)(next)
	req = httptest.NewRequest(http.MethodPost, "/auth/cli/oauth/google/start", nil)
	w = httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	if !called {
		t.Fatal("expected cli oauth start to bypass auth")
	}

	// GET /auth/me should NOT bypass auth
	called = false
	next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middleware = AuthMiddleware(nil, nil)(next)
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

type authTestAccountRepo struct {
	account *domain.Account
}

func (r *authTestAccountRepo) WithTx(_ pgx.Tx) repository.AccountRepository { return r }

func (r *authTestAccountRepo) Create(_ context.Context, _ domain.CreateAccountParams) (*domain.Account, error) {
	return nil, nil
}

func (r *authTestAccountRepo) Get(_ context.Context, id string) (*domain.Account, error) {
	if r.account != nil && r.account.ID == id {
		return r.account, nil
	}
	return nil, domain.ErrNotFound
}

func (r *authTestAccountRepo) GetByEmail(_ context.Context, _ string) (*domain.Account, error) {
	return nil, domain.ErrNotFound
}

type authTestUserRepo struct {
	user *domain.User
}

func (r *authTestUserRepo) WithTx(_ pgx.Tx) repository.UserRepository { return r }

func (r *authTestUserRepo) Create(_ context.Context, _ domain.CreateUserParams) (*domain.User, error) {
	return nil, nil
}

func (r *authTestUserRepo) Get(_ context.Context, id string) (*domain.User, error) {
	if r.user != nil && r.user.ID == id {
		return r.user, nil
	}
	return nil, domain.ErrNotFound
}

func (r *authTestUserRepo) GetByWorkspaceAndAccount(_ context.Context, workspaceID, accountID string) (*domain.User, error) {
	if r.user != nil && r.user.WorkspaceID == workspaceID && r.user.AccountID == accountID {
		return r.user, nil
	}
	return nil, domain.ErrNotFound
}

func (r *authTestUserRepo) ListByAccount(_ context.Context, accountID string) ([]domain.User, error) {
	if r.user != nil && r.user.AccountID == accountID {
		return []domain.User{*r.user}, nil
	}
	return nil, nil
}

func (r *authTestUserRepo) Update(_ context.Context, _ string, _ domain.UpdateUserParams) (*domain.User, error) {
	return nil, nil
}

func (r *authTestUserRepo) List(_ context.Context, _ domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	return nil, nil
}

func TestAuthHandlerMeReturnsAccountFirstIdentityWithWorkspaceUser(t *testing.T) {
	account := &domain.Account{
		ID:            "A123",
		PrincipalType: domain.PrincipalTypeHuman,
		Email:         "member@example.com",
	}
	user := &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Name:          "member",
		RealName:      "Workspace Member",
		DisplayName:   "Workspace Member",
		Email:         "stale-workspace@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}

	authSvc := service.NewAuthService(nil, &authTestUserRepo{user: user}, nil, nil, nil, nil, nil, service.AuthConfig{})
	authSvc.SetIdentityRepositories(&authTestAccountRepo{account: account})
	handler := NewAuthHandler(authSvc)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := ctxutil.WithIdentity(req.Context(), "A123")
	ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, "T123")
	ctx = context.WithValue(ctx, ctxutil.ContextKeyPrincipalType, domain.PrincipalTypeHuman)
	ctx = context.WithValue(ctx, ctxutil.ContextKeyAccountType, domain.AccountTypeAdmin)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Me(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got domain.AuthMeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.AccountID != "A123" || got.UserID != "U123" || got.WorkspaceID != "T123" {
		t.Fatalf("unexpected identity envelope: %+v", got)
	}
	if got.Account == nil || got.Account.Email != "member@example.com" {
		t.Fatalf("expected canonical account on /auth/me, got %+v", got.Account)
	}
	if got.User == nil || got.User.ID != "U123" {
		t.Fatalf("expected workspace-local user on /auth/me, got %+v", got.User)
	}
	if got.User.Email != "member@example.com" {
		t.Fatalf("expected user email mirrored from canonical account email, got %+v", got.User)
	}
	if got.AccountType != domain.AccountTypeAdmin || got.PrincipalType != domain.PrincipalTypeHuman {
		t.Fatalf("unexpected auth role metadata: %+v", got)
	}
}
