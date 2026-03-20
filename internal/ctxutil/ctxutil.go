// Package ctxutil provides shared context key utilities used across handler and service layers.
// This avoids circular imports between handler → service → handler.
package ctxutil

import "context"

type contextKey string

const (
	ContextKeyTeamID contextKey = "team_id"
	ContextKeyUserID contextKey = "user_id"
	ContextKeyIsBot  contextKey = "is_bot"
)

// GetUserID extracts user_id from context (set by AuthMiddleware).
func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserID).(string)
	return v
}

// GetTeamID extracts team_id from context (set by AuthMiddleware).
func GetTeamID(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyTeamID).(string)
	return v
}

// WithUser returns a context with user_id and team_id set.
func WithUser(ctx context.Context, userID, teamID string) context.Context {
	ctx = context.WithValue(ctx, ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, ContextKeyTeamID, teamID)
	return ctx
}
