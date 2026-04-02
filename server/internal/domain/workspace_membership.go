package domain

import "time"

// WorkspaceMembership links a canonical account to a workspace. Legacy user rows may be materialized lazily.
type WorkspaceMembership struct {
	ID          string      `json:"id"`
	AccountID   string      `json:"account_id"`
	WorkspaceID string      `json:"workspace_id"`
	UserID      string      `json:"user_id,omitempty"`
	AccountType AccountType `json:"account_type,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type CreateWorkspaceMembershipParams struct {
	AccountID   string      `json:"account_id"`
	WorkspaceID string      `json:"workspace_id"`
	UserID      string      `json:"user_id,omitempty"`
	AccountType AccountType `json:"account_type,omitempty"`
}
