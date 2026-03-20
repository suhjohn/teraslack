package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

type contextKey string

const (
	contextKeyTeamID contextKey = "team_id"
	contextKeyUserID contextKey = "user_id"
	contextKeyIsBot  contextKey = "is_bot"
)

// AuthHandler handles HTTP requests for authentication operations.
type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// CreateToken handles POST /api/auth.createToken
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

// Test handles POST /api/auth.test
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

// Revoke handles POST /api/auth.revoke
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

// AuthMiddleware validates Bearer token and injects user context.
func AuthMiddleware(authSvc *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				// Allow unauthenticated access for backwards compatibility
				next.ServeHTTP(w, r)
				return
			}

			resp, err := authSvc.ValidateToken(r.Context(), authHeader)
			if err != nil {
				httputil.WriteJSON(w, http.StatusUnauthorized, httputil.SlackResponse{
					OK:    false,
					Error: "invalid_auth",
				})
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, contextKeyTeamID, resp.TeamID)
			ctx = context.WithValue(ctx, contextKeyUserID, resp.UserID)
			ctx = context.WithValue(ctx, contextKeyIsBot, resp.IsBot)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetTeamID extracts team_id from context (set by AuthMiddleware).
func GetTeamID(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyTeamID).(string)
	return v
}

// GetUserID extracts user_id from context (set by AuthMiddleware).
func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyUserID).(string)
	return v
}
