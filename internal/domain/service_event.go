package domain

import (
	"encoding/json"
	"time"
)

// ServiceEvent represents a single event in the service-level event store.
// This replaces the old EventLogEntry and Event types.
type ServiceEvent struct {
	ID            int64           `json:"id"`
	EventType     string          `json:"event_type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	TeamID        string          `json:"team_id"`
	ActorID       string          `json:"actor_id"`
	Payload       json.RawMessage `json:"payload"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Aggregate type constants.
const (
	AggregateUser         = "user"
	AggregateConversation = "conversation"
	AggregateMessage      = "message"
	AggregateUsergroup    = "usergroup"
	AggregatePin          = "pin"
	AggregateBookmark     = "bookmark"
	AggregateFile         = "file"
	AggregateToken        = "token"
	AggregateAPIKey       = "api_key"
	AggregateSubscription = "event_subscription"
)

// Event type constants for event sourcing.
const (
	EventUserCreated = "user.created"
	EventUserUpdated = "user.updated"
	EventUserDeleted = "user.deleted"

	EventConversationCreated    = "conversation.created"
	EventConversationUpdated    = "conversation.updated"
	EventConversationArchived   = "conversation.archived"
	EventConversationUnarchived = "conversation.unarchived"
	EventConversationTopicSet   = "conversation.topic_set"
	EventConversationPurposeSet = "conversation.purpose_set"
	EventMemberJoined           = "conversation.member_joined"
	EventMemberLeft             = "conversation.member_left"

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

	EventTokenCreated = "token.created"
	EventTokenRevoked = "token.revoked"

	EventAPIKeyCreated = "api_key.created"
	EventAPIKeyUpdated = "api_key.updated"
	EventAPIKeyRevoked = "api_key.revoked"
	EventAPIKeyRotated = "api_key.rotated"

	EventSubscriptionCreated = "subscription.created"
	EventSubscriptionUpdated = "subscription.updated"
	EventSubscriptionDeleted = "subscription.deleted"
)
