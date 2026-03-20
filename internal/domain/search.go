package domain

// SearchMessagesParams holds the parameters for searching messages.
type SearchMessagesParams struct {
	TeamID string `json:"team_id"`
	Query  string `json:"query"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// SearchFilesParams holds the parameters for searching files.
type SearchFilesParams struct {
	TeamID string `json:"team_id"`
	Query  string `json:"query"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// SemanticSearchParams holds the parameters for semantic vector search.
type SemanticSearchParams struct {
	TeamID    string `json:"team_id"`
	Query     string `json:"query"`
	ChannelID string `json:"channel_id,omitempty"`
	Limit     int    `json:"limit"`
}

// SemanticSearchResult represents a single semantic search result.
type SemanticSearchResult struct {
	Message  Message `json:"message"`
	Score    float64 `json:"score"`
}
