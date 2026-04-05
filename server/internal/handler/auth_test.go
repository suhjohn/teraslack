package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
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

type authMiddlewareRepo struct {
	sessions map[string]*domain.AuthSession
}

func (r *authMiddlewareRepo) WithTx(_ pgx.Tx) repository.AuthRepository { return r }

func (r *authMiddlewareRepo) CreateSession(_ context.Context, params domain.CreateAuthSessionParams) (*domain.AuthSession, error) {
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: params.WorkspaceID,
		AccountID:   params.AccountID,
		UserID:      params.UserID,
		Provider:    params.Provider,
		Token:       "sess_handler",
		ExpiresAt:   params.ExpiresAt,
		CreatedAt:   time.Now().UTC(),
	}
	if r.sessions == nil {
		r.sessions = map[string]*domain.AuthSession{}
	}
	r.sessions[crypto.HashToken(session.Token)] = session
	return session, nil
}

func (r *authMiddlewareRepo) GetSessionByHash(_ context.Context, sessionHash string) (*domain.AuthSession, error) {
	session, ok := r.sessions[sessionHash]
	if !ok {
		return nil, domain.ErrInvalidAuth
	}
	return session, nil
}

func (r *authMiddlewareRepo) RevokeSessionByHash(_ context.Context, _ string) error {
	return nil
}

func (r *authMiddlewareRepo) DeletePendingEmailVerificationChallenges(_ context.Context, _ string) error {
	return nil
}

func (r *authMiddlewareRepo) CreateEmailVerificationChallenge(_ context.Context, _ domain.CreateEmailVerificationChallengeParams) (*domain.EmailVerificationChallenge, error) {
	return nil, errors.New("not implemented")
}

func (r *authMiddlewareRepo) GetEmailVerificationChallenge(_ context.Context, _, _ string) (*domain.EmailVerificationChallenge, error) {
	return nil, domain.ErrNotFound
}

