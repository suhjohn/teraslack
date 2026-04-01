package domain

import "time"

type InstallSessionStatus string

const (
	InstallSessionStatusPending  InstallSessionStatus = "pending"
	InstallSessionStatusApproved InstallSessionStatus = "approved"
	InstallSessionStatusConsumed InstallSessionStatus = "consumed"
	InstallSessionStatusExpired  InstallSessionStatus = "expired"
	InstallSessionStatusCanceled InstallSessionStatus = "cancelled"
)

type InstallSession struct {
	ID                     string               `json:"id"`
	PollTokenHash          string               `json:"-"`
	Status                 InstallSessionStatus `json:"status"`
	WorkspaceID            string               `json:"workspace_id,omitempty"`
	ApprovedByUserID       string               `json:"approved_by_user_id,omitempty"`
	CredentialID           string               `json:"credential_id,omitempty"`
	RawCredentialEncrypted string               `json:"-"`
	DeviceName             string               `json:"device_name,omitempty"`
	ClientKind             string               `json:"client_kind,omitempty"`
	ExpiresAt              time.Time            `json:"expires_at"`
	ApprovedAt             *time.Time           `json:"approved_at,omitempty"`
	ConsumedAt             *time.Time           `json:"consumed_at,omitempty"`
	CreatedAt              time.Time            `json:"created_at"`
	UpdatedAt              time.Time            `json:"updated_at"`
}

type CreateInstallSessionParams struct {
	DeviceName string
	ClientKind string
	ExpiresAt  time.Time
}

type ApproveInstallSessionParams struct {
	ID                     string
	WorkspaceID            string
	ApprovedByUserID       string
	CredentialID           string
	RawCredentialEncrypted string
	ApprovedAt             time.Time
}

type ConsumeInstallSessionParams struct {
	ID         string
	ConsumedAt time.Time
}

type ApproveInstallSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type CreateInstallSessionRequest struct {
	DeviceName string `json:"device_name"`
	ClientKind string `json:"client_kind"`
}

type CreateInstallSessionResponse struct {
	InstallID   string    `json:"install_id"`
	ApprovalURL string    `json:"approval_url"`
	PollToken   string    `json:"poll_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type PollInstallSessionRequest struct {
	PollToken string `json:"poll_token"`
}

type PollInstallSessionResponse struct {
	Status      InstallSessionStatus `json:"status"`
	BaseURL     string               `json:"base_url,omitempty"`
	WorkspaceID string               `json:"workspace_id,omitempty"`
	UserID      string               `json:"user_id,omitempty"`
	APIKey      string               `json:"api_key,omitempty"`
}

type InstallApprovalPrompt struct {
	Session             *InstallSession
	Workspace           *Workspace
	User                *User
	AvailableWorkspaces []Workspace
	SelectedWorkspaceID string
	ApprovalURL         string
}
