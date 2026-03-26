package domain

import "time"

type ExternalPrincipalAccessMode string

const (
	ExternalPrincipalAccessModeShared         ExternalPrincipalAccessMode = "external_shared"
	ExternalPrincipalAccessModeSharedReadOnly ExternalPrincipalAccessMode = "external_shared_readonly"
)

func IsValidExternalPrincipalAccessMode(mode ExternalPrincipalAccessMode) bool {
	switch mode {
	case ExternalPrincipalAccessModeShared, ExternalPrincipalAccessModeSharedReadOnly:
		return true
	default:
		return false
	}
}

type ExternalPrincipalAccess struct {
	ID                  string                      `json:"id"`
	HostWorkspaceID          string                      `json:"host_workspace_id"`
	PrincipalID         string                      `json:"principal_id"`
	PrincipalType       PrincipalType               `json:"principal_type"`
	HomeWorkspaceID          string                      `json:"home_workspace_id"`
	AccessMode          ExternalPrincipalAccessMode `json:"access_mode"`
	AllowedCapabilities []string                    `json:"allowed_capabilities,omitempty"`
	ConversationIDs     []string                    `json:"conversation_ids,omitempty"`
	GrantedBy           string                      `json:"granted_by"`
	CreatedAt           time.Time                   `json:"created_at"`
	ExpiresAt           *time.Time                  `json:"expires_at,omitempty"`
	RevokedAt           *time.Time                  `json:"revoked_at,omitempty"`
}

type CreateExternalPrincipalAccessParams struct {
	HostWorkspaceID          string                      `json:"host_workspace_id"`
	PrincipalID         string                      `json:"principal_id"`
	PrincipalType       PrincipalType               `json:"principal_type"`
	HomeWorkspaceID          string                      `json:"home_workspace_id"`
	AccessMode          ExternalPrincipalAccessMode `json:"access_mode"`
	AllowedCapabilities []string                    `json:"allowed_capabilities,omitempty"`
	ConversationIDs     []string                    `json:"conversation_ids,omitempty"`
	GrantedBy           string                      `json:"granted_by,omitempty"`
	ExpiresAt           *time.Time                  `json:"expires_at,omitempty"`
}

type UpdateExternalPrincipalAccessParams struct {
	AccessMode          *ExternalPrincipalAccessMode `json:"access_mode,omitempty"`
	AllowedCapabilities *[]string                    `json:"allowed_capabilities,omitempty"`
	ConversationIDs     *[]string                    `json:"conversation_ids,omitempty"`
	ExpiresAt           *time.Time                   `json:"expires_at,omitempty"`
}

type ListExternalPrincipalAccessParams struct {
	HostWorkspaceID string `json:"host_workspace_id"`
}
