package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

const (
	sessionCookieName    = "teraslack_session"
	oauthNonceCookieName = "teraslack_oauth_nonce"
)

type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

func (h *AuthHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	provider := domain.AuthProvider(r.PathValue("provider"))
	result, err := h.svc.StartOAuth(r.Context(), domain.StartOAuthParams{
		Provider:    provider,
		WorkspaceID: r.URL.Query().Get("workspace_id"),
		InviteToken: r.URL.Query().Get("invite"),
		RedirectTo:  r.URL.Query().Get("redirect_to"),
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oauthNonceCookieName,
		Value:    result.Nonce,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, result.AuthorizationURL, http.StatusFound)
}

func (h *AuthHandler) CompleteOAuth(w http.ResponseWriter, r *http.Request) {
	nonceCookie, err := r.Cookie(oauthNonceCookieName)
	if err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	provider := domain.AuthProvider(r.PathValue("provider"))
	result, err := h.svc.CompleteOAuth(r.Context(), domain.CompleteOAuthParams{
		Provider: provider,
		Code:     r.URL.Query().Get("code"),
		State:    r.URL.Query().Get("state"),
		Nonce:    nonceCookie.Value,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	http.SetCookie(w, expiredCookie(oauthNonceCookieName))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    result.Session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  result.Session.ExpiresAt,
	})

	if result.RedirectTo != "" {
		http.Redirect(w, r, result.RedirectTo, http.StatusFound)
		return
	}

	httputil.WriteResource(w, http.StatusOK, result.Session)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	httputil.WriteResource(w, http.StatusOK, domain.AuthContext{
		WorkspaceID:   ctxutil.GetWorkspaceID(r.Context()),
		UserID:        ctxutil.GetUserID(r.Context()),
		PrincipalType: ctxutil.GetPrincipalType(r.Context()),
		AccountType:   ctxutil.GetAccountType(r.Context()),
		IsBot:         ctxutil.GetIsBot(r.Context()),
		Permissions:   ctxutil.GetPermissions(r.Context()),
		Scopes:        ctxutil.GetOAuthScopes(r.Context()),
	})
}

func (h *AuthHandler) RevokeCurrentSession(w http.ResponseWriter, r *http.Request) {
	token, isAPIKey := authCredentialFromRequest(r)
	if token == "" {
		httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	if isAPIKey {
		httputil.WriteErrorResponse(w, r, http.StatusBadRequest, "invalid_request", "API keys cannot be revoked from this endpoint.")
		return
	}

	if err := h.svc.RevokeSession(r.Context(), token); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	http.SetCookie(w, expiredCookie(sessionCookieName))
	httputil.WriteNoContent(w)
}

func (h *AuthHandler) SwitchCurrentSessionWorkspace(w http.ResponseWriter, r *http.Request) {
	token, isAPIKey := authCredentialFromRequest(r)
	if token == "" {
		httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	if isAPIKey {
		httputil.WriteErrorResponse(w, r, http.StatusBadRequest, "invalid_request", "API keys cannot switch browser workspaces.")
		return
	}

	var req struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	session, err := h.svc.SwitchWorkspace(r.Context(), token, req.WorkspaceID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	httputil.WriteNoContent(w)
}

var authBypassPaths = map[string]bool{
	"/healthz":      true,
	"/openapi.json": true,
	"/openapi.yaml": true,
}

func AuthMiddleware(authSvc *service.AuthService, apiKeySvc *service.APIKeyService, mcpOAuthSvc *service.MCPOAuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAuthBypassRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			token, isAPIKey := authCredentialFromRequest(r)
			if token == "" {
				httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
				return
			}

			ctx := r.Context()
			if isAPIKey {
				if apiKeySvc == nil {
					httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "invalid_authentication", "Authentication credentials are invalid.")
					return
				}

				validation, err := apiKeySvc.ValidateAPIKey(ctx, token)
				if err != nil {
					httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "invalid_authentication", "Authentication credentials are invalid.")
					return
				}

				ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, validation.WorkspaceID)
				ctx = context.WithValue(ctx, ctxutil.ContextKeyUserID, validation.UserID)
				ctx = ctxutil.WithPrincipal(ctx, validation.PrincipalType, validation.AccountType, validation.IsBot)
				ctx = ctxutil.WithDelegation(ctx, "", validation.KeyID)
				ctx = ctxutil.WithPermissions(ctx, validation.Permissions)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if mcpOAuthSvc != nil && strings.Count(token, ".") == 2 {
				auth, err := mcpOAuthSvc.ValidateAPIAccessToken(ctx, token)
				if err == nil {
					ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, auth.WorkspaceID)
					ctx = context.WithValue(ctx, ctxutil.ContextKeyUserID, auth.UserID)
					ctx = ctxutil.WithPrincipal(ctx, auth.PrincipalType, auth.AccountType, auth.IsBot)
					ctx = ctxutil.WithPermissions(ctx, auth.Permissions)
					ctx = ctxutil.WithOAuthScopes(ctx, auth.Scopes)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			if authSvc == nil {
				httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "invalid_authentication", "Authentication credentials are invalid.")
				return
			}

			auth, err := authSvc.ValidateSession(ctx, token)
			if err != nil {
				httputil.WriteErrorResponse(w, r, http.StatusUnauthorized, "invalid_authentication", "Authentication credentials are invalid.")
				return
			}

			ctx = context.WithValue(ctx, ctxutil.ContextKeyWorkspaceID, auth.WorkspaceID)
			ctx = context.WithValue(ctx, ctxutil.ContextKeyUserID, auth.UserID)
			ctx = ctxutil.WithPrincipal(ctx, auth.PrincipalType, auth.AccountType, auth.IsBot)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetWorkspaceID(ctx context.Context) string {
	return ctxutil.GetWorkspaceID(ctx)
}

func GetUserID(ctx context.Context) string {
	return ctxutil.GetUserID(ctx)
}

func isAuthBypassRequest(r *http.Request) bool {
	if authBypassPaths[r.URL.Path] {
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/auth/oauth/") ||
		strings.HasPrefix(r.URL.Path, "/oauth/") ||
		strings.HasPrefix(r.URL.Path, "/.well-known/oauth-authorization-server")
}

func authCredentialFromRequest(r *http.Request) (token string, isAPIKey bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		return token, strings.HasPrefix(token, "sk_") || strings.HasPrefix(token, "sk_live_") || strings.HasPrefix(token, "sk_test_")
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	return cookie.Value, false
}

func expiredCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	}
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
