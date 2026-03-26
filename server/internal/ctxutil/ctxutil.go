// Package ctxutil provides shared context key utilities used across handler and service layers.
// This avoids circular imports between handler → service → handler.
package ctxutil

import (
	"context"

	"github.com/suhjohn/teraslack/internal/domain"
)

type contextKey string

const (
	ContextKeyWorkspaceID   contextKey = "workspace_id"
	ContextKeyUserID        contextKey = "user_id"
	ContextKeyIsBot         contextKey = "is_bot"
	ContextKeyPrincipalType contextKey = "principal_type"
	ContextKeyAccountType   contextKey = "account_type"
	ContextKeyOnBehalfOf    contextKey = "on_behalf_of"
	ContextKeyAPIKeyID      contextKey = "api_key_id"
	ContextKeyPermissions   contextKey = "permissions"
	ContextKeyOAuthScopes   contextKey = "oauth_scopes"
)

// GetUserID extracts user_id from context (set by AuthMiddleware).
func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserID).(string)
	return v
}

// GetActingUserID extracts the effective actor ID from context.
// For delegated API keys, this prefers the impersonated user over the key principal.
func GetActingUserID(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyOnBehalfOf).(string)
	if v != "" {
		return v
	}
	return GetUserID(ctx)
}

// GetWorkspaceID extracts workspace_id from context (set by AuthMiddleware).
func GetWorkspaceID(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyWorkspaceID).(string)
	return v
}

// GetIsBot extracts is_bot from context.
func GetIsBot(ctx context.Context) bool {
	v, _ := ctx.Value(ContextKeyIsBot).(bool)
	return v
}

// GetPrincipalType extracts principal_type from context.
func GetPrincipalType(ctx context.Context) domain.PrincipalType {
	v, _ := ctx.Value(ContextKeyPrincipalType).(domain.PrincipalType)
	return v
}

// GetAccountType extracts account_type from context.
func GetAccountType(ctx context.Context) domain.AccountType {
	v, _ := ctx.Value(ContextKeyAccountType).(domain.AccountType)
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

// GetPermissions extracts API key permissions from context.
func GetPermissions(ctx context.Context) []string {
	v, _ := ctx.Value(ContextKeyPermissions).([]string)
	if v == nil {
		return []string{}
	}
	return v
}

func GetOAuthScopes(ctx context.Context) []string {
	v, _ := ctx.Value(ContextKeyOAuthScopes).([]string)
	if v == nil {
		return []string{}
	}
	return v
}

// WithUser returns a context with user_id and workspace_id set.
func WithUser(ctx context.Context, userID, workspaceID string) context.Context {
	ctx = context.WithValue(ctx, ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, ContextKeyWorkspaceID, workspaceID)
	return ctx
}

// WithPrincipal returns a context with the authenticated principal metadata attached.
func WithPrincipal(ctx context.Context, principalType domain.PrincipalType, accountType domain.AccountType, isBot bool) context.Context {
	ctx = context.WithValue(ctx, ContextKeyPrincipalType, principalType)
	ctx = context.WithValue(ctx, ContextKeyAccountType, accountType)
	ctx = context.WithValue(ctx, ContextKeyIsBot, isBot)
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

// WithPermissions returns a context with API key permissions attached.
func WithPermissions(ctx context.Context, permissions []string) context.Context {
	if permissions == nil {
		permissions = []string{}
	}
	return context.WithValue(ctx, ContextKeyPermissions, permissions)
}

func WithOAuthScopes(ctx context.Context, scopes []string) context.Context {
	if scopes == nil {
		scopes = []string{}
	}
	return context.WithValue(ctx, ContextKeyOAuthScopes, scopes)
}
