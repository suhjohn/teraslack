package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/suhjohn/workspace/internal/ctxutil"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)


// AuthHandler handles HTTP requests for authentication operations.
type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// CreateToken handles POST /tokens
func (h *AuthHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateTokenParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	token, err := h.svc.CreateToken(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"token":   token.Token,
		"user_id": token.UserID,
		"team_id": token.TeamID,
		"is_bot":  token.IsBot,
	})
}

// Test handles GET /auth/test
func (h *AuthHandler) Test(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	resp, err := h.svc.ValidateToken(r.Context(), authHeader)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"team_id": resp.TeamID,
		"user_id": resp.UserID,
		"is_bot":  resp.IsBot,
	})
}

// Revoke handles DELETE /tokens
func (h *AuthHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.RevokeToken(r.Context(), req.Token); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// authBypassPaths are endpoints that bypass auth entirely (any method).
var authBypassPaths = map[string]bool{
	"/auth/test": true,
	"/healthz":   true,
}

// authBypassMethodPaths are method+path combos that bypass auth.
var authBypassMethodPaths = map[string]bool{
	"POST /tokens": true,
}

// AuthMiddleware validates Bearer token or API key and injects user context.
// Supports two auth mechanisms:
//   - Bearer token: "Authorization: Bearer xoxb-..." or "Authorization: Bearer xoxp-..."
//   - API key: "Authorization: Bearer sk_live_..." or "Authorization: Bearer sk_test_..."
func AuthMiddleware(authSvc *service.AuthService, apiKeySvc *service.APIKeyService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip middleware for endpoints that handle auth themselves
			if authBypassPaths[r.URL.Path] || authBypassMethodPaths[r.Method+" "+r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				httputil.WriteJSON(w, http.StatusUnauthorized, httputil.SlackResponse{
					OK:    false,
					Error: "not_authed",
				})
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			token = strings.TrimSpace(token)

			ctx := r.Context()

			// Route to API key validation if token starts with sk_live_ or sk_test_
			if strings.HasPrefix(token, "sk_live_") || strings.HasPrefix(token, "sk_test_") {
				if apiKeySvc == nil {
					httputil.WriteJSON(w, http.StatusUnauthorized, httputil.SlackResponse{
						OK:    false,
						Error: "api_keys_not_configured",
					})
					return
				}

				validation, err := apiKeySvc.ValidateAPIKey(ctx, token)
				if err != nil {
					httputil.WriteJSON(w, http.StatusUnauthorized, httputil.SlackResponse{
						OK:    false,
						Error: "invalid_auth",
					})
					return
				}

				ctx = context.WithValue(ctx, ctxutil.ContextKeyTeamID, validation.TeamID)
				ctx = context.WithValue(ctx, ctxutil.ContextKeyUserID, validation.PrincipalID)
				ctx = context.WithValue(ctx, ctxutil.ContextKeyIsBot, false)
				ctx = ctxutil.WithDelegation(ctx, validation.OnBehalfOf, validation.KeyID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Legacy Bearer token validation
			if authSvc == nil {
				httputil.WriteJSON(w, http.StatusUnauthorized, httputil.SlackResponse{
					OK:    false,
					Error: "token_auth_not_configured",
				})
				return
			}
			resp, err := authSvc.ValidateToken(ctx, token)
			if err != nil {
				httputil.WriteJSON(w, http.StatusUnauthorized, httputil.SlackResponse{
					OK:    false,
					Error: "invalid_auth",
				})
				return
			}

			ctx = context.WithValue(ctx, ctxutil.ContextKeyTeamID, resp.TeamID)
			ctx = context.WithValue(ctx, ctxutil.ContextKeyUserID, resp.UserID)
			ctx = context.WithValue(ctx, ctxutil.ContextKeyIsBot, resp.IsBot)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetTeamID extracts team_id from context (set by AuthMiddleware).
func GetTeamID(ctx context.Context) string {
	return ctxutil.GetTeamID(ctx)
}

// GetUserID extracts user_id from context (set by AuthMiddleware).
func GetUserID(ctx context.Context) string {
	return ctxutil.GetUserID(ctx)
}
