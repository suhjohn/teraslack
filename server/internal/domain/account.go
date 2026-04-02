package domain

import "time"

// Account is the canonical identity record that can participate in one or more workspaces.
type Account struct {
	ID            string        `json:"id"`
	PrincipalType PrincipalType `json:"principal_type"`
	Name          string        `json:"name"`
	RealName      string        `json:"real_name"`
	DisplayName   string        `json:"display_name"`
	Email         string        `json:"email"`
	IsBot         bool          `json:"is_bot"`
	Deleted       bool          `json:"deleted"`
	Profile       UserProfile   `json:"profile"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type CreateAccountParams struct {
	PrincipalType PrincipalType `json:"principal_type"`
	Name          string        `json:"name"`
	RealName      string        `json:"real_name"`
	DisplayName   string        `json:"display_name"`
	Email         string        `json:"email"`
	IsBot         bool          `json:"is_bot"`
	Deleted       bool          `json:"deleted"`
	Profile       UserProfile   `json:"profile"`
}

