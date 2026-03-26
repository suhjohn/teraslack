package domain

import (
	"time"
)

// EventSubscription represents a webhook subscription for events.
type EventSubscription struct {
	ID              string    `json:"id"`
	WorkspaceID          string    `json:"workspace_id"`
	URL             string    `json:"url"`
	Type            string    `json:"type,omitempty"`
	ResourceType    string    `json:"resource_type,omitempty"`
	ResourceID      string    `json:"resource_id,omitempty"`
	Secret          string    `json:"secret,omitempty"`           // Plaintext secret — only for runtime use, never persisted in event_data
	EncryptedSecret string    `json:"encrypted_secret,omitempty"` // AES-256-GCM encrypted secret stored in DB
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Redacted returns a copy with the plaintext secret cleared
// for safe serialization into event_data. EncryptedSecret is preserved
// so the projector can restore it during replay/rebuild.
func (s *EventSubscription) Redacted() *EventSubscription {
	copy := *s
	copy.Secret = "" // never store plaintext secret in event log
	// EncryptedSecret is kept — it's already AES-256-GCM ciphertext,
	// safe for the event log and required for projection rebuilds.
	return &copy
}

// CreateEventSubscriptionParams holds the parameters for creating a subscription.
type CreateEventSubscriptionParams struct {
	WorkspaceID       string `json:"workspace_id"`
	URL          string `json:"url"`
	Type         string `json:"type,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Secret       string `json:"secret"`
}

// UpdateEventSubscriptionParams holds the parameters for updating a subscription.
type UpdateEventSubscriptionParams struct {
	URL          *string `json:"url,omitempty"`
	Type         *string `json:"type,omitempty"`
	ResourceType *string `json:"resource_type,omitempty"`
	ResourceID   *string `json:"resource_id,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

// ListEventSubscriptionsParams holds the parameters for listing subscriptions.
type ListEventSubscriptionsParams struct {
	WorkspaceID string `json:"workspace_id"`
}

// Public event types supported by webhook filters.
const (
	EventTypeWorkspaceCreated                      = "workspace.created"
	EventTypeWorkspaceUpdated                      = "workspace.updated"
	EventTypeUserCreated                      = "user.created"
	EventTypeUserUpdated                      = "user.updated"
	EventTypeUserDeleted                      = "user.deleted"
	EventTypeUserRolesUpdated                 = "user.roles.updated"
	EventTypeConversationCreated              = "conversation.created"
	EventTypeConversationUpdated              = "conversation.updated"
	EventTypeConversationArchived             = "conversation.archived"
	EventTypeConversationUnarchived           = "conversation.unarchived"
	EventTypeConversationManagerAdded         = "conversation.manager.added"
	EventTypeConversationManagerRemoved       = "conversation.manager.removed"
	EventTypeConversationPostingPolicyUpdated = "conversation.posting_policy.updated"
	EventTypeConversationMemberAdded          = "conversation.member.added"
	EventTypeConversationMemberRemoved        = "conversation.member.removed"
	EventTypeConversationMessageCreated       = "conversation.message.created"
	EventTypeConversationMessageUpdated       = "conversation.message.updated"
	EventTypeConversationMessageDeleted       = "conversation.message.deleted"
	EventTypeConversationReactionAdded        = "conversation.message.reaction.added"
	EventTypeConversationReactionRemoved      = "conversation.message.reaction.removed"
	EventTypeConversationPinAdded             = "conversation.pin.added"
	EventTypeConversationPinRemoved           = "conversation.pin.removed"
	EventTypeConversationBookmarkCreated      = "conversation.bookmark.created"
	EventTypeConversationBookmarkUpdated      = "conversation.bookmark.updated"
	EventTypeConversationBookmarkDeleted      = "conversation.bookmark.deleted"
	EventTypeUsergroupCreated                 = "usergroup.created"
	EventTypeUsergroupUpdated                 = "usergroup.updated"
	EventTypeUsergroupEnabled                 = "usergroup.enabled"
	EventTypeUsergroupDisabled                = "usergroup.disabled"
	EventTypeUsergroupMembersUpdated          = "usergroup.members.updated"
	EventTypeFileCreated                      = "file.created"
	EventTypeFileUpdated                      = "file.updated"
	EventTypeFileDeleted                      = "file.deleted"
	EventTypeFileShared                       = "file.shared"
	EventTypeEventSubscriptionCreated         = "event_subscription.created"
	EventTypeEventSubscriptionUpdated         = "event_subscription.updated"
	EventTypeEventSubscriptionDeleted         = "event_subscription.deleted"
	EventTypeExternalPrincipalAccessGranted   = "external_principal_access.granted"
	EventTypeExternalPrincipalAccessUpdated   = "external_principal_access.updated"
	EventTypeExternalPrincipalAccessRevoked   = "external_principal_access.revoked"
)

func IsSupportedSubscriptionEventType(eventType string) bool {
	switch eventType {
	case "":
		return true
	case EventTypeWorkspaceCreated:
		return true
	case EventTypeWorkspaceUpdated:
		return true
	case EventTypeUserCreated:
		return true
	case EventTypeUserUpdated:
		return true
	case EventTypeUserDeleted:
		return true
	case EventTypeUserRolesUpdated:
		return true
	case EventTypeConversationCreated:
		return true
	case EventTypeConversationUpdated:
		return true
	case EventTypeConversationArchived:
		return true
	case EventTypeConversationUnarchived:
		return true
	case EventTypeConversationManagerAdded:
		return true
	case EventTypeConversationManagerRemoved:
		return true
	case EventTypeConversationPostingPolicyUpdated:
		return true
	case EventTypeConversationMemberAdded:
		return true
	case EventTypeConversationMemberRemoved:
		return true
	case EventTypeConversationMessageCreated:
		return true
	case EventTypeConversationMessageUpdated:
		return true
	case EventTypeConversationMessageDeleted:
		return true
	case EventTypeConversationReactionAdded:
		return true
	case EventTypeConversationReactionRemoved:
		return true
	case EventTypeConversationPinAdded:
		return true
	case EventTypeConversationPinRemoved:
		return true
	case EventTypeConversationBookmarkCreated:
		return true
	case EventTypeConversationBookmarkUpdated:
		return true
	case EventTypeConversationBookmarkDeleted:
		return true
	case EventTypeUsergroupCreated:
		return true
	case EventTypeUsergroupUpdated:
		return true
	case EventTypeUsergroupEnabled:
		return true
	case EventTypeUsergroupDisabled:
		return true
	case EventTypeUsergroupMembersUpdated:
		return true
	case EventTypeFileCreated:
		return true
	case EventTypeFileUpdated:
		return true
	case EventTypeFileDeleted:
		return true
	case EventTypeFileShared:
		return true
	case EventTypeEventSubscriptionCreated:
		return true
	case EventTypeEventSubscriptionUpdated:
		return true
	case EventTypeEventSubscriptionDeleted:
		return true
	case EventTypeExternalPrincipalAccessGranted:
		return true
	case EventTypeExternalPrincipalAccessUpdated:
		return true
	case EventTypeExternalPrincipalAccessRevoked:
		return true
	default:
		return false
	}
}
