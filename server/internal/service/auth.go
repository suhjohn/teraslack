package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

const (
	authSessionTTL      = 30 * 24 * time.Hour
	emailCodeTTLDefault = 10 * time.Minute
)

type AuthEmailSender interface {
	SendVerificationCode(ctx context.Context, email, code string, expiresAt time.Time) error
}

type AuthConfig struct {
	BaseURL                 string
	FrontendURL             string
	StateSecret             string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	ResendAPIKey            string
	AuthEmailFrom           string
	ResendBaseURL           string
	EmailCodeTTL            time.Duration
	EmailSender             AuthEmailSender
	HTTPClient              *http.Client
}

type oauthProviderConfig struct {
	ClientID     string
	ClientSecret string
}

type oauthState struct {
	Provider    domain.AuthProvider `json:"provider"`
	WorkspaceID string              `json:"workspace_id"`
	InviteToken string              `json:"invite_token"`
	RedirectTo  string              `json:"redirect_to"`
	CallbackURL string              `json:"callback_url"`
	Nonce       string              `json:"nonce"`
}

type oauthProfile struct {
	Subject string
	Email   string
	Name    string
	Login   string
}

type AuthService struct {
	repo          repository.AuthRepository
	userRepo      repository.UserRepository
	accountRepo   repository.AccountRepository
	workspaceRepo repository.WorkspaceRepository
	inviteRepo    repository.WorkspaceInviteRepository
	auditRepo     repository.AuthorizationAuditRepository
	recorder      EventRecorder
	db            repository.TxBeginner
	logger        *slog.Logger
	httpClient    *http.Client
	baseURL       string
	frontendURL   string
	stateSecret   []byte
	github        oauthProviderConfig
	google        oauthProviderConfig
	emailSender   AuthEmailSender
	emailCodeTTL  time.Duration
}

func NewAuthService(repo repository.AuthRepository, userRepo repository.UserRepository, workspaceRepo repository.WorkspaceRepository, inviteRepo repository.WorkspaceInviteRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger, cfg AuthConfig) *AuthService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	emailCodeTTL := cfg.EmailCodeTTL
	if emailCodeTTL <= 0 {
		emailCodeTTL = emailCodeTTLDefault
	}
	emailSender := cfg.EmailSender
	if emailSender == nil && cfg.ResendAPIKey != "" && cfg.AuthEmailFrom != "" {
		emailSender = NewResendAuthEmailSender(httpClient, logger, ResendAuthEmailSenderConfig{
			APIKey:  cfg.ResendAPIKey,
			From:    cfg.AuthEmailFrom,
			BaseURL: cfg.ResendBaseURL,
		})
	}
	return &AuthService{
		repo:          repo,
		userRepo:      userRepo,
		accountRepo:   nil,
		workspaceRepo: workspaceRepo,
		inviteRepo:    inviteRepo,
		recorder:      recorder,
		db:            db,
		logger:        logger,
		httpClient:    httpClient,
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		frontendURL:   strings.TrimRight(cfg.FrontendURL, "/"),
		stateSecret:   []byte(cfg.StateSecret),
		github: oauthProviderConfig{
			ClientID:     cfg.GitHubOAuthClientID,
			ClientSecret: cfg.GitHubOAuthClientSecret,
		},
		google: oauthProviderConfig{
			ClientID:     cfg.GoogleOAuthClientID,
			ClientSecret: cfg.GoogleOAuthClientSecret,
		},
		emailSender:  emailSender,
		emailCodeTTL: emailCodeTTL,
	}
}

func (s *AuthService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *AuthService) SetIdentityRepositories(accountRepo repository.AccountRepository, _ ...any) {
	s.accountRepo = accountRepo
}

func (s *AuthService) GetCurrentIdentity(ctx context.Context) (*domain.Account, *domain.User, error) {
	var user *domain.User
	userID := strings.TrimSpace(ctxutil.GetUserID(ctx))
	switch {
	case userID != "":
		resolvedUser, err := s.userRepo.Get(ctx, userID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				break
			}
			return nil, nil, err
		}
		user = resolvedUser
	case s.accountRepo != nil:
		workspaceID := strings.TrimSpace(ctxutil.GetWorkspaceID(ctx))
		accountID := strings.TrimSpace(ctxutil.GetAccountID(ctx))
		if workspaceID != "" && accountID != "" {
			resolvedUser, err := s.userRepo.GetByWorkspaceAndAccount(ctx, workspaceID, accountID)
			if err != nil {
				if !errors.Is(err, domain.ErrNotFound) {
					return nil, nil, err
				}
			} else {
				user = resolvedUser
			}
		}
	}

	if s.accountRepo == nil {
		return nil, user, nil
	}

	accountID := strings.TrimSpace(ctxutil.GetAccountID(ctx))
	if accountID == "" && user != nil {
		accountID = strings.TrimSpace(user.AccountID)
	}
	if accountID == "" {
		return nil, user, nil
	}

	account, err := s.accountRepo.Get(ctx, accountID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, user, nil
		}
		return nil, nil, err
	}
	return account, applyAccountIdentityToUser(user, account), nil
}