func (r *authMiddlewareRepo) ConsumeEmailVerificationChallenge(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (r *authMiddlewareRepo) GetOAuthAccount(_ context.Context, _ domain.AuthProvider, _ string) (*domain.OAuthAccount, error) {
	return nil, domain.ErrNotFound
}

func (r *authMiddlewareRepo) ListOAuthAccountsBySubject(_ context.Context, _ domain.AuthProvider, _ string) ([]domain.OAuthAccount, error) {
	return nil, nil
}

func (r *authMiddlewareRepo) UpsertOAuthAccount(_ context.Context, _ domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
	return nil, errors.New("not implemented")
}

func TestAuthMiddleware_AttachesAccountWorkspaceAndMembershipContext(t *testing.T) {
	authRepo := &authMiddlewareRepo{
		sessions: map[string]*domain.AuthSession{
			crypto.HashToken("sess_handler"): {
				ID:          "AS123",
				WorkspaceID: "T123",
				AccountID:   "A123",
				UserID:      "U123",
				Provider:    domain.AuthProviderGitHub,
				Token:       "sess_handler",
				ExpiresAt:   time.Now().UTC().Add(time.Hour),
				CreatedAt:   time.Now().UTC(),
			},
		},
	}
	userRepo := &authTestUserRepo{
		user: &domain.User{
			ID:            "U123",
			AccountID:     "A123",
			WorkspaceID:   "T123",
			PrincipalType: domain.PrincipalTypeHuman,
			AccountType:   domain.AccountTypeAdmin,
		},
	}
	authSvc := service.NewAuthService(authRepo, userRepo, nil, nil, nil, nil, nil, service.AuthConfig{})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := ctxutil.GetWorkspaceID(r.Context()); got != "T123" {
			t.Fatalf("workspace_id = %q, want T123", got)
		}
			if got := ctxutil.GetUserID(r.Context()); got != "U123" {
				t.Fatalf("user_id = %q, want U123", got)
			}
		if got := ctxutil.GetAccountID(r.Context()); got != "A123" {
			t.Fatalf("account_id = %q, want A123", got)
		}
		if got := ctxutil.GetWorkspaceMembershipID(r.Context()); got != "WM_U123" {
			t.Fatalf("workspace_membership_id = %q, want WM_U123", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer sess_handler")
	w := httptest.NewRecorder()

	AuthMiddleware(authSvc, nil)(next).ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
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

func TestAuthMiddleware_AttachesCanonicalAccountAndWorkspaceIdentity(t *testing.T) {
	repo := &authTestAuthRepo{}
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: "T123",
		AccountID:   "A123",
		UserID:      "U123",
		Provider:    domain.AuthProviderGitHub,
		Token:       "sess_valid",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	repo.session = session

	authSvc := service.NewAuthService(repo, &authTestUserRepo{
		user: &domain.User{
			ID:            "U123",
			AccountID:     "A123",
			WorkspaceID:   "T123",
			PrincipalType: domain.PrincipalTypeHuman,
			AccountType:   domain.AccountTypeMember,
		},
	}, nil, nil, nil, nil, nil, service.AuthConfig{})

	called := false
	middleware := AuthMiddleware(authSvc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := ctxutil.GetAccountID(r.Context()); got != "A123" {
			t.Fatalf("account_id = %q, want A123", got)
		}
		if got := ctxutil.GetWorkspaceID(r.Context()); got != "T123" {
			t.Fatalf("workspace_id = %q, want T123", got)
		}
			if got := ctxutil.GetUserID(r.Context()); got != "U123" {
				t.Fatalf("user_id = %q, want U123", got)
			}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected next handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
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

func (r *authTestUserRepo) GetWorkspaceMembership(_ context.Context, workspaceID, accountID string) (*domain.WorkspaceMembership, error) {
	if r.user != nil && r.user.WorkspaceID == workspaceID && r.user.AccountID == accountID {
		return &domain.WorkspaceMembership{
			ID:             "WM_" + r.user.ID,
			WorkspaceID:    workspaceID,
			AccountID:      accountID,
			Role:           string(r.user.EffectiveAccountType()),
			Status:         domain.WorkspaceMembershipStatusActive,
			MembershipKind: domain.WorkspaceMembershipKindFull,
			GuestScope:     domain.WorkspaceGuestScopeWorkspaceFull,
		}, nil
	}
	return nil, domain.ErrNotFound
}

func (r *authTestUserRepo) GetWorkspaceMembershipID(_ context.Context, workspaceID, accountID string) (string, error) {
	if r.user != nil && r.user.WorkspaceID == workspaceID && r.user.AccountID == accountID {
		return "WM_" + r.user.ID, nil
	}
	return "", domain.ErrNotFound
}

func (r *authTestUserRepo) ListWorkspaceMembershipsByAccount(_ context.Context, accountID string) ([]domain.WorkspaceMembership, error) {
	if r.user != nil && r.user.AccountID == accountID {
		return []domain.WorkspaceMembership{{
			ID:             "WM_" + r.user.ID,
			WorkspaceID:    r.user.WorkspaceID,
			AccountID:      r.user.AccountID,
			Role:           string(r.user.EffectiveAccountType()),
			Status:         domain.WorkspaceMembershipStatusActive,
			MembershipKind: domain.WorkspaceMembershipKindFull,
			GuestScope:     domain.WorkspaceGuestScopeWorkspaceFull,
		}}, nil
	}
	return nil, nil
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

type authTestAuthRepo struct {
	session *domain.AuthSession
}

func (r *authTestAuthRepo) WithTx(_ pgx.Tx) repository.AuthRepository { return r }

func (r *authTestAuthRepo) CreateSession(_ context.Context, _ domain.CreateAuthSessionParams) (*domain.AuthSession, error) {
	return nil, nil
}

func (r *authTestAuthRepo) GetSessionByHash(_ context.Context, _ string) (*domain.AuthSession, error) {
	if r.session != nil {
		return r.session, nil
	}
	return nil, domain.ErrInvalidAuth
}

func (r *authTestAuthRepo) RevokeSessionByHash(_ context.Context, _ string) error { return nil }

func (r *authTestAuthRepo) DeletePendingEmailVerificationChallenges(_ context.Context, _ string) error {
	return nil
}

func (r *authTestAuthRepo) CreateEmailVerificationChallenge(_ context.Context, _ domain.CreateEmailVerificationChallengeParams) (*domain.EmailVerificationChallenge, error) {
	return nil, nil
}

func (r *authTestAuthRepo) GetEmailVerificationChallenge(_ context.Context, _, _ string) (*domain.EmailVerificationChallenge, error) {
	return nil, domain.ErrNotFound
}

func (r *authTestAuthRepo) ConsumeEmailVerificationChallenge(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (r *authTestAuthRepo) GetOAuthAccount(_ context.Context, _ domain.AuthProvider, _ string) (*domain.OAuthAccount, error) {
	return nil, domain.ErrNotFound
}

func (r *authTestAuthRepo) ListOAuthAccountsBySubject(_ context.Context, _ domain.AuthProvider, _ string) ([]domain.OAuthAccount, error) {
	return nil, nil
}

func (r *authTestAuthRepo) UpsertOAuthAccount(_ context.Context, _ domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
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

	if got.AccountID != "A123" || got.UserID != "" || got.WorkspaceID != "T123" {
		t.Fatalf("unexpected identity envelope: %+v", got)
	}
	if got.Account == nil || got.Account.Email != "member@example.com" {
		t.Fatalf("expected canonical account on /auth/me, got %+v", got.Account)
	}
	if got.User == nil {
		t.Fatalf("expected workspace membership actor on /auth/me, got %+v", got.User)
	}
	if got.User.ID != "" || got.User.AccountID != "A123" || got.User.WorkspaceID != "T123" {
		t.Fatalf("expected synthesized workspace actor on /auth/me, got %+v", got.User)
	}
	if got.User.Email != "member@example.com" {
		t.Fatalf("expected user email mirrored from canonical account email, got %+v", got.User)
	}
	if got.AccountType != domain.AccountTypeAdmin || got.PrincipalType != domain.PrincipalTypeHuman {
		t.Fatalf("unexpected auth role metadata: %+v", got)
	}
}
