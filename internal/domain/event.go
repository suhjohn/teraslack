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

// Redacted returns a copy with sensitive fields cleared
// for safe serialization into event_data.
func (s *EventSubscription) Redacted() *EventSubscription {
	copy := *s
	copy.Secret = ""          // never store plaintext secret in event log
	copy.EncryptedSecret = "" // don't leak encrypted form either
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

// Common event types.
const (
	EventTypeMessage             = "message"
	EventTypeReactionAdded       = "reaction_added"
	EventTypeReactionRemoved     = "reaction_removed"
	EventTypeMemberJoinedChannel = "member_joined_channel"
	EventTypeMemberLeftChannel   = "member_left_channel"
	EventTypeChannelCreated      = "channel_created"
	EventTypeChannelArchive      = "channel_archive"
	EventTypeChannelUnarchive    = "channel_unarchive"
	EventTypeChannelRename       = "channel_rename"
	EventTypeFileShared          = "file_shared"
	EventTypePinAdded            = "pin_added"
	EventTypePinRemoved          = "pin_removed"
)
