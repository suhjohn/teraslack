// Package ctxutil provides shared context key utilities used across handler and service layers.
// This avoids circular imports between handler → service → handler.
package ctxutil

import "context"

type contextKey string

const (
	ContextKeyTeamID     contextKey = "team_id"
	ContextKeyUserID     contextKey = "user_id"
	ContextKeyIsBot      contextKey = "is_bot"
	ContextKeyOnBehalfOf contextKey = "on_behalf_of"
	ContextKeyAPIKeyID   contextKey = "api_key_id"
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

// GetOnBehalfOf extracts on_behalf_of from context (set by AuthMiddleware for API keys with delegation).
func GetOnBehalfOf(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyOnBehalfOf).(string)
	return v
}

// GetAPIKeyID extracts the API key ID from context (set by AuthMiddleware when using API key auth).
func GetAPIKeyID(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyAPIKeyID).(string)
	return v
}

// WithUser returns a context with user_id and team_id set.
func WithUser(ctx context.Context, userID, teamID string) context.Context {
	ctx = context.WithValue(ctx, ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, ContextKeyTeamID, teamID)
	return ctx
}

// WithDelegation returns a context with delegation chain info set.
func WithDelegation(ctx context.Context, onBehalfOf, apiKeyID string) context.Context {
	if onBehalfOf != "" {
		ctx = context.WithValue(ctx, ContextKeyOnBehalfOf, onBehalfOf)
	}
	if apiKeyID != "" {
		ctx = context.WithValue(ctx, ContextKeyAPIKeyID, apiKeyID)
	}
	return ctx
}
