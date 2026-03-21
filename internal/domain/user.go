package domain

import "time"

// User represents a workspace principal (human, agent, or system).
type User struct {
	ID            string        `json:"id"`
	TeamID        string        `json:"team_id"`
	Name          string        `json:"name"`
	RealName      string        `json:"real_name"`
	DisplayName   string        `json:"display_name"`
	Email         string        `json:"email"`
	PrincipalType PrincipalType `json:"principal_type"`
	OwnerID       string        `json:"owner_id,omitempty"` // For agents: the human who owns this agent
	IsBot         bool          `json:"is_bot"`
	IsAdmin       bool          `json:"is_admin"`
	IsOwner       bool          `json:"is_owner"`
	IsRestricted  bool          `json:"is_restricted"`
	Deleted       bool          `json:"deleted"`
	Profile       UserProfile   `json:"profile"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// UserProfile contains display and contact information for a user.
type UserProfile struct {
	Title                string            `json:"title"`
	Phone                string            `json:"phone"`
	StatusText           string            `json:"status_text"`
	StatusEmoji          string            `json:"status_emoji"`
	StatusExpiration     int64             `json:"status_expiration"`
	AvatarHash           string            `json:"avatar_hash"`
	ImageOriginal        string            `json:"image_original"`
	Image48              string            `json:"image_48"`
	Image192             string            `json:"image_192"`
	Image512             string            `json:"image_512"`
	Fields               map[string]CustomField `json:"fields,omitempty"`
}

// CustomField is a workspace-defined custom profile field.
type CustomField struct {
	Value string `json:"value"`
	Alt   string `json:"alt"`
}

// CreateUserParams holds the parameters for creating a new user.
type CreateUserParams struct {
	TeamID        string        `json:"team_id"`
	Name          string        `json:"name"`
	RealName      string        `json:"real_name"`
	DisplayName   string        `json:"display_name"`
	Email         string        `json:"email"`
	PrincipalType PrincipalType `json:"principal_type,omitempty"` // Defaults to "human" if empty
	OwnerID       string        `json:"owner_id,omitempty"`
	IsBot         bool          `json:"is_bot"`
	IsAdmin       bool          `json:"is_admin"`
	Profile       UserProfile   `json:"profile"`
}

// UpdateUserParams holds the parameters for updating a user.
type UpdateUserParams struct {
	RealName    *string      `json:"real_name,omitempty"`
	DisplayName *string      `json:"display_name,omitempty"`
	Email       *string      `json:"email,omitempty"`
	IsAdmin     *bool        `json:"is_admin,omitempty"`
	IsRestricted *bool       `json:"is_restricted,omitempty"`
	Deleted     *bool        `json:"deleted,omitempty"`
	Profile     *UserProfile `json:"profile,omitempty"`
}

// ListUsersParams holds pagination and filter options.
type ListUsersParams struct {
	TeamID string `json:"team_id"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}
