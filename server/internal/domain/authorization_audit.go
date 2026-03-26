package domain

import (
	"encoding/json"
	"time"
)

type AuthorizationAuditLog struct {
	ID         string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	ActorID    string          `json:"actor_id,omitempty"`
	APIKeyID   string          `json:"api_key_id,omitempty"`
	OnBehalfOf string          `json:"on_behalf_of,omitempty"`
	Action     string          `json:"action"`
	Resource   string          `json:"resource"`
	ResourceID string          `json:"resource_id"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type CreateAuthorizationAuditLogParams struct {
	WorkspaceID     string
	ActorID    string
	APIKeyID   string
	OnBehalfOf string
	Action     string
	Resource   string
	ResourceID string
	Metadata   json.RawMessage
}

type ListAuthorizationAuditLogsParams struct {
	WorkspaceID string
	Limit  int
}

const (
	AuditActionAccountTypeUpdated             = "account_type.updated"
	AuditActionPrimaryAdminTransferred        = "primary_admin.transferred"
	AuditActionDelegatedRolesUpdated          = "delegated_roles.updated"
	AuditActionExternalPrincipalAccessGranted = "external_principal_access.granted"
	AuditActionExternalPrincipalAccessUpdated = "external_principal_access.updated"
	AuditActionExternalPrincipalAccessRevoked = "external_principal_access.revoked"
	AuditActionConversationManagersUpdated    = "conversation_managers.updated"
	AuditActionPostingPolicyUpdated           = "conversation_posting_policy.updated"
	AuditActionSessionRevoked                 = "session.revoked"
	AuditActionAPIKeyCreated                  = "api_key.created"
	AuditActionAPIKeyUpdated                  = "api_key.updated"
	AuditActionAPIKeyRevoked                  = "api_key.revoked"
	AuditActionAPIKeyRotated                  = "api_key.rotated"
)
