package domain

import "time"

type ConversationRead struct {
	TeamID         string    `json:"team_id"`
	ConversationID string    `json:"conversation_id"`
	UserID         string    `json:"user_id"`
	LastReadTS     string    `json:"last_read_ts"`
	LastReadAt     time.Time `json:"last_read_at"`
}

type MarkConversationReadParams struct {
	ConversationID string `json:"conversation_id"`
	UserID         string `json:"user_id"`
	LastReadTS     string `json:"last_read_ts"`
}
