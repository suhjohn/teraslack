package domain

import "time"

// PrincipalType identifies what kind of entity a user/principal is.
type PrincipalType string

const (
	PrincipalTypeHuman  PrincipalType = "human"
	PrincipalTypeAgent  PrincipalType = "agent"
	PrincipalTypeSystem PrincipalType = "system"
)

// APIKey represents a managed API key for authentication.
type APIKey struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	KeyHash      string     `json:"key_hash,omitempty"` // SHA-256 hash of the raw key
	KeyPrefix    string     `json:"key_prefix"`         // e.g. "sk_" — stored for display
	KeyHint      string     `json:"key_hint"`           // Last 4 chars for identification
	WorkspaceID       string     `json:"workspace_id"`
	UserID       string     `json:"user_id"`
	CreatedBy    string     `json:"created_by"`
	Permissions  []string   `json:"permissions"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RequestCount int64      `json:"request_count"`
	Revoked      bool       `json:"revoked"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	// For key rotation: if this key was rotated, the old key remains valid
	// until grace_period_ends_at.
	RotatedToID       string     `json:"rotated_to_id,omitempty"`
	GracePeriodEndsAt *time.Time `json:"grace_period_ends_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Redacted returns a copy safe for event_data storage.
// KeyHash (a one-way SHA-256 hash) is preserved so projection rebuilds
// can restore the lookup hash after TRUNCATE + replay. This is safe
// because the hash is irreversible — it's the same pattern as storing
// bcrypt password hashes.
func (k *APIKey) Redacted() *APIKey {
	c := *k
	return &c
}

// CreateAPIKeyParams holds the parameters for creating a new API key.
type CreateAPIKeyParams struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	WorkspaceID      string   `json:"workspace_id"`
	UserID      string   `json:"user_id"`
	CreatedBy   string   `json:"created_by,omitempty"` // Defaults to user_id for user-scoped keys; required for system keys without an authenticated actor.
	Permissions []string `json:"permissions,omitempty"`
	ExpiresIn   string   `json:"expires_in,omitempty"` // Duration string e.g. "1h", "30d"
}

// UpdateAPIKeyParams holds the parameters for updating an API key.
type UpdateAPIKeyParams struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Permissions *[]string `json:"permissions,omitempty"`
}

// RotateAPIKeyParams holds the parameters for rotating an API key.
type RotateAPIKeyParams struct {
	GracePeriod string `json:"grace_period,omitempty"` // Duration string e.g. "24h", "7d". Default: "24h"
}

// ListAPIKeysParams holds pagination and filter options.
type ListAPIKeysParams struct {
	WorkspaceID         string `json:"workspace_id"`
	UserID         string `json:"user_id,omitempty"`
	IncludeRevoked bool   `json:"include_revoked,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

// APIKeyValidation is the result of validating an API key.
type APIKeyValidation struct {
	WorkspaceID        string        `json:"workspace_id"`
	UserID        string        `json:"user_id"`
	PrincipalType PrincipalType `json:"principal_type"`
	AccountType   AccountType   `json:"account_type,omitempty"`
	IsBot         bool          `json:"is_bot"`
	KeyID         string        `json:"key_id"`
	Permissions   []string      `json:"permissions"`
}
