package domain

import (
	"encoding/json"
	"time"
)

// Message represents a chat message within a conversation.
type Message struct {
	TS                          string          `json:"ts"`
	ChannelID                   string          `json:"channel_id"`
	UserID                      string          `json:"user_id,omitempty"`
	AuthorAccountID             string          `json:"author_account_id,omitempty"`
	AuthorWorkspaceMembershipID string          `json:"author_workspace_membership_id,omitempty"`
	Text                        string          `json:"text"`
	ThreadTS                    *string         `json:"thread_ts,omitempty"`
	Type                        string          `json:"type"`
	Subtype                     *string         `json:"subtype,omitempty"`
	Blocks                      json.RawMessage `json:"blocks,omitempty"`
	Metadata                    json.RawMessage `json:"metadata,omitempty"`
	EditedBy                    *string         `json:"edited_by,omitempty"`
	EditedAt                    *string         `json:"edited_at,omitempty"`
	Reactions                   []Reaction      `json:"reactions,omitempty"`
	ReplyCount                  int             `json:"reply_count"`
	ReplyUsersCount             int             `json:"reply_users_count"`
	LatestReply                 *string         `json:"latest_reply,omitempty"`
	IsDeleted                   bool            `json:"is_deleted"`
	CreatedAt                   time.Time       `json:"created_at"`
	UpdatedAt                   time.Time       `json:"updated_at"`
}

// Reaction represents an emoji reaction on a message.
type Reaction struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
	Count int      `json:"count"`
}

// PostMessageParams holds the parameters for posting a new message.
type PostMessageParams struct {
	ChannelID       string          `json:"channel_id"`
	UserID          string          `json:"user_id,omitempty"`
	AuthorAccountID string          `json:"author_account_id,omitempty"`
	Text            string          `json:"text"`
	ThreadTS        string          `json:"thread_ts,omitempty"`
	Blocks          json.RawMessage `json:"blocks,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	ReplyBroadcast  bool            `json:"reply_broadcast,omitempty"`
}

// UpdateMessageParams holds the parameters for updating a message.
type UpdateMessageParams struct {
	Text     *string         `json:"text,omitempty"`
	Blocks   json.RawMessage `json:"blocks,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// ListMessagesParams holds pagination and filter options for message history.
type ListMessagesParams struct {
	ChannelID          string `json:"channel_id"`
	Latest             string `json:"latest,omitempty"`
	Oldest             string `json:"oldest,omitempty"`
	Inclusive          bool   `json:"inclusive,omitempty"`
	IncludeAllMetadata bool   `json:"include_all_metadata,omitempty"`
	Cursor             string `json:"cursor"`
	Limit              int    `json:"limit"`
}

// ListRepliesParams holds pagination options for fetching thread replies.
type ListRepliesParams struct {
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"thread_ts"`
	Cursor    string `json:"cursor"`
	Limit     int    `json:"limit"`
}

// AddReactionParams holds the parameters for adding a reaction.
type AddReactionParams struct {
	ChannelID string `json:"channel_id"`
	MessageTS string `json:"message_ts"`
	UserID    string `json:"user_id"`
	Emoji     string `json:"emoji"`
}

// RemoveReactionParams holds the parameters for removing a reaction.
type RemoveReactionParams struct {
	ChannelID string `json:"channel_id"`
	MessageTS string `json:"message_ts"`
	UserID    string `json:"user_id"`
	Emoji     string `json:"emoji"`
}
