package domain

import (
	"encoding/json"
	"time"
)

// Event represents a Slack-style event.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	TeamID    string          `json:"team_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// EventSubscription represents a webhook subscription for events.
type EventSubscription struct {
	ID         string   `json:"id"`
	TeamID     string   `json:"team_id"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	Secret     string   `json:"secret"`
	Enabled    bool     `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
