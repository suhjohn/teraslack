package domain

import (
	"encoding/json"
	"time"
)

// InternalEvent represents a single immutable fact in the internal event log.
type InternalEvent struct {
	ID            int64           `json:"id"`
	EventType     string          `json:"event_type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	WorkspaceID        string          `json:"workspace_id"`
	ActorID       string          `json:"actor_id"`
	ShardKey      string          `json:"shard_key,omitempty"`
	ShardID       int             `json:"shard_id"`
	Payload       json.RawMessage `json:"payload"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

const InternalEventShardCount = 16

// Aggregate type constants.
const (
	AggregateWorkspace         = "workspace"
	AggregateUser         = "user"
	AggregateConversation = "conversation"
	AggregateMessage      = "message"
	AggregateUsergroup    = "usergroup"
	AggregatePin          = "pin"
	AggregateBookmark     = "bookmark"
	AggregateFile         = "file"
	AggregateAPIKey       = "api_key"
	AggregateSubscription = "event_subscription"
)

// Event type constants for event sourcing.
const (
	EventWorkspaceCreated = "workspace.created"
	EventWorkspaceUpdated = "workspace.updated"

	EventUserCreated      = "user.created"
	EventUserUpdated      = "user.updated"
	EventUserDeleted      = "user.deleted"
	EventUserRolesUpdated = "user.roles_updated"

	EventConversationCreated              = "conversation.created"
	EventConversationUpdated              = "conversation.updated"
	EventConversationArchived             = "conversation.archived"
	EventConversationUnarchived           = "conversation.unarchived"
	EventConversationTopicSet             = "conversation.topic_set"
	EventConversationPurposeSet           = "conversation.purpose_set"
	EventConversationManagerAdded         = "conversation.manager_added"
	EventConversationManagerRemoved       = "conversation.manager_removed"
	EventConversationPostingPolicyUpdated = "conversation.posting_policy.updated"
	EventMemberJoined                     = "conversation.member_joined"
	EventMemberLeft                       = "conversation.member_left"

	EventMessagePosted  = "message.posted"
	EventMessageUpdated = "message.updated"
	EventMessageDeleted = "message.deleted"

	EventReactionAdded   = "reaction.added"
	EventReactionRemoved = "reaction.removed"

	EventUsergroupCreated  = "usergroup.created"
	EventUsergroupUpdated  = "usergroup.updated"
	EventUsergroupEnabled  = "usergroup.enabled"
	EventUsergroupDisabled = "usergroup.disabled"
	EventUsergroupUserSet  = "usergroup.users_set"

	EventPinAdded   = "pin.added"
	EventPinRemoved = "pin.removed"

	EventBookmarkCreated = "bookmark.created"
	EventBookmarkUpdated = "bookmark.updated"
	EventBookmarkDeleted = "bookmark.deleted"

	EventFileCreated = "file.created"
	EventFileUpdated = "file.updated"
	EventFileDeleted = "file.deleted"
	EventFileShared  = "file.shared"

	EventAPIKeyCreated = "api_key.created"
	EventAPIKeyUpdated = "api_key.updated"
	EventAPIKeyRevoked = "api_key.revoked"
	EventAPIKeyRotated = "api_key.rotated"

	EventSubscriptionCreated = "event_subscription.created"
	EventSubscriptionUpdated = "event_subscription.updated"
	EventSubscriptionDeleted = "event_subscription.deleted"

	EventExternalPrincipalAccessGranted = "external_principal_access.granted"
	EventExternalPrincipalAccessUpdated = "external_principal_access.updated"
	EventExternalPrincipalAccessRevoked = "external_principal_access.revoked"
)
