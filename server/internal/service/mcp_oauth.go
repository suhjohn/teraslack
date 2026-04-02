package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/mcpoauth"
	"github.com/suhjohn/teraslack/internal/repository"
)

type MCPOAuthConfig struct {
	Issuer     string
	MCPBaseURL string
	SigningKey string
	HTTPClient *http.Client
}

type MCPOAuthService struct {
	repo        repository.MCPOAuthRepository
	accountRepo repository.AccountRepository
	userRepo    repository.UserRepository
	db          repository.TxBeginner
	httpClient  *http.Client
	logger      *slog.Logger
	cfg         mcpoauth.TokenConfig
}

func NewMCPOAuthService(repo repository.MCPOAuthRepository, userRepo repository.UserRepository, db repository.TxBeginner, logger *slog.Logger, cfg MCPOAuthConfig) *MCPOAuthService {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &MCPOAuthService{
		repo:       repo,
		userRepo:   userRepo,
		db:         db,
		httpClient: client,
		logger:     logger,
		cfg: mcpoauth.TokenConfig{
			Issuer:      cfg.Issuer,
			MCPAudience: cfg.MCPBaseURL,
			APIAudience: cfg.Issuer,
			SigningKey:  cfg.SigningKey,
		},
	}
}

func (s *MCPOAuthService) SetIdentityRepositories(accountRepo repository.AccountRepository, _ ...any) {
	s.accountRepo = accountRepo
}

