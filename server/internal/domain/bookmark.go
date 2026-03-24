package domain

import "time"

// Bookmark represents a bookmarked link in a conversation.
type Bookmark struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	Link      string    `json:"link"`
	Emoji     string    `json:"emoji,omitempty"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateBookmarkParams holds the parameters for creating a bookmark.
type CreateBookmarkParams struct {
	ChannelID string `json:"channel_id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Link      string `json:"link"`
	Emoji     string `json:"emoji,omitempty"`
	CreatedBy string `json:"created_by"`
}

// UpdateBookmarkParams holds the parameters for updating a bookmark.
type UpdateBookmarkParams struct {
	Title     *string `json:"title,omitempty"`
	Link      *string `json:"link,omitempty"`
	Emoji     *string `json:"emoji,omitempty"`
	UpdatedBy string  `json:"updated_by"`
}

// ListBookmarksParams holds the parameters for listing bookmarks.
type ListBookmarksParams struct {
	ChannelID string `json:"channel_id"`
}
