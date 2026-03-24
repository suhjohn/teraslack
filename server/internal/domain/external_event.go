package domain

import (
	"encoding/json"
	"time"
)

const (
	ResourceTypeTeam         = "team"
	ResourceTypeUser         = "user"
	ResourceTypeConversation = "conversation"
	ResourceTypeFile         = "file"
	ResourceTypeUsergroup    = "usergroup"
)

// ExternalEvent is the canonical public event envelope exposed by /events and webhooks.
type ExternalEvent struct {
	ID                     int64           `json:"id"`
	TeamID                 string          `json:"team_id"`
	Type                   string          `json:"type"`
	ResourceType           string          `json:"resource_type"`
	ResourceID             string          `json:"resource_id"`
	OccurredAt             time.Time       `json:"occurred_at"`
	Payload                json.RawMessage `json:"payload"`
	SourceInternalEventID  *int64          `json:"source_internal_event_id,omitempty"`
	SourceInternalEventIDs []int64         `json:"source_internal_event_ids,omitempty"`
	DedupeKey              string          `json:"dedupe_key,omitempty"`
	CreatedAt              time.Time       `json:"created_at,omitempty"`
}

type ListExternalEventsParams struct {
	AfterID      int64  `json:"after_id,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Type         string `json:"type,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
}

type ExternalEventProjectionFailure struct {
	ID              int64     `json:"id"`
	InternalEventID int64     `json:"internal_event_id"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"created_at"`
}
