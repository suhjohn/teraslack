package handler

import (
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
)

type HealthStatusResponse struct {
	Status string `json:"status"`
}

type APIKeySecretResponse struct {
	APIKey *domain.APIKey `json:"api_key"`
	Secret string         `json:"secret"`
}

type EventSubscriptionResponse struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	URL          string    `json:"url"`
	Type         string    `json:"type,omitempty"`
	ResourceType string    `json:"resource_type,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ConversationUpdateRequest struct {
	Name       *string `json:"name,omitempty"`
	IsArchived *bool   `json:"is_archived,omitempty"`
	Topic      *string `json:"topic,omitempty"`
	Purpose    *string `json:"purpose,omitempty"`
}

type ConversationInviteRequest struct {
	AccountIDs []string `json:"account_ids,omitempty"`
}

type ConversationManagersResponse struct {
	ConversationID string   `json:"conversation_id"`
	AccountIDs     []string `json:"account_ids,omitempty"`
}

type ConversationManagersUpdateRequest struct {
	AccountIDs []string `json:"account_ids,omitempty"`
}

type ConversationPostingPolicyResponse struct {
	ConversationID        string                               `json:"conversation_id"`
	PolicyType            domain.ConversationPostingPolicyType `json:"policy_type"`
	AllowedAccountTypes   []domain.AccountType                 `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []domain.DelegatedRole               `json:"allowed_delegated_roles,omitempty"`
	AllowedAccountIDs     []string                             `json:"allowed_account_ids,omitempty"`
	UpdatedBy             string                               `json:"updated_by,omitempty"`
	UpdatedAt             *time.Time                           `json:"updated_at,omitempty"`
}

type ConversationPostingPolicyUpdateRequest struct {
	PolicyType            domain.ConversationPostingPolicyType `json:"policy_type"`
	AllowedAccountTypes   []domain.AccountType                 `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []domain.DelegatedRole               `json:"allowed_delegated_roles,omitempty"`
	AllowedAccountIDs     []string                             `json:"allowed_account_ids,omitempty"`
}

type MessageReactionRequest struct {
	Name   string `json:"name"`
	UserID string `json:"user_id,omitempty"`
}

type ConversationReadUpdateRequest struct {
	LastReadTS string `json:"last_read_ts"`
}

type EventSubscriptionUpdateRequest struct {
	URL          *string `json:"url,omitempty"`
	Type         *string `json:"type,omitempty"`
	ResourceType *string `json:"resource_type,omitempty"`
	ResourceID   *string `json:"resource_id,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

type UserRolesResponse struct {
	UserID         string                 `json:"user_id"`
	DelegatedRoles []domain.DelegatedRole `json:"delegated_roles"`
}

type UserRolesUpdateRequest struct {
	DelegatedRoles []domain.DelegatedRole `json:"delegated_roles"`
}
