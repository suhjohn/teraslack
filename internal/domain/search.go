package domain

import "encoding/json"

// SearchParams holds the parameters for unified search across all resource types.
type SearchParams struct {
	TeamID string   `json:"team_id"`
	Query  string   `json:"query"`
	Types  []string `json:"types,omitempty"` // optional filter: "user", "message", "conversation", "file", etc.
	Limit  int      `json:"limit,omitempty"`
	Cursor string   `json:"cursor,omitempty"`
}

// SearchResult represents a single search result from any resource type.
type SearchResult struct {
	Type  string          `json:"type"`  // "user", "message", "conversation", "file", etc.
	Score float64         `json:"score"` // relevance score from Turbopuffer
	Data  json.RawMessage `json:"data"`  // full entity snapshot
}
