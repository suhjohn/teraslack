package domain

import "time"

type WorkspaceInvite struct {
	ID               string     `json:"id"`
	WorkspaceID      string     `json:"workspace_id"`
	Email            string     `json:"email"`
	InvitedBy        string     `json:"invited_by"`
	AcceptedByUserID string     `json:"accepted_by_user_id,omitempty"`
	ExpiresAt        time.Time  `json:"expires_at"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateWorkspaceInviteParams struct {
	WorkspaceID string    `json:"workspace_id"`
	Email       string    `json:"email"`
	InvitedBy   string    `json:"invited_by"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type CreateWorkspaceInviteResult struct {
	Invite    *WorkspaceInvite `json:"invite"`
	Code      string           `json:"code"`
	InviteURL string           `json:"invite_url"`
}

type AcceptWorkspaceInviteParams struct {
	Code string `json:"code"`
}

type AcceptWorkspaceInviteResult struct {
	Invite *WorkspaceInvite `json:"invite"`
	User   *User            `json:"user"`
}
