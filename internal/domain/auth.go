package domain

import "time"

// Token represents an API token for authentication.
type Token struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"team_id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"`
	Scopes    []string  `json:"scopes"`
	IsBot     bool      `json:"is_bot"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
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