func (s *MCPOAuthService) AuthorizationServerMetadata() (*domain.MCPOAuthAuthorizationServerMetadata, error) {
	issuer, err := mcpoauth.CanonicalURL(s.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	return &domain.MCPOAuthAuthorizationServerMetadata{
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + "/oauth/authorize",
		TokenEndpoint:                     issuer + "/oauth/token",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ScopesSupported:                   domain.MCPOAuthSupportedScopes,
		ClientIDMetadataDocumentSupported: true,
	}, nil
}

func (s *MCPOAuthService) ProtectedResourceMetadata() (*domain.MCPOAuthProtectedResourceMetadata, error) {
	resource, err := mcpoauth.CanonicalURL(s.cfg.MCPAudience)
	if err != nil {
		return nil, err
	}
	issuer, err := mcpoauth.CanonicalURL(s.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	return &domain.MCPOAuthProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{issuer},
		ScopesSupported:        domain.MCPOAuthSupportedScopes,
		BearerMethodsSupported: []string{"header"},
	}, nil
}

func (s *MCPOAuthService) BuildAuthorizePrompt(ctx context.Context, auth *domain.AuthContext, req domain.MCPOAuthAuthorizeRequest) (*domain.MCPOAuthAuthorizePrompt, error) {
	client, scopes, err := s.validateAuthorizeRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	user, err := resolveAuthContextUser(ctx, s.userRepo, auth)
	if err != nil {
		return nil, oauthInvalidRequest("invalid user session")
	}
	workspaceID := user.WorkspaceID
	if workspaceID == "" {
		workspaceID = auth.WorkspaceID
	}
	return &domain.MCPOAuthAuthorizePrompt{
		Request:         req,
		Client:          *client,
		WorkspaceID:     workspaceID,
		UserID:          user.ID,
		UserName:        user.Name,
		UserEmail:       user.Email,
		RequestedScopes: scopes,
	}, nil
}

func (s *MCPOAuthService) CompleteAuthorize(ctx context.Context, auth *domain.AuthContext, req domain.MCPOAuthApproveRequest) (string, error) {
	client, scopes, err := s.validateAuthorizeRequest(ctx, req.MCPOAuthAuthorizeRequest)
	if err != nil {
		return "", err
	}
	if !req.Approved {
		return oauthRedirectURL(req.RedirectURI, map[string]string{
			"error":             "access_denied",
			"error_description": "The request was denied.",
			"state":             req.State,
		})
	}

	user, err := resolveAuthContextUser(ctx, s.userRepo, auth)
	if err != nil {
		return "", oauthInvalidRequest("invalid user session")
	}
	workspaceID := user.WorkspaceID
	if workspaceID == "" {
		workspaceID = auth.WorkspaceID
	}

	code, rawCode, err := s.repo.CreateAuthorizationCode(ctx, domain.CreateMCPOAuthAuthorizationCodeParams{
		ClientID:            client.ClientID,
		ClientName:          client.ClientName,
		RedirectURI:         req.RedirectURI,
		WorkspaceID:         workspaceID,
		UserID:              user.ID,
		Scopes:              scopes,
		Resource:            req.Resource,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		ExpiresAt:           time.Now().UTC().Add(mcpoauth.AuthCodeTTL),
	})
	if err != nil {
		return "", fmt.Errorf("create auth code: %w", err)
	}
	_ = code
	return oauthRedirectURL(req.RedirectURI, map[string]string{
		"code":  rawCode,
		"state": req.State,
	})
}

func (s *MCPOAuthService) ExchangeToken(ctx context.Context, params domain.MCPOAuthTokenExchangeParams) (*domain.MCPOAuthTokenResponse, error) {
	switch params.GrantType {
	case "authorization_code":
		return s.exchangeAuthorizationCode(ctx, params)
	case "refresh_token":
		return s.exchangeRefreshToken(ctx, params)
	default:
		return nil, oauthUnsupportedGrant("grant_type must be authorization_code or refresh_token")
	}
}

func (s *MCPOAuthService) ValidateAccessToken(ctx context.Context, raw, audience string) (*domain.AuthContext, error) {
	claims, err := mcpoauth.ValidateAccessToken(s.cfg, raw, audience)
	if err != nil {
		return nil, err
	}
	return &domain.AuthContext{
		WorkspaceID:   claims.WorkspaceID,
		UserID:        claims.UserID,
		AccountID:     claims.AccountID,
		PrincipalType: domain.PrincipalType(claims.PrincipalType),
		AccountType:   domain.AccountType(claims.AccountType),
		IsBot:         claims.IsBot,
		Permissions:   append([]string(nil), claims.Permissions...),
		Scopes:        mcpoauth.NormalizeScopes(claims.Scope),
	}, nil
}

func (s *MCPOAuthService) ValidateAPIAccessToken(ctx context.Context, raw string) (*domain.AuthContext, error) {
	return s.ValidateAccessToken(ctx, raw, s.cfg.APIAudience)
}

func (s *MCPOAuthService) exchangeAuthorizationCode(ctx context.Context, params domain.MCPOAuthTokenExchangeParams) (*domain.MCPOAuthTokenResponse, error) {
	if strings.TrimSpace(params.Code) == "" {
		return nil, oauthInvalidGrant("code is required")
	}
	if strings.TrimSpace(params.RedirectURI) == "" {
		return nil, oauthInvalidGrant("redirect_uri is required")
	}
	if strings.TrimSpace(params.ClientID) == "" {
		return nil, oauthInvalidClient("client_id is required")
	}
	if strings.TrimSpace(params.CodeVerifier) == "" {
		return nil, oauthInvalidGrant("code_verifier is required")
	}
	if strings.TrimSpace(params.Resource) == "" {
		return nil, oauthInvalidGrant("resource is required")
	}

	code, err := s.repo.GetAuthorizationCodeByHash(ctx, sha256Hex(params.Code))
	if err != nil {
		return nil, oauthInvalidGrant("authorization code is invalid")
	}
	if code.ClientID != params.ClientID || code.RedirectURI != params.RedirectURI {
		return nil, oauthInvalidGrant("authorization code does not match client or redirect URI")
	}
	if err := mcpoauth.ValidateResource(params.Resource, code.Resource); err != nil {
		return nil, oauthInvalidGrant(err.Error())
	}
	if code.UsedAt != nil || time.Now().UTC().After(code.ExpiresAt) {
		return nil, oauthInvalidGrant("authorization code is expired or already used")
	}
	if !mcpoauth.VerifyPKCEVerifier(code.CodeChallenge, params.CodeVerifier) {
		return nil, oauthInvalidGrant("code_verifier does not match code_challenge")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	repoTx := s.repo.WithTx(tx)
	if err := repoTx.MarkAuthorizationCodeUsed(ctx, code.ID, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("mark auth code used: %w", err)
	}

	response, err := s.issueTokenResponse(ctx, tx, code.ClientID, code.ClientName, code.WorkspaceID, code.UserID, code.Scopes, code.Resource)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return response, nil
}

func (s *MCPOAuthService) exchangeRefreshToken(ctx context.Context, params domain.MCPOAuthTokenExchangeParams) (*domain.MCPOAuthTokenResponse, error) {
	if strings.TrimSpace(params.RefreshToken) == "" {
		return nil, oauthInvalidGrant("refresh_token is required")
	}
	if strings.TrimSpace(params.ClientID) == "" {
		return nil, oauthInvalidClient("client_id is required")
	}
	if strings.TrimSpace(params.Resource) == "" {
		return nil, oauthInvalidGrant("resource is required")
	}

	token, err := s.repo.GetRefreshTokenByHash(ctx, sha256Hex(params.RefreshToken))
	if err != nil {
		return nil, oauthInvalidGrant("refresh_token is invalid")
	}
	if token.ClientID != params.ClientID {
		return nil, oauthInvalidGrant("refresh_token does not match client")
	}
	if err := mcpoauth.ValidateResource(params.Resource, token.Resource); err != nil {
		return nil, oauthInvalidGrant(err.Error())
	}
	if token.RevokedAt != nil || time.Now().UTC().After(token.ExpiresAt) {
		return nil, oauthInvalidGrant("refresh_token is expired or revoked")
	}

	scopes := token.Scopes
	if requested := mcpoauth.NormalizeScopes(params.Scope); len(requested) > 0 {
		for _, scope := range requested {
			if !slices.Contains(scopes, scope) {
				return nil, oauthInvalidScope("requested scope exceeds original grant")
			}
		}
		scopes = requested
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	repoTx := s.repo.WithTx(tx)
	response, newToken, err := s.issueTokenResponseWithRefresh(ctx, tx, token.ClientID, token.ClientName, token.WorkspaceID, token.UserID, scopes, token.Resource)
	if err != nil {
		return nil, err
	}
	if newToken != nil {
		if err := repoTx.RotateRefreshToken(ctx, token.ID, newToken.ID, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("rotate refresh token: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return response, nil
}

func (s *MCPOAuthService) issueTokenResponse(ctx context.Context, tx pgx.Tx, clientID, clientName, workspaceID, userID string, scopes []string, resource string) (*domain.MCPOAuthTokenResponse, error) {
	response, _, err := s.issueTokenResponseWithRefresh(ctx, tx, clientID, clientName, workspaceID, userID, scopes, resource)
	return response, err
}

func (s *MCPOAuthService) issueTokenResponseWithRefresh(ctx context.Context, tx pgx.Tx, clientID, clientName, workspaceID, userID string, scopes []string, resource string) (*domain.MCPOAuthTokenResponse, *domain.MCPOAuthRefreshToken, error) {
	user, err := s.userRepo.WithTx(tx).Get(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("load user: %w", err)
	}
	if user.WorkspaceID != "" {
		workspaceID = user.WorkspaceID
	}

	accessToken, expiresIn, err := mcpoauth.IssueAccessToken(s.cfg, time.Now().UTC(), mcpoauth.AccessTokenClaims{
		WorkspaceID:   workspaceID,
		UserID:        user.ID,
		AccountID:     user.AccountID,
		PrincipalType: string(user.PrincipalType),
		AccountType:   string(user.EffectiveAccountType()),
		IsBot:         user.IsBot,
		ClientID:      clientID,
		Scope:         mcpoauth.ScopeString(scopes),
		Permissions:   mcpoauth.PermissionsFromScopes(scopes),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("issue access token: %w", err)
	}

	response := &domain.MCPOAuthTokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
		Scope:       mcpoauth.ScopeString(scopes),
	}

	if !slices.Contains(scopes, domain.MCPOAuthScopeOfflineAccess) {
		return response, nil, nil
	}

	refresh, rawRefresh, err := s.repo.WithTx(tx).CreateRefreshToken(ctx, domain.CreateMCPOAuthRefreshTokenParams{
		ClientID:    clientID,
		ClientName:  clientName,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Scopes:      scopes,
		Resource:    resource,
		ExpiresAt:   time.Now().UTC().Add(mcpoauth.RefreshTokenTTL),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create refresh token: %w", err)
	}
	response.RefreshToken = rawRefresh
	return response, refresh, nil
}

func (s *MCPOAuthService) validateAuthorizeRequest(ctx context.Context, req domain.MCPOAuthAuthorizeRequest) (*domain.MCPOAuthClientMetadata, []string, error) {
	if strings.TrimSpace(req.ResponseType) != "code" {
		return nil, nil, oauthInvalidRequest("response_type must be code")
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return nil, nil, oauthInvalidClient("client_id is required")
	}
	if strings.TrimSpace(req.RedirectURI) == "" {
		return nil, nil, oauthInvalidRequest("redirect_uri is required")
	}
	if strings.TrimSpace(req.Resource) == "" {
		return nil, nil, oauthInvalidRequest("resource is required")
	}
	if err := mcpoauth.ValidateResource(req.Resource, s.cfg.MCPAudience); err != nil {
		return nil, nil, oauthInvalidTarget(err.Error())
	}
	if err := mcpoauth.ValidatePKCEChallenge(req.CodeChallenge, req.CodeChallengeMethod); err != nil {
		return nil, nil, oauthInvalidRequest(err.Error())
	}

	client, err := s.fetchClientMetadata(ctx, req.ClientID)
	if err != nil {
		return nil, nil, err
	}
	if !matchesRegisteredRedirectURI(client.RedirectURIs, req.RedirectURI) {
		return nil, nil, oauthInvalidRequest("redirect_uri is not registered for this client")
	}

	scopes := mcpoauth.NormalizeScopes(req.Scope)
	if len(scopes) == 0 {
		scopes = []string{domain.MCPOAuthScopeTools}
	}
	if !slices.Contains(scopes, domain.MCPOAuthScopeTools) {
		scopes = append([]string{domain.MCPOAuthScopeTools}, scopes...)
	}
	if err := mcpoauth.ValidateRequestedScopes(scopes); err != nil {
		return nil, nil, oauthInvalidScope(err.Error())
	}

	return client, scopes, nil
}

func (s *MCPOAuthService) fetchClientMetadata(ctx context.Context, clientID string) (*domain.MCPOAuthClientMetadata, error) {
	normalized, err := mcpoauth.CanonicalURL(clientID)
	if err != nil {
		return nil, oauthInvalidClient("client_id must be an https URL")
	}
	parsed, _ := url.Parse(normalized)
	if parsed.Scheme != "https" || parsed.Path == "" {
		return nil, oauthInvalidClient("client_id must use https and include a path")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, oauthInvalidClient("client metadata request is invalid")
	}
	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, oauthInvalidClient("failed to fetch client metadata")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, oauthInvalidClient("client metadata endpoint returned an error")
	}

	var metadata domain.MCPOAuthClientMetadata
	if err := json.NewDecoder(res.Body).Decode(&metadata); err != nil {
		return nil, oauthInvalidClient("client metadata is not valid JSON")
	}
	if metadata.ClientID != normalized {
		return nil, oauthInvalidClient("client metadata client_id does not match document URL")
	}
	if strings.TrimSpace(metadata.ClientName) == "" {
		return nil, oauthInvalidClient("client metadata must include client_name")
	}
	if len(metadata.RedirectURIs) == 0 {
		return nil, oauthInvalidClient("client metadata must include redirect_uris")
	}
	for _, redirectURI := range metadata.RedirectURIs {
		if err := validateClientRedirectURI(redirectURI); err != nil {
			return nil, oauthInvalidClient(err.Error())
		}
	}
	if metadata.TokenEndpointAuthMethod != "" && metadata.TokenEndpointAuthMethod != "none" {
		return nil, oauthInvalidClient("only token_endpoint_auth_method=none is supported")
	}
	if len(metadata.ResponseTypes) > 0 && !slices.Contains(metadata.ResponseTypes, "code") {
		return nil, oauthInvalidClient("client must support response_type=code")
	}
	return &metadata, nil
}

func validateClientRedirectURI(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("redirect URI %q is invalid", raw)
	}
	if isLoopbackRedirectURI(parsed) {
		return nil
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("redirect URI %q must use https or localhost http", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("redirect URI %q must be absolute", raw)
	}
	return nil
}

func matchesRegisteredRedirectURI(registered []string, requested string) bool {
	requestedURL, err := url.Parse(strings.TrimSpace(requested))
	if err != nil {
		return false
	}

	for _, raw := range registered {
		if raw == requested {
			return true
		}

		registeredURL, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if !isLoopbackRedirectURI(registeredURL) || !isLoopbackRedirectURI(requestedURL) {
			continue
		}
		if !sameLoopbackRedirectTarget(registeredURL, requestedURL) {
			continue
		}
		return true
	}

	return false
}

func sameLoopbackRedirectTarget(registered, requested *url.URL) bool {
	return registered.Scheme == requested.Scheme &&
		registered.Hostname() == requested.Hostname() &&
		registered.EscapedPath() == requested.EscapedPath() &&
		registered.RawQuery == requested.RawQuery &&
		registered.Fragment == requested.Fragment
}

func isLoopbackRedirectURI(parsed *url.URL) bool {
	if parsed == nil || parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "localhost"
}

func oauthRedirectURL(raw string, params map[string]string) (string, error) {
	target, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query := target.Query()
	for key, value := range params {
		if strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(key, value)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func oauthInvalidRequest(description string) *domain.OAuthProtocolError {
	return &domain.OAuthProtocolError{StatusCode: http.StatusBadRequest, Code: "invalid_request", Description: description}
}

func oauthInvalidClient(description string) *domain.OAuthProtocolError {
	return &domain.OAuthProtocolError{StatusCode: http.StatusBadRequest, Code: "invalid_client", Description: description}
}

func oauthInvalidGrant(description string) *domain.OAuthProtocolError {
	return &domain.OAuthProtocolError{StatusCode: http.StatusBadRequest, Code: "invalid_grant", Description: description}
}

func oauthInvalidScope(description string) *domain.OAuthProtocolError {
	return &domain.OAuthProtocolError{StatusCode: http.StatusBadRequest, Code: "invalid_scope", Description: description}
}

func oauthUnsupportedGrant(description string) *domain.OAuthProtocolError {
	return &domain.OAuthProtocolError{StatusCode: http.StatusBadRequest, Code: "unsupported_grant_type", Description: description}
}

func oauthInvalidTarget(description string) *domain.OAuthProtocolError {
	return &domain.OAuthProtocolError{StatusCode: http.StatusBadRequest, Code: "invalid_target", Description: description}
}

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
