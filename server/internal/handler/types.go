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
	TeamID       string    `json:"team_id"`
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
	UserIDs []string `json:"user_ids"`
}

type ConversationManagersResponse struct {
	ConversationID string   `json:"conversation_id"`
	UserIDs        []string `json:"user_ids"`
}

type ConversationManagersUpdateRequest struct {
	UserIDs []string `json:"user_ids"`
}

type ConversationPostingPolicyResponse struct {
	ConversationID        string                               `json:"conversation_id"`
	PolicyType            domain.ConversationPostingPolicyType `json:"policy_type"`
	AllowedAccountTypes   []domain.AccountType                 `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []domain.DelegatedRole               `json:"allowed_delegated_roles,omitempty"`
	AllowedUserIDs        []string                             `json:"allowed_user_ids,omitempty"`
	AllowedUsergroupIDs   []string                             `json:"allowed_usergroup_ids,omitempty"`
	UpdatedBy             string                               `json:"updated_by,omitempty"`
	UpdatedAt             *time.Time                           `json:"updated_at,omitempty"`
}

type ConversationPostingPolicyUpdateRequest struct {
	PolicyType            domain.ConversationPostingPolicyType `json:"policy_type"`
	AllowedAccountTypes   []domain.AccountType                 `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []domain.DelegatedRole               `json:"allowed_delegated_roles,omitempty"`
	AllowedUserIDs        []string                             `json:"allowed_user_ids,omitempty"`
	AllowedUsergroupIDs   []string                             `json:"allowed_usergroup_ids,omitempty"`
}

type MessageReactionRequest struct {
	Name   string `json:"name"`
	UserID string `json:"user_id,omitempty"`
}

type BookmarkUpdateRequest struct {
	Title     *string `json:"title,omitempty"`
	Link      *string `json:"link,omitempty"`
	Emoji     *string `json:"emoji,omitempty"`
	UpdatedBy string  `json:"updated_by"`
}

type PinCreateRequest struct {
	MessageTS string `json:"message_ts"`
	UserID    string `json:"user_id,omitempty"`
}

type ConversationReadUpdateRequest struct {
	LastReadTS string `json:"last_read_ts"`
}

type UsergroupUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Handle      *string `json:"handle,omitempty"`
	Description *string `json:"description,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	UpdatedBy   string  `json:"updated_by"`
}

type UsergroupMembersUpdateRequest struct {
	Users []string `json:"users"`
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
