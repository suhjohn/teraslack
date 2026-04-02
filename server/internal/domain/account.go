package domain

import "time"

// Account is the canonical global auth identity.
// Workspace-local access and persona state belongs on User rows linked by account_id.
type Account struct {
	ID            string        `json:"id"`
	PrincipalType PrincipalType `json:"principal_type"`
	Email         string        `json:"email"`
	IsBot         bool          `json:"is_bot"`
	Deleted       bool          `json:"deleted"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type CreateAccountParams struct {
	PrincipalType PrincipalType `json:"principal_type"`
	Email         string        `json:"email"`
	IsBot         bool          `json:"is_bot"`
	Deleted       bool          `json:"deleted"`
}
