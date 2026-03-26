package domain

import (
	"encoding/json"
	"time"
)

// WorkspaceDiscoverability controls how visible a workspace is.
type WorkspaceDiscoverability string

const (
	WorkspaceDiscoverabilityOpen       WorkspaceDiscoverability = "open"
	WorkspaceDiscoverabilityInviteOnly WorkspaceDiscoverability = "invite_only"
)

// Workspace stores workspace-level settings and metadata.
type Workspace struct {
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Domain          string                   `json:"domain"`
	EmailDomain     string                   `json:"email_domain"`
	Description     string                   `json:"description"`
	Icon            WorkspaceIcon            `json:"icon"`
	Discoverability WorkspaceDiscoverability `json:"discoverability"`
	DefaultChannels []string                 `json:"default_channels"`
	Preferences     json.RawMessage          `json:"preferences"`
	ProfileFields   []WorkspaceProfileField  `json:"profile_fields"`
	Billing         WorkspaceBilling         `json:"billing"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

// WorkspaceIcon contains image URLs for a workspace icon.
type WorkspaceIcon struct {
	ImageOriginal string `json:"image_original,omitempty"`
	Image34       string `json:"image_34,omitempty"`
	Image44       string `json:"image_44,omitempty"`
}

// WorkspaceProfileField defines a workspace profile field.
type WorkspaceProfileField struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Hint     string   `json:"hint,omitempty"`
	Type     string   `json:"type"`
	Options  []string `json:"options,omitempty"`
	Ordering int      `json:"ordering"`
}

// WorkspaceBilling stores workspace billing metadata.
type WorkspaceBilling struct {
	Plan         string `json:"plan"`
	Status       string `json:"status"`
	BillingEmail string `json:"billing_email,omitempty"`
}

// WorkspaceBillableInfo captures whether a user is billable.
type WorkspaceBillableInfo struct {
	UserID        string `json:"user_id"`
	BillingActive bool   `json:"billing_active"`
}

// WorkspaceAccessLog is a simplified access log entry.
type WorkspaceAccessLog struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	EventType string    `json:"event_type"`
	DateFirst time.Time `json:"date_first"`
	DateLast  time.Time `json:"date_last"`
}

// WorkspaceIntegrationLog is a simplified integration log entry.
type WorkspaceIntegrationLog struct {
	AppID    string    `json:"app_id"`
	AppType  string    `json:"app_type"`
	AppName  string    `json:"app_name"`
	UserID   string    `json:"user_id"`
	UserName string    `json:"user_name"`
	Action   string    `json:"action"`
	Date     time.Time `json:"date"`
}

// ExternalWorkspace models a Slack Connect style external workspace link.
type ExternalWorkspace struct {
	ID             string     `json:"id"`
	ExternalWorkspaceID string     `json:"external_workspace_id"`
	Name           string     `json:"name"`
	ConnectionType string     `json:"connection_type"`
	Connected      bool       `json:"connected"`
	CreatedAt      time.Time  `json:"created_at"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
}

// CreateWorkspaceParams contains workspace creation inputs.
type CreateWorkspaceParams struct {
	Name            string                   `json:"name"`
	Domain          string                   `json:"domain"`
	EmailDomain     string                   `json:"email_domain"`
	Description     string                   `json:"description"`
	Icon            WorkspaceIcon            `json:"icon"`
	Discoverability WorkspaceDiscoverability `json:"discoverability"`
	DefaultChannels []string                 `json:"default_channels"`
	Preferences     json.RawMessage          `json:"preferences"`
	ProfileFields   []WorkspaceProfileField  `json:"profile_fields"`
	Billing         WorkspaceBilling         `json:"billing"`
}

// UpdateWorkspaceParams contains partial workspace updates.
type UpdateWorkspaceParams struct {
	Name            *string                   `json:"name,omitempty"`
	Domain          *string                   `json:"domain,omitempty"`
	EmailDomain     *string                   `json:"email_domain,omitempty"`
	Description     *string                   `json:"description,omitempty"`
	Icon            *WorkspaceIcon            `json:"icon,omitempty"`
	Discoverability *WorkspaceDiscoverability `json:"discoverability,omitempty"`
	DefaultChannels *[]string                 `json:"default_channels,omitempty"`
	Preferences     json.RawMessage           `json:"preferences,omitempty"`
	ProfileFields   *[]WorkspaceProfileField  `json:"profile_fields,omitempty"`
	Billing         *WorkspaceBilling         `json:"billing,omitempty"`
}