func (s *AuthService) GetCurrentAccount(ctx context.Context) (*domain.Account, error) {
	account, _, err := s.GetCurrentIdentity(ctx)
	return account, err
}

func (s *AuthService) GetCurrentUser(ctx context.Context) (*domain.User, error) {
	_, user, err := s.GetCurrentIdentity(ctx)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) createSessionParamsForUser(workspaceID string, user *domain.User, provider domain.AuthProvider, expiresAt time.Time) (domain.CreateAuthSessionParams, error) {
	params := domain.CreateAuthSessionParams{
		WorkspaceID: workspaceID,
		Provider:    provider,
		ExpiresAt:   expiresAt,
	}
	if user == nil {
		return params, fmt.Errorf("user: %w", domain.ErrInvalidArgument)
	}
	params.UserID = user.ID
	if user.AccountID != "" {
		params.AccountID = user.AccountID
	}
	if user.WorkspaceID != "" {
		params.WorkspaceID = user.WorkspaceID
	}
	return params, nil
}

func (s *AuthService) oauthAccountParamsForUser(workspaceID string, user *domain.User, provider domain.AuthProvider, providerSubject, email string) (domain.UpsertOAuthAccountParams, error) {
	params := domain.UpsertOAuthAccountParams{
		WorkspaceID:     workspaceID,
		Provider:        provider,
		ProviderSubject: providerSubject,
		Email:           email,
	}
	if user == nil {
		return params, fmt.Errorf("user: %w", domain.ErrInvalidArgument)
	}
	params.UserID = user.ID
	if user.AccountID != "" {
		params.AccountID = user.AccountID
	}
	if user.WorkspaceID != "" {
		params.WorkspaceID = user.WorkspaceID
	}
	return params, nil
}

