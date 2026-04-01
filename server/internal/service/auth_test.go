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

func (m *mockAuthRepo) GetOAuthAccount(_ context.Context, workspaceID string, provider domain.AuthProvider, providerSubject string) (*domain.OAuthAccount, error) {
	account, ok := m.oauthAccounts[oauthAccountKey(workspaceID, provider, providerSubject)]
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
	return workspaceID + "|" + string(provider) + "|" + providerSubject
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

func TestAuthService_StartCLIOAuth_AllowsLocalhostCallback(t *testing.T) {
	svc := NewAuthService(newMockAuthRepo(), &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
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

func TestAuthService_SignupStoresChallengeAndSendsEmail(t *testing.T) {
	repo := newMockAuthRepo()
	sender := &mockAuthEmailSender{}
	svc := NewAuthService(repo, &mockUserRepoForUG{}, nil, nil, nil, mockTxBeginner{}, nil, AuthConfig{
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

func TestAuthService_VerifyCreatesPersonalWorkspaceAndSession(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	workspaceRepo := newMockWorkspaceRepo()
	sender := &mockAuthEmailSender{}
	svc := NewAuthService(repo, userRepo, workspaceRepo, nil, nil, mockTxBeginner{}, nil, AuthConfig{
		EmailSender: sender,
	})

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
	if session.WorkspaceID != "T_NEW" || session.UserID != "U123" {
		t.Fatalf("unexpected session: %+v", session)
	}
	user, err := userRepo.Get(context.Background(), "U123")
	if err != nil {
		t.Fatalf("expected created user: %v", err)
	}
	if user.Email != "alice@example.com" || user.AccountType != domain.AccountTypePrimaryAdmin {
		t.Fatalf("unexpected created user: %+v", user)
	}
	if user.RealName != "Alice Example" || user.DisplayName != "Alice Example" {
		t.Fatalf("expected created user name to come from verify params, got %+v", user)
	}
	for _, challenge := range repo.challenges {
		if challenge.ConsumedAt == nil {
			t.Fatal("expected challenge to be consumed")
		}
	}
}

func TestAuthService_VerifyUsesExistingUser(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
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
	if session.WorkspaceID != "T123" || session.UserID != "U_EXISTING" {
		t.Fatalf("unexpected session for existing user: %+v", session)
	}
	if got := userRepo.users["U_EXISTING"].RealName; got != "Existing Name" {
		t.Fatalf("existing user real_name = %q, want unchanged existing value", got)
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

func TestAuthService_ResolveOAuthLogin_ReusesExistingEmailUser(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U_EXISTING"] = &domain.User{
		ID:            "U_EXISTING",
		WorkspaceID:   "T123",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}

	svc := NewAuthService(repo, userRepo, newMockWorkspaceRepo(), nil, nil, mockTxBeginner{}, nil, AuthConfig{})

	workspaceID, user, err := svc.resolveOAuthLogin(
		context.Background(),
		nil,
		userRepo,
		repo,
		newMockWorkspaceRepo(),
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
	if workspaceID != "T123" || user.ID != "U_EXISTING" {
		t.Fatalf("unexpected oauth login target: workspace=%q user=%q", workspaceID, user.ID)
	}
	if len(userRepo.users) != 1 {
		t.Fatalf("expected oauth login to reuse existing user, got %d users", len(userRepo.users))
	}

	accounts, err := repo.ListOAuthAccountsBySubject(context.Background(), domain.AuthProviderGoogle, "google-user-123")
	if err != nil {
		t.Fatalf("ListOAuthAccountsBySubject() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected one linked oauth account, got %d", len(accounts))
	}
	if accounts[0].UserID != "U_EXISTING" || accounts[0].WorkspaceID != "T123" {
		t.Fatalf("unexpected linked oauth account: %+v", accounts[0])
	}
	if accounts[0].Email != "johnsuh94@gmail.com" {
		t.Fatalf("oauth account email = %q, want normalized lower-case email", accounts[0].Email)
	}
}

func TestAuthService_VerifyReusesExistingOAuthUser(t *testing.T) {
	repo := newMockAuthRepo()
	userRepo := newMockUserRepoTenant()
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
	if session.WorkspaceID != "T123" || session.UserID != "U_EXISTING" {
		t.Fatalf("unexpected session for existing oauth user: %+v", session)
	}
	if len(userRepo.users) != 1 {
		t.Fatalf("expected verify to reuse existing oauth user, got %d users", len(userRepo.users))
	}
}
