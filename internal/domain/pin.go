package domain

import "time"

// Pin represents a pinned message in a conversation.
type Pin struct {
	ChannelID string    `json:"channel_id"`
	MessageTS string    `json:"message_ts"`
	PinnedBy  string    `json:"pinned_by"`
	PinnedAt  time.Time `json:"pinned_at"`
}

// PinParams holds the parameters for pinning/unpinning a message.
type PinParams struct {
	ChannelID string `json:"channel_id"`
	MessageTS string `json:"message_ts"`
	UserID    string `json:"user_id"`
}

// ListPinsParams holds the parameters for listing pins.
type ListPinsParams struct {
	ChannelID string `json:"channel_id"`
}
