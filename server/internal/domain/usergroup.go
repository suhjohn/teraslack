package domain

import "time"

// Usergroup represents a group of users (e.g., agent capability groups).
type Usergroup struct {
	ID          string    `json:"id"`
	WorkspaceID      string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Handle      string    `json:"handle"`
	Description string    `json:"description"`
	IsExternal  bool      `json:"is_external"`
	Enabled     bool      `json:"enabled"`
	UserCount   int       `json:"user_count"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateUsergroupParams holds the parameters for creating a usergroup.
type CreateUsergroupParams struct {
	WorkspaceID      string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Handle      string   `json:"handle"`
	Description string   `json:"description"`
	CreatedBy   string   `json:"created_by"`
	Users       []string `json:"users,omitempty"`
}

// UpdateUsergroupParams holds the parameters for updating a usergroup.
type UpdateUsergroupParams struct {
	Name        *string `json:"name,omitempty"`
	Handle      *string `json:"handle,omitempty"`
	Description *string `json:"description,omitempty"`
	UpdatedBy   string  `json:"updated_by"`
}

// ListUsergroupsParams holds filter/pagination options for listing usergroups.
type ListUsergroupsParams struct {
	WorkspaceID         string `json:"workspace_id"`
	IncludeDisabled bool  `json:"include_disabled"`
}
