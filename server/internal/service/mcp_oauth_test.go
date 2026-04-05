package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockMCPOAuthRepo struct {
	codeSeq    int
	refreshSeq int
	codes      map[string]*domain.MCPOAuthAuthorizationCode
	refreshes  map[string]*domain.MCPOAuthRefreshToken
}

func newMockMCPOAuthRepo() *mockMCPOAuthRepo {
	return &mockMCPOAuthRepo{
		codes:     map[string]*domain.MCPOAuthAuthorizationCode{},
		refreshes: map[string]*domain.MCPOAuthRefreshToken{},
	}
}

func (m *mockMCPOAuthRepo) WithTx(_ pgx.Tx) repository.MCPOAuthRepository { return m }

func (m *mockMCPOAuthRepo) CreateAuthorizationCode(_ context.Context, params domain.CreateMCPOAuthAuthorizationCodeParams) (*domain.MCPOAuthAuthorizationCode, string, error) {
	m.codeSeq++
	raw := "code-" + string(rune('0'+m.codeSeq))
	now := time.Now().UTC()
	code := &domain.MCPOAuthAuthorizationCode{
		ID:                  "OAC1",
		CodeHash:            hashString(raw),
		ClientID:            params.ClientID,
		ClientName:          params.ClientName,
		RedirectURI:         params.RedirectURI,
		WorkspaceID:         params.WorkspaceID,
		UserID:              params.UserID,
		Scopes:              append([]string(nil), params.Scopes...),
		Resource:            params.Resource,
		CodeChallenge:       params.CodeChallenge,
		CodeChallengeMethod: params.CodeChallengeMethod,
		ExpiresAt:           params.ExpiresAt,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	m.codes[code.CodeHash] = code
	return code, raw, nil
}

func (m *mockMCPOAuthRepo) GetAuthorizationCodeByHash(_ context.Context, codeHash string) (*domain.MCPOAuthAuthorizationCode, error) {
	code, ok := m.codes[codeHash]
	if !ok {
		return nil, domain.ErrInvalidAuth
	}
	return code, nil
}

func (m *mockMCPOAuthRepo) MarkAuthorizationCodeUsed(_ context.Context, id string, usedAt time.Time) error {
	for _, code := range m.codes {
		if code.ID == id {
			code.UsedAt = &usedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (m *mockMCPOAuthRepo) CreateRefreshToken(_ context.Context, params domain.CreateMCPOAuthRefreshTokenParams) (*domain.MCPOAuthRefreshToken, string, error) {
	m.refreshSeq++
	raw := "refresh-" + string(rune('0'+m.refreshSeq))
	now := time.Now().UTC()
	token := &domain.MCPOAuthRefreshToken{
		ID:          "ORT1",
		TokenHash:   hashString(raw),
		ClientID:    params.ClientID,
		ClientName:  params.ClientName,
		WorkspaceID: params.WorkspaceID,
		UserID:      params.UserID,
		Scopes:      append([]string(nil), params.Scopes...),
		Resource:    params.Resource,
		ExpiresAt:   params.ExpiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.refreshes[token.TokenHash] = token
	return token, raw, nil
}

func (m *mockMCPOAuthRepo) GetRefreshTokenByHash(_ context.Context, tokenHash string) (*domain.MCPOAuthRefreshToken, error) {
	token, ok := m.refreshes[tokenHash]
	if !ok {
		return nil, domain.ErrInvalidAuth
	}
	return token, nil
}

func (m *mockMCPOAuthRepo) RotateRefreshToken(_ context.Context, oldID, newID string, revokedAt time.Time) error {
	for _, token := range m.refreshes {
		if token.ID == oldID {
			token.RotatedToID = newID
			token.RevokedAt = &revokedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (m *mockMCPOAuthRepo) RevokeRefreshToken(_ context.Context, id string, revokedAt time.Time) error {
	for _, token := range m.refreshes {
		if token.ID == id {
			token.RevokedAt = &revokedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func TestMCPOAuthService_AuthorizeAndExchangeCode(t *testing.T) {
	clientID := ""
	clientMeta := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"client_id":"` + clientID + `",
			"client_name":"Codex Test Client",
			"redirect_uris":["http://127.0.0.1:3000/callback"],
			"response_types":["code"],
			"token_endpoint_auth_method":"none"
		}`))
	}))
	defer clientMeta.Close()
	clientID = clientMeta.URL + "/client.json"

	repo := newMockMCPOAuthRepo()
	userRepo := newMockUserRepoTenant()
	userRepo.users["U123"] = &domain.User{
		ID:            "U123",
		AccountID:     "A123",
		WorkspaceID:   "T123",
		Name:          "Existing Name",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}
	svc := NewMCPOAuthService(repo, userRepo, mockTxBeginner{}, nil, MCPOAuthConfig{
		Issuer:     "https://api.teraslack.ai",
		MCPBaseURL: "https://mcp.teraslack.ai/mcp",
		SigningKey: "test-signing-key",
		HTTPClient: clientMeta.Client(),
	})

	prompt, err := svc.BuildAuthorizePrompt(context.Background(), &domain.AuthContext{
		WorkspaceID: "T123",
		UserID:      "U123",
	}, domain.MCPOAuthAuthorizeRequest{
		ResponseType:        "code",
		ClientID:            clientID,
		RedirectURI:         "http://127.0.0.1:3000/callback",
		Scope:               "mcp:tools offline_access messages.read",
		State:               "state123",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		Resource:            "https://mcp.teraslack.ai/mcp",
	})
	if err != nil {
		t.Fatalf("BuildAuthorizePrompt() error = %v", err)
	}
	if prompt.Client.ClientName != "Codex Test Client" {
		t.Fatalf("client name = %q", prompt.Client.ClientName)
	}

	redirectURL, err := svc.CompleteAuthorize(context.Background(), &domain.AuthContext{
		WorkspaceID: "T123",
		UserID:      "U123",
	}, domain.MCPOAuthApproveRequest{
		MCPOAuthAuthorizeRequest: prompt.Request,
		Approved:                 true,
	})
	if err != nil {
		t.Fatalf("CompleteAuthorize() error = %v", err)
	}
	redirect, err := url.Parse(redirectURL)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}
	code := redirect.Query().Get("code")
	if code == "" {
		t.Fatalf("expected authorization code in redirect URL %q", redirectURL)
	}

	tokenResp, err := svc.ExchangeToken(context.Background(), domain.MCPOAuthTokenExchangeParams{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "http://127.0.0.1:3000/callback",
		ClientID:     clientID,
		CodeVerifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		Resource:     "https://mcp.teraslack.ai/mcp",
	})
	if err != nil {
		t.Fatalf("ExchangeToken() error = %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if tokenResp.RefreshToken == "" {
		t.Fatal("expected refresh token for offline_access")
	}

	auth, err := svc.ValidateAccessToken(context.Background(), tokenResp.AccessToken, "https://mcp.teraslack.ai/mcp")
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if auth.UserID != "U123" || auth.WorkspaceID != "T123" {
		t.Fatalf("unexpected auth context: %+v", auth)
	}
	if auth.AccountID != "A123" {
		t.Fatalf("unexpected identity context: %+v", auth)
	}
	if auth.AccountType != domain.AccountTypeMember {
		t.Fatalf("expected canonical user account type, got %+v", auth)
	}
	if !containsOAuthValue(auth.Scopes, domain.MCPOAuthScopeTools) || !containsOAuthValue(auth.Permissions, domain.PermissionMessagesRead) {
		t.Fatalf("unexpected scopes/permissions: %+v", auth)
	}
}

func TestMCPOAuthService_BuildAuthorizePrompt_AllowsLoopbackRedirectPortForClientMetadata(t *testing.T) {
	clientID := ""
	clientMeta := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"client_id":"` + clientID + `",
			"client_name":"Claude Code",
			"redirect_uris":["http://localhost/callback","http://127.0.0.1/callback"],
			"response_types":["code"],
			"token_endpoint_auth_method":"none"
		}`))
	}))
	defer clientMeta.Close()
	clientID = clientMeta.URL + "/client.json"

	svc := NewMCPOAuthService(newMockMCPOAuthRepo(), &mockUserRepoDefault{}, mockTxBeginner{}, nil, MCPOAuthConfig{
		Issuer:     "https://api.teraslack.ai",
		MCPBaseURL: "https://mcp.teraslack.ai/mcp",
		SigningKey: "test-signing-key",
		HTTPClient: clientMeta.Client(),
	})

	prompt, err := svc.BuildAuthorizePrompt(context.Background(), &domain.AuthContext{
		WorkspaceID: "T123",
		UserID:      "U123",
	}, domain.MCPOAuthAuthorizeRequest{
		ResponseType:        "code",
		ClientID:            clientID,
		RedirectURI:         "http://localhost:54482/callback",
		Scope:               "mcp:tools",
		State:               "state123",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		Resource:            "https://mcp.teraslack.ai/mcp",
	})
	if err != nil {
		t.Fatalf("BuildAuthorizePrompt() error = %v", err)
	}
	if prompt.Request.RedirectURI != "http://localhost:54482/callback" {
		t.Fatalf("redirect URI = %q", prompt.Request.RedirectURI)
	}
}

func TestMCPOAuthService_BuildAuthorizePrompt_RejectsMismatchedLoopbackRedirectPath(t *testing.T) {
	clientID := ""
	clientMeta := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"client_id":"` + clientID + `",
			"client_name":"Claude Code",
			"redirect_uris":["http://localhost/callback"],
			"response_types":["code"],
			"token_endpoint_auth_method":"none"
		}`))
	}))
	defer clientMeta.Close()
	clientID = clientMeta.URL + "/client.json"

	svc := NewMCPOAuthService(newMockMCPOAuthRepo(), &mockUserRepoDefault{}, mockTxBeginner{}, nil, MCPOAuthConfig{
		Issuer:     "https://api.teraslack.ai",
		MCPBaseURL: "https://mcp.teraslack.ai/mcp",
		SigningKey: "test-signing-key",
		HTTPClient: clientMeta.Client(),
	})

	_, err := svc.BuildAuthorizePrompt(context.Background(), &domain.AuthContext{
		WorkspaceID: "T123",
		UserID:      "U123",
	}, domain.MCPOAuthAuthorizeRequest{
		ResponseType:        "code",
		ClientID:            clientID,
		RedirectURI:         "http://localhost:54482/not-callback",
		Scope:               "mcp:tools",
		State:               "state123",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		Resource:            "https://mcp.teraslack.ai/mcp",
	})
	if err == nil {
		t.Fatal("expected BuildAuthorizePrompt() error")
	}

	var oauthErr *domain.OAuthProtocolError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("expected OAuthProtocolError, got %T", err)
	}
	if oauthErr.Code != "invalid_request" || oauthErr.Description != "redirect_uri is not registered for this client" {
		t.Fatalf("unexpected OAuth error: %+v", oauthErr)
	}
}

func TestMCPOAuthService_BuildAuthorizePrompt_RejectsUserlessWorkspaceIdentity(t *testing.T) {
	clientID := ""
	clientMeta := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"client_id":"` + clientID + `",
			"client_name":"Codex Test Client",
			"redirect_uris":["http://127.0.0.1:3000/callback"],
			"response_types":["code"],
			"token_endpoint_auth_method":"none"
		}`))
	}))
	defer clientMeta.Close()
	clientID = clientMeta.URL + "/client.json"

	userRepo := newMockUserRepoTenant()
	accountRepo := newMockAccountRepo()
	accountRepo.byID["A123"] = &domain.Account{
		ID:            "A123",
		Email:         "member@example.com",
		PrincipalType: domain.PrincipalTypeHuman,
	}
	accountRepo.byEmail["member@example.com"] = accountRepo.byID["A123"]

	svc := NewMCPOAuthService(newMockMCPOAuthRepo(), userRepo, mockTxBeginner{}, nil, MCPOAuthConfig{
		Issuer:     "https://api.teraslack.ai",
		MCPBaseURL: "https://mcp.teraslack.ai/mcp",
		SigningKey: "test-signing-key",
		HTTPClient: clientMeta.Client(),
	})
	svc.SetIdentityRepositories(accountRepo)

	_, err := svc.BuildAuthorizePrompt(context.Background(), &domain.AuthContext{
		WorkspaceID: "T123",
		AccountID:   "A123",
	}, domain.MCPOAuthAuthorizeRequest{
		ResponseType:        "code",
		ClientID:            clientID,
		RedirectURI:         "http://127.0.0.1:3000/callback",
		Scope:               "mcp:tools",
		State:               "state123",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		Resource:            "https://mcp.teraslack.ai/mcp",
	})
	if err == nil {
		t.Fatal("expected invalid user session for account-without-user auth context")
	}
}

func hashString(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func containsOAuthValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
