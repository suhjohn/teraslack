package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const authSessionTTL = 30 * 24 * time.Hour

type AuthConfig struct {
	BaseURL                 string
	FrontendURL             string
	StateSecret             string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	HTTPClient              *http.Client
}

type oauthProviderConfig struct {
	ClientID     string
	ClientSecret string
}

type oauthState struct {
	Provider   domain.AuthProvider `json:"provider"`
	TeamID     string              `json:"team_id"`
	RedirectTo string              `json:"redirect_to"`
	Nonce      string              `json:"nonce"`
}

type oauthProfile struct {
	Subject string
	Email   string
	Name    string
	Login   string
}

type AuthService struct {
	repo        repository.AuthRepository
	userRepo    repository.UserRepository
	auditRepo   repository.AuthorizationAuditRepository
	recorder    EventRecorder
	db          repository.TxBeginner
	logger      *slog.Logger
	httpClient  *http.Client
	baseURL     string
	frontendURL string
	stateSecret []byte
	github      oauthProviderConfig
	google      oauthProviderConfig
}

func NewAuthService(repo repository.AuthRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger, cfg AuthConfig) *AuthService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &AuthService{
		repo:        repo,
		userRepo:    userRepo,
		recorder:    recorder,
		db:          db,
		logger:      logger,
		httpClient:  httpClient,
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		frontendURL: strings.TrimRight(cfg.FrontendURL, "/"),
		stateSecret: []byte(cfg.StateSecret),
		github: oauthProviderConfig{
			ClientID:     cfg.GitHubOAuthClientID,
			ClientSecret: cfg.GitHubOAuthClientSecret,
		},
		google: oauthProviderConfig{
			ClientID:     cfg.GoogleOAuthClientID,
			ClientSecret: cfg.GoogleOAuthClientSecret,
		},
	}
}

func (s *AuthService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *AuthService) StartOAuth(ctx context.Context, params domain.StartOAuthParams) (*domain.StartOAuthResult, error) {
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	if err := validateAuthProvider(params.Provider); err != nil {
		return nil, err
	}
	if len(s.stateSecret) == 0 {
		return nil, fmt.Errorf("auth state secret: %w", domain.ErrInvalidArgument)
	}
	redirectTo, err := s.resolveRedirectTo(params.RedirectTo)
	if err != nil {
		return nil, err
	}

	cfg, err := s.providerConfig(params.Provider)
	if err != nil {
		return nil, err
	}

	nonce, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	state, err := s.encodeState(oauthState{
		Provider:   params.Provider,
		TeamID:     params.TeamID,
		RedirectTo: redirectTo,
		Nonce:      nonce,
	})
	if err != nil {
		return nil, err
	}

	return &domain.StartOAuthResult{
		AuthorizationURL: s.authorizationURL(params.Provider, cfg, state),
		Nonce:            nonce,
	}, nil
}

