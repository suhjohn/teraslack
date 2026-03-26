package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockAuthRepo struct {
	sessions map[string]*domain.AuthSession
}

func newMockAuthRepo() *mockAuthRepo {
	return &mockAuthRepo{sessions: make(map[string]*domain.AuthSession)}
}

func (m *mockAuthRepo) WithTx(_ pgx.Tx) repository.AuthRepository { return m }

func (m *mockAuthRepo) CreateSession(_ context.Context, params domain.CreateAuthSessionParams) (*domain.AuthSession, error) {
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: params.WorkspaceID,
		UserID:      params.UserID,
		Provider:    params.Provider,
		Token:       "sess_test",
		ExpiresAt:   params.ExpiresAt,
		CreatedAt:   time.Now().UTC(),
	}
	m.sessions[crypto.HashToken(session.Token)] = session
	return session, nil
}

func (m *mockAuthRepo) GetSessionByHash(_ context.Context, sessionHash string) (*domain.AuthSession, error) {
	session, ok := m.sessions[sessionHash]
	if !ok {
		return nil, domain.ErrInvalidAuth
	}
	return session, nil
}

func (m *mockAuthRepo) RevokeSessionByHash(_ context.Context, sessionHash string) error {
	session, ok := m.sessions[sessionHash]
	if !ok {
		return domain.ErrInvalidAuth
	}
	now := time.Now().UTC()
	session.RevokedAt = &now
	return nil
}

func (m *mockAuthRepo) GetOAuthAccount(_ context.Context, _ string, _ domain.AuthProvider, _ string) (*domain.OAuthAccount, error) {
	return nil, domain.ErrNotFound
}

func (m *mockAuthRepo) ListOAuthAccountsBySubject(_ context.Context, _ domain.AuthProvider, _ string) ([]domain.OAuthAccount, error) {
	return nil, nil
}

func (m *mockAuthRepo) UpsertOAuthAccount(_ context.Context, params domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
	return &domain.OAuthAccount{
		ID:              "OA123",
		WorkspaceID:     params.WorkspaceID,
		UserID:          params.UserID,
		Provider:        params.Provider,
		ProviderSubject: params.ProviderSubject,
		Email:           params.Email,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}, nil
}

func TestAuthService_ValidateSession(t *testing.T) {
	repo := newMockAuthRepo()
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: "T123",
		UserID:      "U123",
		Provider:    domain.AuthProviderGitHub,
		Token:       "sess_valid",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	repo.sessions[crypto.HashToken(session.Token)] = session

	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	auth, err := svc.ValidateSession(context.Background(), "Bearer "+session.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if auth.WorkspaceID != "T123" || auth.UserID != "U123" || auth.IsBot {
		t.Fatalf("unexpected auth context: %+v", auth)
	}
	if auth.PrincipalType != domain.PrincipalTypeHuman || auth.AccountType != domain.AccountTypeMember {
		t.Fatalf("unexpected principal in auth context: %+v", auth)
	}
}

func TestAuthService_ValidateSession_RejectsRevokedSession(t *testing.T) {
	repo := newMockAuthRepo()
	raw := "sess_revoked"
	now := time.Now().UTC()
	repo.sessions[crypto.HashToken(raw)] = &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: "T123",
		UserID:      "U123",
		Provider:    domain.AuthProviderGoogle,
		ExpiresAt:   now.Add(time.Hour),
		RevokedAt:   &now,
		CreatedAt:   now,
	}

	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	_, err := svc.ValidateSession(context.Background(), "Bearer "+raw)
	if !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("ValidateSession() error = %v", err)
	}
}

func TestAuthService_RevokeSession(t *testing.T) {
	repo := newMockAuthRepo()
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: "T123",
		UserID:      "U123",
		Provider:    domain.AuthProviderGitHub,
		Token:       "sess_valid",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	hash := crypto.HashToken(session.Token)
	repo.sessions[hash] = session

	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	if err := svc.RevokeSession(context.Background(), session.Token); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if repo.sessions[hash].RevokedAt == nil {
		t.Fatal("expected session to be revoked")
	}
}

func TestAuthService_SwitchWorkspace(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	current := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: "T123",
		UserID:      "U_CURRENT",
		Provider:    domain.AuthProviderGitHub,
		Token:       "sess_valid",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	hash := crypto.HashToken(current.Token)
	repo.sessions[hash] = current
	userRepo.users["U_CURRENT"] = &domain.User{
		ID:            "U_CURRENT",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	userRepo.users["U_TARGET"] = &domain.User{
		ID:            "U_TARGET",
		WorkspaceID:   "T999",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(repo, userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	next, err := svc.SwitchWorkspace(context.Background(), "Bearer "+current.Token, "T999")
	if err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	if next.WorkspaceID != "T999" || next.UserID != "U_TARGET" {
		t.Fatalf("unexpected switched session: %+v", next)
	}
	if repo.sessions[hash].RevokedAt == nil {
		t.Fatal("expected previous session to be revoked")
	}
}

func TestAuthService_StartOAuth_AllowsFrontendRedirect(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		BaseURL:                 "https://api.teraslack.ai",
		FrontendURL:             "https://teraslack.ai",
		StateSecret:             "test-secret",
		GitHubOAuthClientID:     "github-client",
		GitHubOAuthClientSecret: "github-secret",
	})

	result, err := svc.StartOAuth(context.Background(), domain.StartOAuthParams{
		Provider:    domain.AuthProviderGitHub,
		WorkspaceID: "T123",
		RedirectTo:  "https://teraslack.ai/admin",
	})
	if err != nil {
		t.Fatalf("StartOAuth() error = %v", err)
	}
	if !strings.Contains(result.AuthorizationURL, "redirect_uri=https%3A%2F%2Fapi.teraslack.ai%2Fauth%2Foauth%2Fgithub%2Fcallback") {
		t.Fatalf("authorization url should keep API callback, got %q", result.AuthorizationURL)
	}
}

func TestAuthService_StartOAuth_RejectsUnknownRedirectHost(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		BaseURL:                 "https://api.teraslack.ai",
		FrontendURL:             "https://teraslack.ai",
		StateSecret:             "test-secret",
		GitHubOAuthClientID:     "github-client",
		GitHubOAuthClientSecret: "github-secret",
	})

	_, err := svc.StartOAuth(context.Background(), domain.StartOAuthParams{
		Provider:    domain.AuthProviderGitHub,
		WorkspaceID: "T123",
		RedirectTo:  "https://evil.example.com/admin",
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("StartOAuth() error = %v, want invalid argument", err)
	}
}

func TestAuthService_doJSON_MapsOAuthProviderErrorsToInvalidAuth(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Bad Request"}`))
	}))
	defer provider.Close()

	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		HTTPClient: provider.Client(),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, provider.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	var target map[string]any
	err = svc.doJSON(req, &target)
	if !errors.Is(err, domain.ErrInvalidAuth) {
		t.Fatalf("doJSON() error = %v, want invalid auth", err)
	}
}
