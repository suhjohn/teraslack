package domain

import (
	"encoding/json"
	"time"
)

// EventLogEntry represents a single entry in the append-only event log.
type EventLogEntry struct {
	SequenceID    int64           `json:"sequence_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	EventData     json.RawMessage `json:"event_data"`
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
	AggregateSubscription = "event_subscription"
)

// Event type constants for event sourcing.
const (
	EventUserCreated   = "user.created"
	EventUserUpdated   = "user.updated"
	EventUserDeleted   = "user.deleted"

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

	EventSubscriptionCreated = "subscription.created"
	EventSubscriptionUpdated = "subscription.updated"
	EventSubscriptionDeleted = "subscription.deleted"
)
