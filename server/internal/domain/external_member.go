package domain

import "time"

// ExternalMember grants a canonical account access to a single host-workspace conversation.
type ExternalMember struct {
	ID                  string                      `json:"id"`
	ConversationID      string                      `json:"conversation_id"`
	HostWorkspaceID     string                      `json:"host_workspace_id"`
	ExternalWorkspaceID string                      `json:"external_workspace_id"`
	AccountID           string                      `json:"account_id"`
	AccessMode          ExternalPrincipalAccessMode `json:"access_mode"`
	AllowedCapabilities []string                    `json:"allowed_capabilities,omitempty"`
	InvitedBy           string                      `json:"invited_by"` // Compatibility user id of the inviter.
	CreatedAt           time.Time                   `json:"created_at"`
	ExpiresAt           *time.Time                  `json:"expires_at,omitempty"`
	RevokedAt           *time.Time                  `json:"revoked_at,omitempty"`
	Account             *Account                    `json:"account,omitempty"`
}

type CreateExternalMemberParams struct {
	ConversationID      string                      `json:"conversation_id"`
	ExternalWorkspaceID string                      `json:"external_workspace_id"`
	AccountID           string                      `json:"account_id,omitempty"`
	PrincipalType       PrincipalType               `json:"principal_type"`
	Email               string                      `json:"email,omitempty"`
	Name                string                      `json:"name,omitempty"`
	RealName            string                      `json:"real_name,omitempty"`
	DisplayName         string                      `json:"display_name,omitempty"`
	AccessMode          ExternalPrincipalAccessMode `json:"access_mode"`
	AllowedCapabilities []string                    `json:"allowed_capabilities,omitempty"`
	InvitedBy           string                      `json:"invited_by,omitempty"` // Compatibility user id of the inviter.
	ExpiresAt           *time.Time                  `json:"expires_at,omitempty"`
}

type UpdateExternalMemberParams struct {
	AccessMode          *ExternalPrincipalAccessMode `json:"access_mode,omitempty"`
	AllowedCapabilities *[]string                    `json:"allowed_capabilities,omitempty"`
	ExpiresAt           *time.Time                   `json:"expires_at,omitempty"`
}