func (s *AuthService) Signup(ctx context.Context, params domain.SignupParams) (*domain.SignupResult, error) {
	email, err := normalizeEmailAddress(params.Email)
	if err != nil {
		return nil, err
	}
	if s.emailSender == nil {
		return nil, domain.ErrEmailAuthDisabled
	}

	code, err := randomNumericCode(6)
	if err != nil {
		return nil, fmt.Errorf("generate verification code: %w", err)
	}
	expiresAt := time.Now().UTC().Add(s.emailCodeTTL)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	authRepo := s.repo.WithTx(tx)
	if err := authRepo.DeletePendingEmailVerificationChallenges(ctx, email); err != nil {
		return nil, fmt.Errorf("delete pending email verification challenges: %w", err)
	}
	challenge, err := authRepo.CreateEmailVerificationChallenge(ctx, domain.CreateEmailVerificationChallengeParams{
		Email:     email,
		CodeHash:  crypto.HashToken(code),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("create email verification challenge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	if err := s.emailSender.SendVerificationCode(ctx, email, code, expiresAt); err != nil {
		return nil, fmt.Errorf("send verification email: %w", err)
	}

	return &domain.SignupResult{
		Email:     challenge.Email,
		ExpiresAt: challenge.ExpiresAt,
	}, nil
}

func (s *AuthService) Verify(ctx context.Context, params domain.VerifyParams) (*domain.AuthSession, error) {
	email, err := normalizeEmailAddress(params.Email)
	if err != nil {
		return nil, err
	}
	code, err := normalizeVerificationCode(params.Code)
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
	workspaceRepo := s.workspaceRepo
	if workspaceRepo != nil {
		workspaceRepo = workspaceRepo.WithTx(tx)
	}

	challenge, err := authRepo.GetEmailVerificationChallenge(ctx, email, crypto.HashToken(code))
	if err != nil {
		return nil, err
	}
	if challenge.ConsumedAt != nil || challenge.ExpiresAt.Before(time.Now().UTC()) {
		return nil, domain.ErrInvalidAuth
	}

	workspaceID, user, err := s.resolveEmailLogin(ctx, tx, userRepo, workspaceRepo, email, strings.TrimSpace(params.Name))
	if err != nil {
		return nil, fmt.Errorf("resolve email login: %w", err)
	}

	consumedAt := time.Now().UTC()
	if err := authRepo.ConsumeEmailVerificationChallenge(ctx, challenge.ID, consumedAt); err != nil {
		return nil, err
	}

	sessionParams, err := s.createSessionParamsForUser(workspaceID, user, domain.AuthProviderEmail, time.Now().UTC().Add(authSessionTTL))
	if err != nil {
		return nil, fmt.Errorf("build email auth session: %w", err)
	}
	session, err := authRepo.CreateSession(ctx, sessionParams)
	if err != nil {
		return nil, fmt.Errorf("create email auth session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return session, nil
}

func (s *AuthService) StartOAuth(ctx context.Context, params domain.StartOAuthParams) (*domain.StartOAuthResult, error) {
	return s.startOAuth(ctx, params, false)
}

func (s *AuthService) StartCLIOAuth(ctx context.Context, params domain.StartOAuthParams) (*domain.StartOAuthResult, error) {
	return s.startOAuth(ctx, params, true)
}

func (s *AuthService) startOAuth(ctx context.Context, params domain.StartOAuthParams, allowLocalhostCallback bool) (*domain.StartOAuthResult, error) {
	if err := validateOAuthProvider(params.Provider); err != nil {
		return nil, err
	}
	workspaceID, err := resolveOptionalWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	if len(s.stateSecret) == 0 {
		return nil, fmt.Errorf("auth state secret: %w", domain.ErrInvalidArgument)
	}
	redirectTo, err := s.resolveRedirectTo(params.RedirectTo)
	if err != nil {
		return nil, err
	}
	callbackURL, err := s.resolveCallbackURL(params.Provider, params.CallbackURL, allowLocalhostCallback)
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
		Provider:    params.Provider,
		WorkspaceID: params.WorkspaceID,
		InviteToken: strings.TrimSpace(params.InviteToken),
		RedirectTo:  redirectTo,
		CallbackURL: callbackURL,
		Nonce:       nonce,
	})
	if err != nil {
		return nil, err
	}

	return &domain.StartOAuthResult{
		AuthorizationURL: s.authorizationURL(params.Provider, cfg, state, callbackURL),
		Nonce:            nonce,
	}, nil
}

func (s *AuthService) CompleteOAuth(ctx context.Context, params domain.CompleteOAuthParams) (*domain.CompleteOAuthResult, error) {
	if err := validateOAuthProvider(params.Provider); err != nil {
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

	callbackURL := strings.TrimSpace(state.CallbackURL)
	if callbackURL == "" {
		callbackURL = s.defaultCallbackURL(params.Provider)
	}

	profile, err := s.exchangeCode(ctx, params.Provider, params.Code, callbackURL)
	if err != nil {
		return nil, fmt.Errorf("exchange oauth code: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	authRepo := s.repo.WithTx(tx)
	userRepo := s.userRepo.WithTx(tx)
	workspaceRepo := s.workspaceRepo
	if workspaceRepo != nil {
		workspaceRepo = workspaceRepo.WithTx(tx)
	}
	inviteRepo := s.inviteRepo
	if inviteRepo != nil {
		inviteRepo = inviteRepo.WithTx(tx)
	}

	workspaceID, user, err := s.resolveOAuthLogin(ctx, tx, userRepo, authRepo, workspaceRepo, inviteRepo, state, params.Provider, profile)
	if err != nil {
		return nil, fmt.Errorf("resolve oauth login: %w", err)
	}

	sessionParams, err := s.createSessionParamsForUser(workspaceID, user, params.Provider, time.Now().UTC().Add(authSessionTTL))
	if err != nil {
		return nil, fmt.Errorf("build oauth session: %w", err)
	}
	session, err := authRepo.CreateSession(ctx, sessionParams)
	if err != nil {
		return nil, fmt.Errorf("create oauth session: %w", err)
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

	var user *domain.User
	switch {
	case session.UserID != "":
		user, err = s.userRepo.Get(ctx, session.UserID)
	case session.AccountID != "" && session.WorkspaceID != "":
		user, err = s.userRepo.GetByWorkspaceAndAccount(ctx, session.WorkspaceID, session.AccountID)
	default:
		return nil, domain.ErrInvalidAuth
	}
	if err != nil {
		return nil, err
	}
	var account *domain.Account
	if s.accountRepo != nil && strings.TrimSpace(user.AccountID) != "" {
		account, err = s.accountRepo.Get(ctx, user.AccountID)
		if err != nil && err != domain.ErrNotFound {
			return nil, err
		}
		user = applyAccountIdentityToUser(user, account)
	}

	auth := &domain.AuthContext{
		WorkspaceID:   session.WorkspaceID,
		UserID:        user.ID,
		AccountID:     user.AccountID,
		PrincipalType: user.PrincipalType,
		AccountType:   user.EffectiveAccountType(),
		IsBot:         user.IsBot,
	}
	if user.WorkspaceID != "" {
		auth.WorkspaceID = user.WorkspaceID
	}

	return auth, nil
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
	auditPayload := map[string]any{}
	if session.AccountID != "" {
		auditPayload["account_id"] = session.AccountID
	}
	if session.UserID != "" {
		auditPayload["user_id"] = session.UserID
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, tx, session.WorkspaceID, domain.AuditActionSessionRevoked, "auth_session", session.ID, auditPayload); err != nil {
		return fmt.Errorf("record authorization audit log: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *AuthService) SwitchWorkspace(ctx context.Context, bearerToken, targetWorkspaceID string) (*domain.AuthSession, error) {
	token := strings.TrimSpace(strings.TrimPrefix(bearerToken, "Bearer "))
	targetWorkspaceID = strings.TrimSpace(targetWorkspaceID)
	if token == "" {
		return nil, fmt.Errorf("token: %w", domain.ErrInvalidArgument)
	}
	if targetWorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
	}

	currentSession, err := s.repo.GetSessionByHash(ctx, crypto.HashToken(token))
	if err != nil {
		return nil, err
	}
	if currentSession.RevokedAt != nil || currentSession.ExpiresAt.Before(time.Now().UTC()) {
		return nil, domain.ErrSessionRevoked
	}

	var currentUser *domain.User
	switch {
	case currentSession.UserID != "":
		currentUser, err = s.userRepo.Get(ctx, currentSession.UserID)
	case currentSession.AccountID != "" && currentSession.WorkspaceID != "":
		currentUser, err = s.userRepo.GetByWorkspaceAndAccount(ctx, currentSession.WorkspaceID, currentSession.AccountID)
	default:
		return nil, domain.ErrInvalidAuth
	}
	if err != nil {
		return nil, err
	}
	if currentUser.PrincipalType != domain.PrincipalTypeHuman || currentUser.Deleted || currentUser.Email == "" {
		return nil, domain.ErrForbidden
	}

	if currentUser.AccountID == "" {
		return nil, domain.ErrForbidden
	}
	targetUser, err := s.userRepo.GetByWorkspaceAndAccount(ctx, targetWorkspaceID, currentUser.AccountID)
	if err != nil {
		return nil, err
	}
	if targetUser.PrincipalType != domain.PrincipalTypeHuman || targetUser.Deleted {
		return nil, domain.ErrForbidden
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.RevokeSessionByHash(ctx, crypto.HashToken(token)); err != nil {
		return nil, err
	}

	sessionParams, err := s.createSessionParamsForUser(targetWorkspaceID, targetUser, currentSession.Provider, time.Now().UTC().Add(authSessionTTL))
	if err != nil {
		return nil, fmt.Errorf("build switched session: %w", err)
	}
	session, err := txRepo.CreateSession(ctx, sessionParams)
	if err != nil {
		return nil, fmt.Errorf("create switched session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return session, nil
}

func (s *AuthService) resolveOAuthUser(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	authRepo repository.AuthRepository,
	workspaceID string,
	provider domain.AuthProvider,
	profile oauthProfile,
) (*domain.User, error) {
	profile, err := normalizeOAuthProfile(profile)
	if err != nil {
		return nil, err
	}

	account, err := authRepo.GetOAuthAccount(ctx, workspaceID, provider, profile.Subject)
	if err != nil && err != domain.ErrNotFound {
		return nil, err
	}

	var user *domain.User
	if account != nil {
		switch {
		case account.UserID != "":
			user, err = userRepo.Get(ctx, account.UserID)
			if err != nil {
				return nil, err
			}
		case account.AccountID != "":
			user, err = userRepo.GetByWorkspaceAndAccount(ctx, workspaceID, account.AccountID)
			if err != nil {
				return nil, err
			}
		default:
			return nil, domain.ErrNotFound
		}
	} else {
		if s.accountRepo != nil {
			accountRepo := s.accountRepo
			if tx != nil {
				accountRepo = accountRepo.WithTx(tx)
			}
			account, accountErr := resolveOrCreateOAuthInviteAccount(ctx, accountRepo, profile)
			if accountErr != nil {
				return nil, accountErr
			}
			user, err = userRepo.GetByWorkspaceAndAccount(ctx, workspaceID, account.ID)
			if err == nil {
				// existing workspace-local user
			} else if errors.Is(err, domain.ErrNotFound) {
				user, err = s.createOAuthUser(ctx, tx, userRepo, workspaceID, profile, domain.AccountTypeMember)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			user, err = s.createOAuthUser(ctx, tx, userRepo, workspaceID, profile, domain.AccountTypeMember)
			if err != nil {
				return nil, err
			}
		}
	}

	if user.PrincipalType != domain.PrincipalTypeHuman {
		return nil, domain.ErrForbidden
	}

	oauthParams, err := s.oauthAccountParamsForUser(workspaceID, user, provider, profile.Subject, profile.Email)
	if err != nil {
		return nil, err
	}
	_, err = authRepo.UpsertOAuthAccount(ctx, oauthParams)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) resolveOAuthLogin(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	authRepo repository.AuthRepository,
	workspaceRepo repository.WorkspaceRepository,
	inviteRepo repository.WorkspaceInviteRepository,
	state oauthState,
	provider domain.AuthProvider,
	profile oauthProfile,
) (string, *domain.User, error) {
	profile, err := normalizeOAuthProfile(profile)
	if err != nil {
		return "", nil, err
	}

	if inviteToken := strings.TrimSpace(state.InviteToken); inviteToken != "" {
		return s.resolveInviteOAuthLogin(ctx, tx, userRepo, authRepo, inviteRepo, inviteToken, provider, profile)
	}
	if state.WorkspaceID != "" {
		user, err := s.resolveOAuthUser(ctx, tx, userRepo, authRepo, state.WorkspaceID, provider, profile)
		return state.WorkspaceID, user, err
	}

	accounts, err := authRepo.ListOAuthAccountsBySubject(ctx, provider, profile.Subject)
	if err != nil {
		return "", nil, err
	}
	if len(accounts) > 0 {
		workspaceID := accounts[0].WorkspaceID
		user, err := s.resolveOAuthUser(ctx, tx, userRepo, authRepo, workspaceID, provider, profile)
		return workspaceID, user, err
	}

	if workspaceID, user, err := s.resolveExistingAccountLogin(ctx, tx, userRepo, profile.Email); err != nil {
		return "", nil, err
	} else if user != nil {
		oauthParams, err := s.oauthAccountParamsForUser(workspaceID, user, provider, profile.Subject, profile.Email)
		if err != nil {
			return "", nil, err
		}
		_, err = authRepo.UpsertOAuthAccount(ctx, oauthParams)
		if err != nil {
			return "", nil, err
		}
		return workspaceID, user, nil
	}

	if workspaceRepo == nil {
		return "", nil, fmt.Errorf("workspace repo: %w", domain.ErrInvalidArgument)
	}
	workspace, user, err := s.createPersonalWorkspaceAndUser(ctx, tx, workspaceRepo, userRepo, profile)
	if err != nil {
		return "", nil, err
	}
	oauthParams, err := s.oauthAccountParamsForUser(workspace.ID, user, provider, profile.Subject, profile.Email)
	if err != nil {
		return "", nil, err
	}
	if _, err := authRepo.UpsertOAuthAccount(ctx, oauthParams); err != nil {
		return "", nil, err
	}
	return workspace.ID, user, nil
}

func (s *AuthService) resolveEmailLogin(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	workspaceRepo repository.WorkspaceRepository,
	email string,
	name string,
) (string, *domain.User, error) {
	if workspaceID, user, err := s.resolveExistingAccountLogin(ctx, tx, userRepo, email); err != nil {
		return "", nil, err
	} else if user != nil {
		return workspaceID, user, nil
	}

	if workspaceRepo == nil {
		return "", nil, fmt.Errorf("workspace repo: %w", domain.ErrInvalidArgument)
	}
	workspace, user, err := s.createPersonalWorkspaceAndUser(ctx, tx, workspaceRepo, userRepo, oauthProfile{
		Email: email,
		Login: emailLocalPart(email),
		Name:  strings.TrimSpace(name),
	})
	if err != nil {
		return "", nil, err
	}
	return workspace.ID, user, nil
}

func (s *AuthService) resolveExistingAccountLogin(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	email string,
) (string, *domain.User, error) {
	if s.accountRepo == nil {
		return "", nil, nil
	}

	accountRepo := s.accountRepo
	if tx != nil {
		accountRepo = accountRepo.WithTx(tx)
	}

	account, err := accountRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil, nil
		}
		return "", nil, err
	}
	if account.Deleted || account.PrincipalType != domain.PrincipalTypeHuman {
		return "", nil, nil
	}

	users, err := userRepo.ListByAccount(ctx, account.ID)
	if err != nil {
		return "", nil, err
	}
	for _, user := range users {
		if user.Deleted || user.PrincipalType != domain.PrincipalTypeHuman {
			continue
		}
		return user.WorkspaceID, &user, nil
	}
	return "", nil, nil
}

func (s *AuthService) resolveInviteOAuthLogin(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	authRepo repository.AuthRepository,
	inviteRepo repository.WorkspaceInviteRepository,
	inviteToken string,
	provider domain.AuthProvider,
	profile oauthProfile,
) (string, *domain.User, error) {
	if inviteRepo == nil {
		return "", nil, fmt.Errorf("invite repo: %w", domain.ErrInvalidArgument)
	}
	invite, err := inviteRepo.GetByTokenHash(ctx, crypto.HashToken(inviteToken))
	if err != nil {
		return "", nil, err
	}
	if invite.AcceptedAt != nil || invite.ExpiresAt.Before(time.Now().UTC()) {
		return "", nil, domain.ErrForbidden
	}
	if !strings.EqualFold(strings.TrimSpace(invite.Email), strings.TrimSpace(profile.Email)) {
		return "", nil, domain.ErrForbidden
	}

	if s.accountRepo != nil {
		accountRepo := s.accountRepo
		if tx != nil {
			accountRepo = accountRepo.WithTx(tx)
		}
		account, err := resolveOrCreateOAuthInviteAccount(ctx, accountRepo, profile)
		if err != nil {
			return "", nil, err
		}
		user, err := userRepo.GetByWorkspaceAndAccount(ctx, invite.WorkspaceID, account.ID)
		if err == nil {
			if user.Deleted || user.PrincipalType != domain.PrincipalTypeHuman {
				return "", nil, domain.ErrForbidden
			}
			oauthParams, err := s.oauthAccountParamsForUser(invite.WorkspaceID, user, provider, profile.Subject, profile.Email)
			if err != nil {
				return "", nil, err
			}
			if _, err := authRepo.UpsertOAuthAccount(ctx, oauthParams); err != nil {
				return "", nil, err
			}
			if err := inviteRepo.MarkAccepted(ctx, invite.ID, account.ID, time.Now().UTC()); err != nil {
				return "", nil, err
			}
			return invite.WorkspaceID, user, nil
		}
		if err != domain.ErrNotFound {
			return "", nil, err
		}
	}

	user, err := s.resolveOAuthUser(ctx, tx, userRepo, authRepo, invite.WorkspaceID, provider, profile)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(user.AccountID) == "" {
		return "", nil, fmt.Errorf("account_id: %w", domain.ErrInvalidAuth)
	}
	if err := inviteRepo.MarkAccepted(ctx, invite.ID, user.AccountID, time.Now().UTC()); err != nil {
		return "", nil, err
	}
	return invite.WorkspaceID, user, nil
}

func resolveOrCreateOAuthInviteAccount(ctx context.Context, accountRepo repository.AccountRepository, profile oauthProfile) (*domain.Account, error) {
	if accountRepo == nil {
		return nil, fmt.Errorf("account: %w", domain.ErrInvalidArgument)
	}
	account, err := accountRepo.GetByEmail(ctx, profile.Email)
	if err == nil {
		return account, nil
	}
	if err != nil && err != domain.ErrNotFound {
		return nil, err
	}
	name := strings.TrimSpace(profile.Login)
	if name == "" {
		name = emailLocalPart(profile.Email)
	}
	realName := strings.TrimSpace(profile.Name)
	if realName == "" {
		realName = name
	}
	return accountRepo.Create(ctx, domain.CreateAccountParams{
		PrincipalType: domain.PrincipalTypeHuman,
		Email:         profile.Email,
	})
}

func (s *AuthService) createOAuthUser(ctx context.Context, tx pgx.Tx, userRepo repository.UserRepository, workspaceID string, profile oauthProfile, accountType domain.AccountType) (*domain.User, error) {
	name := strings.TrimSpace(profile.Login)
	if name == "" {
		name = emailLocalPart(profile.Email)
	}
	realName := strings.TrimSpace(profile.Name)
	if realName == "" {
		realName = name
	}
	var accountID string
	if s.accountRepo != nil {
		accountRepo := s.accountRepo
		if tx != nil {
			accountRepo = accountRepo.WithTx(tx)
		}
		account, err := resolveOrCreateOAuthInviteAccount(ctx, accountRepo, profile)
		if err != nil {
			return nil, err
		}
		accountID = account.ID
	}

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		AccountID:     accountID,
		WorkspaceID:   workspaceID,
		Name:          name,
		RealName:      realName,
		DisplayName:   realName,
		Email:         profile.Email,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   accountType,
		IsBot:         false,
		Profile:       domain.UserProfile{},
	})
	if err != nil {
		return nil, err
	}
	if s.accountRepo != nil {
		accountRepo := s.accountRepo
		if tx != nil {
			accountRepo = accountRepo.WithTx(tx)
		}
		account, err := ensureAccountForUser(ctx, accountRepo, user)
		if err != nil {
			return nil, fmt.Errorf("sync identity for oauth user: %w", err)
		}
		if account != nil && user.AccountID == "" {
			user.AccountID = account.ID
		}
	}

	payload, _ := json.Marshal(user)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		WorkspaceID:   user.WorkspaceID,
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	return user, nil
}

func (s *AuthService) createPersonalWorkspaceAndUser(
	ctx context.Context,
	tx pgx.Tx,
	workspaceRepo repository.WorkspaceRepository,
	userRepo repository.UserRepository,
	profile oauthProfile,
) (*domain.Workspace, *domain.User, error) {
	name := personalWorkspaceName(profile)
	workspace, err := workspaceRepo.Create(ctx, domain.CreateWorkspaceParams{
		Name:            name,
		Domain:          personalWorkspaceDomain(profile),
		Discoverability: domain.WorkspaceDiscoverabilityInviteOnly,
	})
	if err != nil {
		return nil, nil, err
	}

	payload, _ := json.Marshal(workspace)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventWorkspaceCreated,
		AggregateType: domain.AggregateWorkspace,
		AggregateID:   workspace.ID,
		WorkspaceID:   workspace.ID,
		ActorID:       "",
		Payload:       payload,
	}); err != nil {
		return nil, nil, fmt.Errorf("record workspace.created event: %w", err)
	}

	user, err := s.createOAuthUser(ctx, tx, userRepo, workspace.ID, profile, domain.AccountTypePrimaryAdmin)
	if err != nil {
		return nil, nil, err
	}
	return workspace, user, nil
}

func (s *AuthService) exchangeCode(ctx context.Context, provider domain.AuthProvider, code, redirectURI string) (oauthProfile, error) {
	switch provider {
	case domain.AuthProviderGitHub:
		return s.exchangeGitHubCode(ctx, code, redirectURI)
	case domain.AuthProviderGoogle:
		return s.exchangeGoogleCode(ctx, code, redirectURI)
	default:
		return oauthProfile{}, fmt.Errorf("provider: %w", domain.ErrInvalidArgument)
	}
}

func (s *AuthService) exchangeGitHubCode(ctx context.Context, code, redirectURI string) (oauthProfile, error) {
	cfg, err := s.providerConfig(domain.AuthProviderGitHub)
	if err != nil {
		return oauthProfile{}, err
	}

	tokenReq := url.Values{}
	tokenReq.Set("client_id", cfg.ClientID)
	tokenReq.Set("client_secret", cfg.ClientSecret)
	tokenReq.Set("code", code)
	tokenReq.Set("redirect_uri", redirectURI)

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

func (s *AuthService) exchangeGoogleCode(ctx context.Context, code, redirectURI string) (oauthProfile, error) {
	cfg, err := s.providerConfig(domain.AuthProviderGoogle)
	if err != nil {
		return oauthProfile{}, err
	}

	tokenReq := url.Values{}
	tokenReq.Set("client_id", cfg.ClientID)
	tokenReq.Set("client_secret", cfg.ClientSecret)
	tokenReq.Set("code", code)
	tokenReq.Set("grant_type", "authorization_code")
	tokenReq.Set("redirect_uri", redirectURI)

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
		return fmt.Errorf("perform oauth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		trimmedBody := strings.TrimSpace(string(body))

		var oauthErr struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oauthErr)

		if s.logger != nil {
			attrs := []any{
				"method", req.Method,
				"url", req.URL.String(),
				"status", resp.StatusCode,
			}
			if oauthErr.Error != "" {
				attrs = append(attrs, "oauth_error", oauthErr.Error)
			}
			if oauthErr.ErrorDescription != "" {
				attrs = append(attrs, "oauth_error_description", oauthErr.ErrorDescription)
			} else if trimmedBody != "" {
				attrs = append(attrs, "body", trimmedBody)
			}
			s.logger.Warn("oauth request failed", attrs...)
		}

		switch oauthErr.Error {
		case "invalid_grant", "invalid_client", "unauthorized_client", "access_denied", "invalid_request":
			detail := oauthErr.Error
			if oauthErr.ErrorDescription != "" {
				detail += ": " + oauthErr.ErrorDescription
			}
			return fmt.Errorf("oauth request failed (%s): %w", detail, domain.ErrInvalidAuth)
		}

		if trimmedBody == "" {
			trimmedBody = resp.Status
		}
		return fmt.Errorf("oauth request failed: %s", trimmedBody)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode oauth response: %w", err)
	}
	return nil
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

func (s *AuthService) authorizationURL(provider domain.AuthProvider, cfg oauthProviderConfig, state, callbackURL string) string {
	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("redirect_uri", callbackURL)
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

func (s *AuthService) defaultCallbackURL(provider domain.AuthProvider) string {
	return s.baseURL + "/auth/oauth/" + string(provider) + "/callback"
}

func (s *AuthService) resolveCallbackURL(provider domain.AuthProvider, raw string, allowLocalhost bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return s.defaultCallbackURL(provider), nil
	}

	callbackURL, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("callback_url: %w", domain.ErrInvalidArgument)
	}
	if callbackURL.Fragment != "" || callbackURL.Host == "" {
		return "", fmt.Errorf("callback_url: %w", domain.ErrInvalidArgument)
	}

	if allowLocalhost && isLoopbackCallbackURL(callbackURL) {
		return callbackURL.String(), nil
	}

	allowedOrigins, err := s.allowedRedirectOrigins()
	if err != nil {
		return "", err
	}
	for _, origin := range allowedOrigins {
		if callbackURL.Scheme == origin.Scheme && callbackURL.Host == origin.Host {
			return callbackURL.String(), nil
		}
	}

	return "", fmt.Errorf("callback_url: %w", domain.ErrInvalidArgument)
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

func isLoopbackCallbackURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	if u.Fragment != "" || strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := strings.TrimSpace(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"
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

func validateOAuthProvider(provider domain.AuthProvider) error {
	switch provider {
	case domain.AuthProviderGitHub, domain.AuthProviderGoogle:
		return nil
	default:
		return fmt.Errorf("provider: %w", domain.ErrInvalidArgument)
	}
}

func normalizeEmailAddress(raw string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}
	email := strings.ToLower(strings.TrimSpace(address.Address))
	if email == "" {
		return "", fmt.Errorf("email: %w", domain.ErrInvalidArgument)
	}
	return email, nil
}

func normalizeOAuthProfile(profile oauthProfile) (oauthProfile, error) {
	email, err := normalizeEmailAddress(profile.Email)
	if err != nil {
		return oauthProfile{}, err
	}

	return oauthProfile{
		Subject: strings.TrimSpace(profile.Subject),
		Email:   email,
		Name:    strings.TrimSpace(profile.Name),
		Login:   strings.TrimSpace(profile.Login),
	}, nil
}

func normalizeVerificationCode(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	if len(code) != 6 {
		return "", fmt.Errorf("code: %w", domain.ErrInvalidArgument)
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return "", fmt.Errorf("code: %w", domain.ErrInvalidArgument)
		}
	}
	return code, nil
}

func resolveOptionalWorkspaceID(ctx context.Context, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if ctxWorkspace := ctxutil.GetWorkspaceID(ctx); ctxWorkspace != "" {
		if requested != "" && requested != ctxWorkspace {
			return "", domain.ErrForbidden
		}
		return ctxWorkspace, nil
	}
	return requested, nil
}

func emailLocalPart(email string) string {
	local, _, found := strings.Cut(email, "@")
	if found && local != "" {
		return local
	}
	return email
}

func personalWorkspaceName(profile oauthProfile) string {
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = strings.TrimSpace(profile.Login)
	}
	if name == "" {
		name = emailLocalPart(profile.Email)
	}
	return name + "'s workspace"
}

func personalWorkspaceDomain(profile oauthProfile) string {
	base := strings.TrimSpace(profile.Login)
	if base == "" {
		base = emailLocalPart(profile.Email)
	}
	base = strings.ToLower(base)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "personal"
	}
	return base
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length: %w", domain.ErrInvalidArgument)
	}
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", length, n.Int64()), nil
}
