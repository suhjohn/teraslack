package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockAuthRepo struct {
	sessions      map[string]*domain.AuthSession
	challenges    map[string]*domain.EmailVerificationChallenge
	oauthAccounts map[string]*domain.OAuthAccount
}

func newMockAuthRepo() *mockAuthRepo {
	return &mockAuthRepo{
		sessions:      make(map[string]*domain.AuthSession),
		challenges:    make(map[string]*domain.EmailVerificationChallenge),
		oauthAccounts: make(map[string]*domain.OAuthAccount),
	}
}

func (m *mockAuthRepo) WithTx(_ pgx.Tx) repository.AuthRepository { return m }

func (m *mockAuthRepo) CreateSession(_ context.Context, params domain.CreateAuthSessionParams) (*domain.AuthSession, error) {
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: params.WorkspaceID,
		AccountID:   params.AccountID,
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

func (m *mockAuthRepo) DeletePendingEmailVerificationChallenges(_ context.Context, email string) error {
	for id, challenge := range m.challenges {
		if strings.EqualFold(challenge.Email, email) && challenge.ConsumedAt == nil {
			delete(m.challenges, id)
		}
	}
	return nil
}

func (m *mockAuthRepo) CreateEmailVerificationChallenge(_ context.Context, params domain.CreateEmailVerificationChallengeParams) (*domain.EmailVerificationChallenge, error) {
	challenge := &domain.EmailVerificationChallenge{
		ID:        fmt.Sprintf("EV%d", len(m.challenges)+1),
		Email:     params.Email,
		CodeHash:  params.CodeHash,
		ExpiresAt: params.ExpiresAt,
		CreatedAt: time.Now().UTC(),
	}
	m.challenges[challenge.ID] = challenge
	return challenge, nil
}

func (m *mockAuthRepo) GetEmailVerificationChallenge(_ context.Context, email, codeHash string) (*domain.EmailVerificationChallenge, error) {
	for _, challenge := range m.challenges {
		if strings.EqualFold(challenge.Email, email) && challenge.CodeHash == codeHash {
			return challenge, nil
		}
	}
	return nil, domain.ErrInvalidAuth
}

func (m *mockAuthRepo) ConsumeEmailVerificationChallenge(_ context.Context, id string, consumedAt time.Time) error {
	challenge, ok := m.challenges[id]
	if !ok || challenge.ConsumedAt != nil {
		return domain.ErrInvalidAuth
	}
	challenge.ConsumedAt = &consumedAt
	return nil
}

func (m *mockAuthRepo) GetOAuthAccount(_ context.Context, provider domain.AuthProvider, providerSubject string) (*domain.OAuthAccount, error) {
	account, ok := m.oauthAccounts[oauthAccountKey("", provider, providerSubject)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return account, nil
}

func (m *mockAuthRepo) ListOAuthAccountsBySubject(_ context.Context, provider domain.AuthProvider, providerSubject string) ([]domain.OAuthAccount, error) {
	accounts := make([]domain.OAuthAccount, 0)
	for _, account := range m.oauthAccounts {
		if account.Provider == provider && account.ProviderSubject == providerSubject {
			accounts = append(accounts, *account)
		}
	}
	return accounts, nil
}

func (m *mockAuthRepo) UpsertOAuthAccount(_ context.Context, params domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error) {
	account := &domain.OAuthAccount{
		ID:              "OA123",
		WorkspaceID:     params.WorkspaceID,
		AccountID:       params.AccountID,
		UserID:          params.UserID,
		Provider:        params.Provider,
		ProviderSubject: params.ProviderSubject,
		Email:           params.Email,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	m.oauthAccounts[oauthAccountKey(params.WorkspaceID, params.Provider, params.ProviderSubject)] = account
	return account, nil
}

func oauthAccountKey(workspaceID string, provider domain.AuthProvider, providerSubject string) string {
	return string(provider) + "|" + providerSubject
}

type mockAuthEmailSender struct {
	emails []string
	codes  []string
	sentAt []time.Time
}

func (m *mockAuthEmailSender) SendVerificationCode(_ context.Context, email, code string, expiresAt time.Time) error {
	m.emails = append(m.emails, email)
	m.codes = append(m.codes, code)
	m.sentAt = append(m.sentAt, expiresAt)
	return nil
}

type mockAccountRepo struct {
	byID    map[string]*domain.Account
	byEmail map[string]*domain.Account
}

func newMockAccountRepo() *mockAccountRepo {
	return &mockAccountRepo{
		byID:    map[string]*domain.Account{},
		byEmail: map[string]*domain.Account{},
	}
}

func (m *mockAccountRepo) WithTx(_ pgx.Tx) repository.AccountRepository { return m }

func (m *mockAccountRepo) Create(_ context.Context, params domain.CreateAccountParams) (*domain.Account, error) {
	account := &domain.Account{
		ID:            "A_NEW",
		PrincipalType: params.PrincipalType,
		Email:         params.Email,
		IsBot:         params.IsBot,
		Deleted:       params.Deleted,
	}
	m.byID[account.ID] = account
	if account.Email != "" {
		m.byEmail[strings.ToLower(account.Email)] = account
	}
	return account, nil
}

func (m *mockAccountRepo) Get(_ context.Context, id string) (*domain.Account, error) {
	account, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return account, nil
}

func (m *mockAccountRepo) GetByEmail(_ context.Context, email string) (*domain.Account, error) {
	account, ok := m.byEmail[strings.ToLower(email)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return account, nil
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

	svc := NewAuthService(repo, &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
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

func TestAuthService_ValidateSessionUsesUserIdentity(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
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
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(repo, userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})

	auth, err := svc.ValidateSession(context.Background(), "Bearer "+session.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if auth.AccountID != "A123" {
		t.Fatalf("unexpected identity context: %+v", auth)
	}
	if auth.WorkspaceMembershipID != "WM_U123" {
		t.Fatalf("workspace_membership_id = %q, want %q", auth.WorkspaceMembershipID, "WM_U123")
	}
	if auth.AccountType != domain.AccountTypeMember {
		t.Fatalf("expected user account type, got %s", auth.AccountType)
	}
}

func TestAuthService_ValidateSessionFromAccountAndWorkspaceMembership(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	session := &domain.AuthSession{
		ID:          "AS123",
		WorkspaceID: "T123",
		AccountID:   "A123",
		Provider:    domain.AuthProviderGitHub,
		Token:       "sess_valid",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	repo.sessions[crypto.HashToken(session.Token)] = session
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		PrincipalType: domain.PrincipalTypeHuman,
		Email:         "member@example.com",
	}
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}

	svc := NewAuthService(repo, userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo, nil)

	auth, err := svc.ValidateSession(context.Background(), "Bearer "+session.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if auth.AccountID != "A123" {
		t.Fatalf("unexpected identity context: %+v", auth)
	}
	if auth.UserID != "U123" {
		t.Fatalf("expected existing workspace user U123, got %+v", auth)
	}
	if auth.WorkspaceMembershipID != "WM_U123" {
		t.Fatalf("expected workspace_membership_id WM_U123, got %+v", auth)
	}
	if auth.AccountType != domain.AccountTypeAdmin || auth.PrincipalType != domain.PrincipalTypeHuman {
		t.Fatalf("unexpected canonical auth context: %+v", auth)
	}
}

func TestAuthService_ValidateSessionUsesCanonicalAccountIdentity(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
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
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		PrincipalType: domain.PrincipalTypeAgent,
		Email:         "canonical@example.com",
		IsBot:         true,
	}
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "stale@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		IsBot:         false,
		AccountType:   domain.AccountTypeNone,
	}

	svc := NewAuthService(repo, userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo, nil)

	auth, err := svc.ValidateSession(context.Background(), "Bearer "+session.Token)
	if err != nil {
		t.Fatalf("ValidateSession() error = %v", err)
	}
	if auth.AccountID != "A123" {
		t.Fatalf("account_id = %q, want A123", auth.AccountID)
	}
	if auth.PrincipalType != domain.PrincipalTypeAgent {
		t.Fatalf("principal_type = %q, want %q", auth.PrincipalType, domain.PrincipalTypeAgent)
	}
	if !auth.IsBot {
		t.Fatal("expected canonical is_bot=true from account")
	}
}

func TestAuthService_GetCurrentUserFromAccountAndWorkspaceMembership(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		PrincipalType: domain.PrincipalTypeHuman,
		Email:         "member@example.com",
	}
	accountRepo.byEmail["member@example.com"] = accountRepo.byID["A123"]
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(newMockAuthRepo(), userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, "T123")
	user, err := svc.GetCurrentUser(ctx)
	if err != nil {
		t.Fatalf("GetCurrentUser() error = %v", err)
	}
	if user == nil {
		t.Fatalf("expected current workspace user, got %+v", user)
	}
	if user.ID != "" || user.AccountID != "A123" || user.WorkspaceID != "T123" {
		t.Fatalf("expected synthesized membership actor for T123/A123, got %+v", user)
	}
	if user.EffectiveAccountType() != domain.AccountTypeMember {
		t.Fatalf("expected member account type, got %+v", user)
	}
}

func TestAuthService_GetCurrentIdentityReturnsAccountAndWorkspaceMembershipActor(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		PrincipalType: domain.PrincipalTypeHuman,
		Email:         "member@example.com",
	}
	accountRepo.byEmail["member@example.com"] = accountRepo.byID["A123"]
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(newMockAuthRepo(), userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, "T123")

	account, user, err := svc.GetCurrentIdentity(ctx)
	if err != nil {
		t.Fatalf("GetCurrentIdentity() error = %v", err)
	}
	if account == nil || account.ID != "A123" {
		t.Fatalf("expected current account, got %+v", account)
	}
	if user == nil {
		t.Fatalf("expected current workspace user, got %+v", user)
	}
	if user.ID != "" || user.AccountID != "A123" || user.WorkspaceID != "T123" {
		t.Fatalf("expected synthesized membership actor for T123/A123, got %+v", user)
	}
	if user.EffectiveAccountType() != domain.AccountTypeMember {
		t.Fatalf("expected member account type, got %+v", user)
	}
}

func TestAuthService_GetCurrentUserUsesUserState(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(newMockAuthRepo(), userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})

	user, err := svc.GetCurrentUser(ctxutil.WithUser(context.Background(), "U123", "T123"))
	if err != nil {
		t.Fatalf("GetCurrentUser() error = %v", err)
	}
	if user.WorkspaceID != "T123" {
		t.Fatalf("workspace_id = %q, want T123", user.WorkspaceID)
	}
	if user.EffectiveAccountType() != domain.AccountTypeMember {
		t.Fatalf("account_type = %q, want member", user.EffectiveAccountType())
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

	svc := NewAuthService(repo, &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
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

	svc := NewAuthService(repo, &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	if err := svc.RevokeSession(context.Background(), session.Token); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if repo.sessions[hash].RevokedAt == nil {
		t.Fatal("expected session to be revoked")
	}
}

func TestAuthService_SwitchWorkspaceRequiresAccountLinkedWorkspaceUser(t *testing.T) {
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
	if _, err := svc.SwitchWorkspace(context.Background(), "Bearer "+current.Token, "T999"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("SwitchWorkspace() error = %v, want forbidden", err)
	}
}

func TestAuthService_SwitchWorkspaceUsesAccountScopedUsers(t *testing.T) {
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
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	userRepo.users["U_TARGET"] = &domain.User{
		ID:            "U_TARGET",
		AccountID:     "A123",
		WorkspaceID:   "T999",
		Email:         "different@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(repo, userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})

	next, err := svc.SwitchWorkspace(context.Background(), "Bearer "+current.Token, "T999")
	if err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	if next.WorkspaceID != "T999" || next.AccountID != "A123" || next.UserID != "" {
		t.Fatalf("unexpected switched session: %+v", next)
	}
}

func TestAuthService_SwitchWorkspaceDoesNotReuseEmailOnlyUser(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
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
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	userRepo.users["U_LEAK"] = &domain.User{
		ID:            "U_LEAK",
		AccountID:     "A999",
		WorkspaceID:   "T999",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["alice@example.com"] = accountRepo.byID["A123"]
	userRepo.users["U_TARGET"] = &domain.User{
		ID:            "U_TARGET",
		AccountID:     "A123",
		WorkspaceID:   "T999",
		Email:         "alice+target@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(repo, userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	next, err := svc.SwitchWorkspace(context.Background(), "Bearer "+current.Token, "T999")
	if err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	if next.AccountID != "A123" || next.UserID != "" {
		t.Fatalf("expected account-linked workspace user, got %+v", next)
	}
}

func TestAuthService_StartOAuth_AllowsFrontendRedirect(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
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

func TestAuthService_StartCLIOAuth_AllowsLocalhostCallback(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		BaseURL:                 "https://api.teraslack.ai",
		FrontendURL:             "https://teraslack.ai",
		StateSecret:             "test-secret",
		GoogleOAuthClientID:     "google-client",
		GoogleOAuthClientSecret: "google-secret",
	})

	result, err := svc.StartCLIOAuth(context.Background(), domain.StartOAuthParams{
		Provider:    domain.AuthProviderGoogle,
		CallbackURL: "http://127.0.0.1:43123/callback",
	})
	if err != nil {
		t.Fatalf("StartCLIOAuth() error = %v", err)
	}

	u, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if got, want := u.Query().Get("redirect_uri"), "http://127.0.0.1:43123/callback"; got != want {
		t.Fatalf("redirect_uri = %q, want %q", got, want)
	}
}

func TestAuthService_StartOAuth_RejectsUnknownRedirectHost(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
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

	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
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

func TestAuthService_SignupStoresChallengeAndSendsEmail(t *testing.T) {
	repo := newMockAuthRepo()
	sender := &mockAuthEmailSender{}
	svc := NewAuthService(repo, &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		EmailSender:  sender,
		EmailCodeTTL: time.Minute,
	})

	result, err := svc.Signup(context.Background(), domain.SignupParams{Email: "Alice@Example.com"})
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}
	if result.Email != "alice@example.com" {
		t.Fatalf("normalized email = %q, want alice@example.com", result.Email)
	}
	if len(sender.emails) != 1 || sender.emails[0] != "alice@example.com" {
		t.Fatalf("sent emails = %#v", sender.emails)
	}
	if len(sender.codes) != 1 || len(sender.codes[0]) != 6 {
		t.Fatalf("sent codes = %#v", sender.codes)
	}
	if len(repo.challenges) != 1 {
		t.Fatalf("expected one stored challenge, got %d", len(repo.challenges))
	}
	for _, challenge := range repo.challenges {
		if challenge.Email != "alice@example.com" {
			t.Fatalf("challenge email = %q", challenge.Email)
		}
		if challenge.CodeHash != crypto.HashToken(sender.codes[0]) {
			t.Fatalf("challenge hash = %q, want hash of sent code", challenge.CodeHash)
		}
		if challenge.ExpiresAt != result.ExpiresAt {
			t.Fatalf("expires_at mismatch: challenge=%v result=%v", challenge.ExpiresAt, result.ExpiresAt)
		}
	}
}

func TestAuthService_SignupRequiresConfiguredEmailSender(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoDefault{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})

	_, err := svc.Signup(context.Background(), domain.SignupParams{Email: "alice@example.com"})
	if !errors.Is(err, domain.ErrEmailAuthDisabled) {
		t.Fatalf("Signup() error = %v, want email auth disabled", err)
	}
}

func TestAuthService_VerifyCreatesPersonalWorkspaceAndSession(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	workspaceRepo := newMockWorkspaceRepo()
	accountRepo := newMockAccountRepo()
	sender := &mockAuthEmailSender{}
	svc := NewAuthService(repo, userRepo, workspaceRepo, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		EmailSender: sender,
	})
	svc.SetIdentityRepositories(accountRepo)

	if _, err := svc.Signup(context.Background(), domain.SignupParams{Email: "alice@example.com"}); err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	session, err := svc.Verify(context.Background(), domain.VerifyParams{
		Email: "alice@example.com",
		Code:  sender.codes[0],
		Name:  "Alice Example",
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if session.Provider != domain.AuthProviderEmail {
		t.Fatalf("session provider = %q, want email", session.Provider)
	}
	if session.WorkspaceID != "" || session.AccountID == "" || session.UserID != "" {
		t.Fatalf("unexpected session: %+v", session)
	}
	account, err := accountRepo.GetByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("expected created account: %v", err)
	}
	if account.ID != session.AccountID || account.Email != "alice@example.com" {
		t.Fatalf("unexpected created account: %+v", account)
	}
	if len(userRepo.users) != 0 {
		t.Fatalf("expected verify to avoid creating workspace users, got %d", len(userRepo.users))
	}
	for _, challenge := range repo.challenges {
		if challenge.ConsumedAt == nil {
			t.Fatal("expected challenge to be consumed")
		}
	}
}

func TestAuthService_VerifyDoesNotReuseEmailOnlyUserWithoutAccountMembership(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		WorkspaceID:   "T123",
		RealName:      "Existing Name",
		DisplayName:   "Existing Name",
		Email:         "alice@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	repo.challenges["EV_EXISTING"] = &domain.EmailVerificationChallenge{
		ID:        "EV_EXISTING",
		Email:     "alice@example.com",
		CodeHash:  crypto.HashToken("123456"),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		CreatedAt: time.Now().UTC(),
	}

	session, err := svc.Verify(context.Background(), domain.VerifyParams{
		Email: "alice@example.com",
		Code:  "123456",
		Name:  "Updated Name",
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if session.WorkspaceID != "" || session.AccountID == "" || session.UserID != "" {
		t.Fatalf("unexpected account-only session: %+v", session)
	}
	if got := userRepo.users["U_EXISTING"].RealName; got != "Existing Name" {
		t.Fatalf("existing user real_name = %q, want unchanged existing value", got)
	}
	if len(userRepo.users) != 1 {
		t.Fatalf("expected verify to leave workspace users untouched, got %d", len(userRepo.users))
	}
}

func TestAuthService_VerifyRejectsExpiredCode(t *testing.T) {
	repo := newMockAuthRepo()
	repo.challenges["EV_EXPIRED"] = &domain.EmailVerificationChallenge{
		ID:        "EV_EXPIRED",
		Email:     "alice@example.com",
		CodeHash:  crypto.HashToken("123456"),
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
		CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
	}

	svc := NewAuthService(repo, newMockUserRepoTenant(), newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	if _, err := svc.Verify(context.Background(), domain.VerifyParams{
		Email: "alice@example.com",
		Code:  "123456",
	}); !errors.Is(err, domain.ErrInvalidAuth) {
		t.Fatalf("Verify() error = %v, want invalid auth", err)
	}
}

func TestAuthService_ResolveOAuthLogin_DoesNotReuseEmailOnlyUserWithoutAccountMembership(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		WorkspaceID:   "T123",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	workspaceID, user, err := svc.resolveOAuthLogin(
		context.Background(),
		nil,
		userRepo,
		repo,
		nil,
		oauthState{},
		domain.AuthProviderGoogle,
		oauthProfile{
			Subject: "google-user-123",
			Email:   "JohnSuh94@gmail.com",
			Name:    "John Suh",
			Login:   "johnsuh94",
		},
	)
	if err != nil {
		t.Fatalf("resolveOAuthLogin() error = %v", err)
	}
	if workspaceID != "" || user.ID != "" || user.AccountID == "" || user.WorkspaceID != "" {
		t.Fatalf("unexpected oauth login target: workspace=%q user=%q", workspaceID, user.ID)
	}
	if len(userRepo.users) != 1 {
		t.Fatalf("expected oauth login to avoid creating a workspace user, got %d users", len(userRepo.users))
	}

	accounts, err := repo.ListOAuthAccountsBySubject(context.Background(), domain.AuthProviderGoogle, "google-user-123")
	if err != nil {
		t.Fatalf("ListOAuthAccountsBySubject() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected one linked oauth account, got %d", len(accounts))
	}
	if accounts[0].AccountID == "" || accounts[0].UserID != "" || accounts[0].WorkspaceID != "" {
		t.Fatalf("unexpected linked oauth account: %+v", accounts[0])
	}
	if accounts[0].Email != "johnsuh94@gmail.com" {
		t.Fatalf("oauth account email = %q, want normalized lower-case email", accounts[0].Email)
	}
}

func TestAuthService_ResolveOAuthLogin_UsesAccountMembershipsBeforeEmailOnlyScan(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "workspace-local@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["johnsuh94@gmail.com"] = accountRepo.byID["A123"]
	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	workspaceID, user, err := svc.resolveOAuthLogin(
		context.Background(),
		nil,
		userRepo,
		repo,
		nil,
		oauthState{WorkspaceID: "T123"},
		domain.AuthProviderGoogle,
		oauthProfile{
			Subject: "google-user-456",
			Email:   "JohnSuh94@gmail.com",
			Name:    "John Suh",
			Login:   "johnsuh94",
		},
	)
	if err != nil {
		t.Fatalf("resolveOAuthLogin() error = %v", err)
	}
	if workspaceID != "T123" || user.ID != "U_EXISTING" {
		t.Fatalf("unexpected oauth login target: workspace=%q user=%q", workspaceID, user.ID)
	}
}

func TestAuthService_ResolveOAuthLogin_InviteReusesExistingWorkspaceUser(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	inviteRepo := newMockWorkspaceInviteRepo()
	accountRepo := newMockAccountRepo()
	token := "invite_oauth_existing_user"

	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		AccountID:     "A123",
		WorkspaceID:   "T_INVITED",
		Email:         "invite@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "invite@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["invite@example.com"] = accountRepo.byID["A123"]
	inviteRepo.invites[crypto.HashToken(token)] = &domain.WorkspaceInvite{
		ID:          "WI1",
		WorkspaceID: "T_INVITED",
		Email:       "invite@example.com",
		InvitedBy:   "U_ADMIN",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), inviteRepo, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	workspaceID, user, err := svc.resolveOAuthLogin(
		context.Background(),
		nil,
		userRepo,
		repo,
		inviteRepo,
		oauthState{InviteToken: token},
		domain.AuthProviderGoogle,
		oauthProfile{
			Subject: "google-invite-123",
			Email:   "invite@example.com",
			Name:    "Invite User",
			Login:   "invite-user",
		},
	)
	if err != nil {
		t.Fatalf("resolveOAuthLogin() error = %v", err)
	}
	if workspaceID != "T_INVITED" || user.ID != "U_EXISTING" {
		t.Fatalf("unexpected invite oauth target: workspace=%q user=%q", workspaceID, user.ID)
	}
	if inviteRepo.invites[crypto.HashToken(token)].AcceptedByAccountID != "A123" {
		t.Fatalf("expected invite acceptance to reuse the existing workspace user, got %+v", inviteRepo.invites[crypto.HashToken(token)])
	}
	accounts, err := repo.ListOAuthAccountsBySubject(context.Background(), domain.AuthProviderGoogle, "google-invite-123")
	if err != nil {
		t.Fatalf("ListOAuthAccountsBySubject() error = %v", err)
	}
	if len(accounts) != 1 || accounts[0].AccountID != "A123" || accounts[0].UserID != "" || accounts[0].WorkspaceID != "" {
		t.Fatalf("unexpected linked oauth account: %+v", accounts)
	}
}

func TestAuthService_VerifyUsesAccountLinkedUsersBeforeEmailOnlyScan(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Email:         "workspace-local@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["johnsuh94@gmail.com"] = accountRepo.byID["A123"]
	repo.challenges["EV_EXISTING"] = &domain.EmailVerificationChallenge{
		ID:        "EV_EXISTING",
		Email:     "JohnSuh94@gmail.com",
		CodeHash:  crypto.HashToken("123456"),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		CreatedAt: time.Now().UTC(),
	}

	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	session, err := svc.Verify(context.Background(), domain.VerifyParams{
		Email: "JohnSuh94@gmail.com",
		Code:  "123456",
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if session.WorkspaceID != "" || session.AccountID != "A123" || session.UserID != "" {
		t.Fatalf("unexpected account-first session for linked account: %+v", session)
	}
}

func TestAuthService_VerifyCreatesWorkspaceUserWhenAccountHasNoWorkspaceUser(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["johnsuh94@gmail.com"] = accountRepo.byID["A123"]
	repo.challenges["EV_EXISTING"] = &domain.EmailVerificationChallenge{
		ID:        "EV_EXISTING",
		Email:     "JohnSuh94@gmail.com",
		CodeHash:  crypto.HashToken("123456"),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		CreatedAt: time.Now().UTC(),
	}

	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	session, err := svc.Verify(context.Background(), domain.VerifyParams{
		Email: "JohnSuh94@gmail.com",
		Code:  "123456",
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if session.WorkspaceID != "" || session.AccountID != "A123" || session.UserID != "" {
		t.Fatalf("expected account-only session for the existing account: %+v", session)
	}
	if len(userRepo.users) != 0 {
		t.Fatalf("expected verify to avoid creating workspace users, got %d", len(userRepo.users))
	}
}

func TestAuthService_GetCurrentIdentityUsesRequestedWorkspaceMembershipForSharedAccount(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	userRepo.users["U_T123"] = &domain.User{
		ID:            "U_T123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	}
	userRepo.users["U_T999"] = &domain.User{
		ID:            "U_T999",
		AccountID:     "A123",
		WorkspaceID:   "T999",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(newMockAuthRepo(), userRepo, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo, nil)

	ctx := ctxutil.WithIdentity(context.Background(), "A123")
	ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, "T999")

	account, user, err := svc.GetCurrentIdentity(ctx)
	if err != nil {
		t.Fatalf("GetCurrentIdentity() error = %v", err)
	}
	if account == nil || account.ID != "A123" {
		t.Fatalf("expected current account A123, got %+v", account)
	}
	if user == nil {
		t.Fatalf("expected workspace actor for requested membership, got %+v", user)
	}
	if user.ID != "" || user.AccountID != "A123" || user.WorkspaceID != "T999" {
		t.Fatalf("expected synthesized membership actor for T999/A123, got %+v", user)
	}
	if user.EffectiveAccountType() != domain.AccountTypeMember {
		t.Fatalf("expected member account type from T999 membership, got %+v", user)
	}
}

func TestAuthService_CreateOAuthUserSyncsIdentity(t *testing.T) {
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	svc := NewAuthService(newMockAuthRepo(), userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)

	user, err := svc.createOAuthUser(context.Background(), nil, userRepo, "T123", oauthProfile{
		Email: "new@example.com",
		Login: "new-user",
		Name:  "New User",
	}, domain.AccountTypeMember)
	if err != nil {
		t.Fatalf("createOAuthUser() error = %v", err)
	}
	account, err := accountRepo.GetByEmail(context.Background(), "new@example.com")
	if err != nil {
		t.Fatalf("expected synced account: %v", err)
	}
	if user.AccountID != account.ID || user.WorkspaceID != "T123" {
		t.Fatalf("unexpected synced identity: account=%+v user=%+v", account, user)
	}
}

func TestAuthService_VerifyDoesNotReuseOAuthEmailOnlyUserWithoutAccountMembership(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		WorkspaceID:   "T123",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	if _, err := repo.UpsertOAuthAccount(context.Background(), domain.UpsertOAuthAccountParams{
		WorkspaceID:     "T123",
		UserID:          "U_EXISTING",
		Provider:        domain.AuthProviderGoogle,
		ProviderSubject: "google-user-123",
		Email:           "johnsuh94@gmail.com",
	}); err != nil {
		t.Fatalf("UpsertOAuthAccount() error = %v", err)
	}

	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})
	svc.SetIdentityRepositories(accountRepo)
	repo.challenges["EV_EXISTING"] = &domain.EmailVerificationChallenge{
		ID:        "EV_EXISTING",
		Email:     "JohnSuh94@gmail.com",
		CodeHash:  crypto.HashToken("123456"),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		CreatedAt: time.Now().UTC(),
	}

	session, err := svc.Verify(context.Background(), domain.VerifyParams{
		Email: "JohnSuh94@gmail.com",
		Code:  "123456",
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if session.WorkspaceID != "" || session.AccountID == "" || session.UserID != "" {
		t.Fatalf("unexpected account-first session for oauth-linked email: %+v", session)
	}
	if len(userRepo.users) != 1 {
		t.Fatalf("expected verify to avoid creating workspace users, got %d users", len(userRepo.users))
	}
}
