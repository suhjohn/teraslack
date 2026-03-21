package domain

import (
	"time"
)

// EventSubscription represents a webhook subscription for events.
type EventSubscription struct {
	ID              string   `json:"id"`
	TeamID          string   `json:"team_id"`
	URL             string   `json:"url"`
	EventTypes      []string `json:"event_types"`
	Secret          string   `json:"secret,omitempty"`           // Plaintext secret — only for runtime use, never persisted in event_data
	EncryptedSecret string   `json:"encrypted_secret,omitempty"` // AES-256-GCM encrypted secret stored in DB
	Enabled         bool     `json:"enabled"`
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
	TeamID     string   `json:"team_id"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	Secret     string   `json:"secret"`
}

// UpdateEventSubscriptionParams holds the parameters for updating a subscription.
type UpdateEventSubscriptionParams struct {
	URL        *string  `json:"url,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

// ListEventSubscriptionsParams holds the parameters for listing subscriptions.
type ListEventSubscriptionsParams struct {
	TeamID string `json:"team_id"`
}

// Subscription event types — these must match the service event type constants
// in service_event.go so that GetMatchingSubscriptions can match them via
// exact equality ($2::TEXT = ANY(event_types)).
const (
	EventTypeMessagePosted       = "message.posted"
	EventTypeMessageUpdated      = "message.updated"
	EventTypeMessageDeleted      = "message.deleted"
	EventTypeReactionAdded       = "reaction.added"
	EventTypeReactionRemoved     = "reaction.removed"
	EventTypeMemberJoinedChannel = "conversation.member_joined"
	EventTypeMemberLeftChannel   = "conversation.member_left"
	EventTypeChannelCreated      = "conversation.created"
	EventTypeChannelArchive      = "conversation.archived"
	EventTypeChannelUnarchive    = "conversation.unarchived"
	EventTypeChannelUpdated      = "conversation.updated"
	EventTypeFileShared          = "file.shared"
	EventTypeFileCreated         = "file.created"
	EventTypePinAdded            = "pin.added"
	EventTypePinRemoved          = "pin.removed"
)
