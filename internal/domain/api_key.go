package domain

import "time"

// PrincipalType identifies what kind of entity a user/principal is.
type PrincipalType string

const (
	PrincipalTypeHuman  PrincipalType = "human"
	PrincipalTypeAgent  PrincipalType = "agent"
	PrincipalTypeSystem PrincipalType = "system"
)

// APIKeyType describes the intended use of an API key.
type APIKeyType string

const (
	APIKeyTypePersistent APIKeyType = "persistent" // Long-lived key
	APIKeyTypeSession    APIKeyType = "session"    // Short-lived, auto-expires
	APIKeyTypeRestricted APIKeyType = "restricted" // Scoped to specific resources
)

// APIKeyEnvironment distinguishes live vs test keys.
type APIKeyEnvironment string

const (
	APIKeyEnvLive APIKeyEnvironment = "live"
	APIKeyEnvTest APIKeyEnvironment = "test"
)

// APIKey represents a managed API key for authentication.
type APIKey struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	KeyHash      string            `json:"key_hash,omitempty"`    // SHA-256 hash of the raw key
	KeyPrefix    string            `json:"key_prefix"`            // e.g. "sk_live_" — stored for display
	KeyHint      string            `json:"key_hint"`              // Last 4 chars for identification
	TeamID       string            `json:"team_id"`
	PrincipalID  string            `json:"principal_id"`          // The principal this key acts as
	CreatedBy    string            `json:"created_by"`            // The principal who created this key
	OnBehalfOf   string            `json:"on_behalf_of,omitempty"` // Delegation — actions attributed to this principal
	Type         APIKeyType        `json:"type"`
	Environment  APIKeyEnvironment `json:"environment"`
	Permissions  []string          `json:"permissions"`
	ExpiresAt    *time.Time        `json:"expires_at,omitempty"`
	LastUsedAt   *time.Time        `json:"last_used_at,omitempty"`
	RequestCount int64             `json:"request_count"`
	Revoked      bool              `json:"revoked"`
	RevokedAt    *time.Time        `json:"revoked_at,omitempty"`
	// For key rotation: if this key was rotated, the old key remains valid
	// until grace_period_ends_at.
	RotatedToID       string     `json:"rotated_to_id,omitempty"`
	GracePeriodEndsAt *time.Time `json:"grace_period_ends_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Redacted returns a copy with sensitive fields cleared for event_data.
func (k *APIKey) Redacted() *APIKey {
	c := *k
	c.KeyHash = ""
	return &c
}

// CreateAPIKeyParams holds the parameters for creating a new API key.
type CreateAPIKeyParams struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	TeamID      string            `json:"team_id"`
	PrincipalID string            `json:"principal_id"`
	CreatedBy   string            `json:"created_by,omitempty"` // Defaults to principal_id if empty
	OnBehalfOf  string            `json:"on_behalf_of,omitempty"`
	Type        APIKeyType        `json:"type"`
	Environment APIKeyEnvironment `json:"environment"`
	Permissions []string          `json:"permissions,omitempty"`
	ExpiresIn   string            `json:"expires_in,omitempty"` // Duration string e.g. "1h", "30d"
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
	TeamID       string `json:"team_id"`
	PrincipalID  string `json:"principal_id,omitempty"`
	IncludeRevoked bool `json:"include_revoked,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// APIKeyValidation is the result of validating an API key.
type APIKeyValidation struct {
	TeamID      string   `json:"team_id"`
	PrincipalID string   `json:"principal_id"`
	OnBehalfOf  string   `json:"on_behalf_of,omitempty"`
	KeyID       string   `json:"key_id"`
	Permissions []string `json:"permissions"`
	Environment APIKeyEnvironment `json:"environment"`
}