func (s *AuthService) CompleteOAuth(ctx context.Context, params domain.CompleteOAuthParams) (*domain.CompleteOAuthResult, error) {
	if err := validateAuthProvider(params.Provider); err != nil {
		return nil, err
	}
	if params.Code == "" {
		return nil, fmt.Errorf("code: %w", domain.ErrInvalidArgument)
	}
	if params.State == "" {
		return nil, fmt.Errorf("state: %w", domain.ErrInvalidArgument)
	}
	if params.Nonce == "" {
		return nil, fmt.Errorf("nonce: %w", domain.ErrInvalidArgument)
	}

	state, err := s.decodeState(params.State)
	if err != nil {
		return nil, err
	}
	if state.Provider != params.Provider {
		return nil, fmt.Errorf("provider: %w", domain.ErrInvalidArgument)
	}
	if state.Nonce != params.Nonce {
		return nil, fmt.Errorf("nonce: %w", domain.ErrInvalidArgument)
	}

	profile, err := s.exchangeCode(ctx, params.Provider, params.Code)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	authRepo := s.repo.WithTx(tx)
	userRepo := s.userRepo.WithTx(tx)

	user, err := s.resolveOAuthUser(ctx, tx, userRepo, authRepo, state.TeamID, params.Provider, profile)
	if err != nil {
		return nil, err
	}

	session, err := authRepo.CreateSession(ctx, domain.CreateAuthSessionParams{
		TeamID:    state.TeamID,
		UserID:    user.ID,
		Provider:  params.Provider,
		ExpiresAt: time.Now().UTC().Add(authSessionTTL),
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &domain.CompleteOAuthResult{
		Session:    session,
		RedirectTo: state.RedirectTo,
	}, nil
}

func (s *AuthService) ValidateSession(ctx context.Context, bearerToken string) (*domain.AuthContext, error) {
	token := strings.TrimSpace(strings.TrimPrefix(bearerToken, "Bearer "))
	if token == "" {
		return nil, fmt.Errorf("token: %w", domain.ErrInvalidArgument)
	}

	session, err := s.repo.GetSessionByHash(ctx, crypto.HashToken(token))
	if err != nil {
		return nil, err
	}
	if session.RevokedAt != nil || session.ExpiresAt.Before(time.Now().UTC()) {
		return nil, domain.ErrSessionRevoked
	}

	user, err := s.userRepo.Get(ctx, session.UserID)
	if err != nil {
		return nil, err
	}

	return &domain.AuthContext{
		TeamID:        session.TeamID,
		UserID:        session.UserID,
		PrincipalType: user.PrincipalType,
		AccountType:   user.EffectiveAccountType(),
		IsBot:         user.IsBot,
	}, nil
}

func (s *AuthService) RevokeSession(ctx context.Context, bearerToken string) error {
	token := strings.TrimSpace(strings.TrimPrefix(bearerToken, "Bearer "))
	if token == "" {
		return fmt.Errorf("token: %w", domain.ErrInvalidArgument)
	}
	sessionHash := crypto.HashToken(token)
	session, err := s.repo.GetSessionByHash(ctx, sessionHash)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).RevokeSessionByHash(ctx, sessionHash); err != nil {
		return err
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, session.TeamID, domain.AuditActionSessionRevoked, "auth_session", session.ID, map[string]any{
		"user_id": session.UserID,
	}); err != nil {
		return fmt.Errorf("record authorization audit log: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *AuthService) resolveOAuthUser(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	authRepo repository.AuthRepository,
	teamID string,
	provider domain.AuthProvider,
	profile oauthProfile,
) (*domain.User, error) {
	account, err := authRepo.GetOAuthAccount(ctx, teamID, provider, profile.Subject)
	if err != nil && err != domain.ErrNotFound {
		return nil, err
	}

	var user *domain.User
	if account != nil {
		user, err = userRepo.Get(ctx, account.UserID)
		if err != nil {
			return nil, err
		}
	} else {
		user, err = userRepo.GetByTeamEmail(ctx, teamID, profile.Email)
		if err == domain.ErrNotFound {
			user, err = s.createOAuthUser(ctx, tx, userRepo, teamID, profile)
		}
		if err != nil {
			return nil, err
		}
	}

	if user.PrincipalType != domain.PrincipalTypeHuman {
		return nil, domain.ErrForbidden
	}

	_, err = authRepo.UpsertOAuthAccount(ctx, domain.UpsertOAuthAccountParams{
		TeamID:          teamID,
		UserID:          user.ID,
		Provider:        provider,
		ProviderSubject: profile.Subject,
		Email:           profile.Email,
	})
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) createOAuthUser(ctx context.Context, tx pgx.Tx, userRepo repository.UserRepository, teamID string, profile oauthProfile) (*domain.User, error) {
	name := strings.TrimSpace(profile.Login)
	if name == "" {
		name = emailLocalPart(profile.Email)
	}
	realName := strings.TrimSpace(profile.Name)
	if realName == "" {
		realName = name
	}

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		TeamID:        teamID,
		Name:          name,
		RealName:      realName,
		DisplayName:   realName,
		Email:         profile.Email,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
		IsBot:         false,
		Profile:       domain.UserProfile{},
	})
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		TeamID:        user.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	return user, nil
}

func (s *AuthService) exchangeCode(ctx context.Context, provider domain.AuthProvider, code string) (oauthProfile, error) {
	switch provider {
	case domain.AuthProviderGitHub:
		return s.exchangeGitHubCode(ctx, code)
	case domain.AuthProviderGoogle:
		return s.exchangeGoogleCode(ctx, code)
	default:
		return oauthProfile{}, fmt.Errorf("provider: %w", domain.ErrInvalidArgument)
	}
}

func (s *AuthService) exchangeGitHubCode(ctx context.Context, code string) (oauthProfile, error) {
	cfg, err := s.providerConfig(domain.AuthProviderGitHub)
	if err != nil {
		return oauthProfile{}, err
	}

	tokenReq := url.Values{}
	tokenReq.Set("client_id", cfg.ClientID)
	tokenReq.Set("client_secret", cfg.ClientSecret)
	tokenReq.Set("code", code)
	tokenReq.Set("redirect_uri", s.callbackURL(domain.AuthProviderGitHub))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(tokenReq.Encode()))
	if err != nil {
		return oauthProfile{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := s.doJSON(req, &tokenResp); err != nil {
		return oauthProfile{}, err
	}
	if tokenResp.AccessToken == "" {
		return oauthProfile{}, domain.ErrInvalidAuth
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return oauthProfile{}, err
	}
	userReq.Header.Set("Accept", "application/json")
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	var userResp struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := s.doJSON(userReq, &userResp); err != nil {
		return oauthProfile{}, err
	}

	email := strings.TrimSpace(userResp.Email)
	if email == "" {
		emailReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
		if err != nil {
			return oauthProfile{}, err
		}
		emailReq.Header.Set("Accept", "application/json")
		emailReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := s.doJSON(emailReq, &emails); err != nil {
			return oauthProfile{}, err
		}
		for _, candidate := range emails {
			if candidate.Primary && candidate.Verified {
				email = candidate.Email
				break
			}
		}
	}
	if email == "" {
		return oauthProfile{}, domain.ErrInvalidAuth
	}

	return oauthProfile{
		Subject: fmt.Sprintf("%d", userResp.ID),
		Email:   email,
		Name:    strings.TrimSpace(userResp.Name),
		Login:   strings.TrimSpace(userResp.Login),
	}, nil
}

func (s *AuthService) exchangeGoogleCode(ctx context.Context, code string) (oauthProfile, error) {
	cfg, err := s.providerConfig(domain.AuthProviderGoogle)
	if err != nil {
		return oauthProfile{}, err
	}

	tokenReq := url.Values{}
	tokenReq.Set("client_id", cfg.ClientID)
	tokenReq.Set("client_secret", cfg.ClientSecret)
	tokenReq.Set("code", code)
	tokenReq.Set("grant_type", "authorization_code")
	tokenReq.Set("redirect_uri", s.callbackURL(domain.AuthProviderGoogle))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(tokenReq.Encode()))
	if err != nil {
		return oauthProfile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := s.doJSON(req, &tokenResp); err != nil {
		return oauthProfile{}, err
	}
	if tokenResp.AccessToken == "" {
		return oauthProfile{}, domain.ErrInvalidAuth
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return oauthProfile{}, err
	}
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	var userResp struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := s.doJSON(userReq, &userResp); err != nil {
		return oauthProfile{}, err
	}
	if userResp.Sub == "" || userResp.Email == "" || !userResp.EmailVerified {
		return oauthProfile{}, domain.ErrInvalidAuth
	}

	return oauthProfile{
		Subject: userResp.Sub,
		Email:   userResp.Email,
		Name:    strings.TrimSpace(userResp.Name),
		Login:   emailLocalPart(userResp.Email),
	}, nil
}

func (s *AuthService) doJSON(req *http.Request, target any) error {
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("oauth request failed: %s", strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *AuthService) providerConfig(provider domain.AuthProvider) (oauthProviderConfig, error) {
	switch provider {
	case domain.AuthProviderGitHub:
		if s.github.ClientID == "" || s.github.ClientSecret == "" {
			return oauthProviderConfig{}, domain.ErrInvalidArgument
		}
		return s.github, nil
	case domain.AuthProviderGoogle:
		if s.google.ClientID == "" || s.google.ClientSecret == "" {
			return oauthProviderConfig{}, domain.ErrInvalidArgument
		}
		return s.google, nil
	default:
		return oauthProviderConfig{}, fmt.Errorf("provider: %w", domain.ErrInvalidArgument)
	}
}

func (s *AuthService) authorizationURL(provider domain.AuthProvider, cfg oauthProviderConfig, state string) string {
	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("redirect_uri", s.callbackURL(provider))
	values.Set("state", state)

	switch provider {
	case domain.AuthProviderGitHub:
		values.Set("scope", "user:email")
		return "https://github.com/login/oauth/authorize?" + values.Encode()
	case domain.AuthProviderGoogle:
		values.Set("response_type", "code")
		values.Set("scope", "openid email profile")
		values.Set("prompt", "select_account")
		return "https://accounts.google.com/o/oauth2/v2/auth?" + values.Encode()
	default:
		panic("unknown provider")
	}
}

func (s *AuthService) callbackURL(provider domain.AuthProvider) string {
	return s.baseURL + "/auth/oauth/" + string(provider) + "/callback"
}

func (s *AuthService) resolveRedirectTo(raw string) (string, error) {
	if raw == "" {
		return s.baseURL, nil
	}

	redirectURL, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("redirect_to: %w", domain.ErrInvalidArgument)
	}

	allowedOrigins, err := s.allowedRedirectOrigins()
	if err != nil {
		return "", err
	}
	for _, origin := range allowedOrigins {
		if redirectURL.Scheme == origin.Scheme && redirectURL.Host == origin.Host {
			return redirectURL.String(), nil
		}
	}

	return "", fmt.Errorf("redirect_to: %w", domain.ErrInvalidArgument)
}

func (s *AuthService) allowedRedirectOrigins() ([]*url.URL, error) {
	rawOrigins := []string{s.baseURL}
	if s.frontendURL != "" {
		rawOrigins = append(rawOrigins, s.frontendURL)
	}

	origins := make([]*url.URL, 0, len(rawOrigins))
	for _, raw := range rawOrigins {
		parsed, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		origins = append(origins, parsed)
	}
	return origins, nil
}

func (s *AuthService) encodeState(state oauthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.stateSecret)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *AuthService) decodeState(raw string) (oauthState, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return oauthState{}, fmt.Errorf("state: %w", domain.ErrInvalidArgument)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oauthState{}, fmt.Errorf("state: %w", domain.ErrInvalidArgument)
	}
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return oauthState{}, fmt.Errorf("state: %w", domain.ErrInvalidArgument)
	}

	mac := hmac.New(sha256.New, s.stateSecret)
	mac.Write(payload)
	if !hmac.Equal(gotSig, mac.Sum(nil)) {
		return oauthState{}, fmt.Errorf("state: %w", domain.ErrInvalidArgument)
	}

	var state oauthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return oauthState{}, fmt.Errorf("state: %w", domain.ErrInvalidArgument)
	}
	return state, nil
}

func validateAuthProvider(provider domain.AuthProvider) error {
	switch provider {
	case domain.AuthProviderGitHub, domain.AuthProviderGoogle:
		return nil
	default:
		return fmt.Errorf("provider: %w", domain.ErrInvalidArgument)
	}
}

func emailLocalPart(email string) string {
	local, _, found := strings.Cut(email, "@")
	if found && local != "" {
		return local
	}
	return email
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
