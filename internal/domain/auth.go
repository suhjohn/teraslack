package domain

import "time"

// Token represents an API token for authentication.
type Token struct {
	ID        string     `json:"id"`
	TeamID    string     `json:"team_id"`
	UserID    string     `json:"user_id"`
	Token     string     `json:"token,omitempty"`      // Raw token — only populated on creation, never persisted in event_data
	TokenHash string     `json:"token_hash,omitempty"` // SHA-256 hash of the raw token
	Scopes    []string   `json:"scopes"`
	IsBot     bool       `json:"is_bot"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Redacted returns a copy of the token with sensitive fields cleared
// for safe serialization into event_data.
func (t *Token) Redacted() *Token {
	copy := *t
	copy.Token = "" // never store raw token in event log
	return &copy
}

// AuthTestResponse represents the response from auth.test.
type AuthTestResponse struct {
	TeamID string `json:"team_id"`
	UserID string `json:"user_id"`
	IsBot  bool   `json:"is_bot"`
}

// CreateTokenParams holds the parameters for creating a token.
type CreateTokenParams struct {
	TeamID string   `json:"team_id"`
	UserID string   `json:"user_id"`
	Scopes []string `json:"scopes"`
	IsBot  bool     `json:"is_bot"`
}
