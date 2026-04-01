package domain

import "time"

// AccountType represents the effective human access level within a workspace.
type AccountType string

const (
	AccountTypeNone         AccountType = ""
	AccountTypePrimaryAdmin AccountType = "primary_admin"
	AccountTypeAdmin        AccountType = "admin"
	AccountTypeMember       AccountType = "member"
)

type DelegatedRole string

const (
	DelegatedRoleChannelsAdmin     DelegatedRole = "channels_admin"
	DelegatedRoleRolesAdmin        DelegatedRole = "roles_admin"
	DelegatedRoleSecurityAdmin     DelegatedRole = "security_admin"
	DelegatedRoleIntegrationsAdmin DelegatedRole = "integrations_admin"
	DelegatedRoleSupportReadonly   DelegatedRole = "support_readonly"
)

func IsValidDelegatedRole(role DelegatedRole) bool {
	switch role {
	case DelegatedRoleChannelsAdmin,
		DelegatedRoleRolesAdmin,
		DelegatedRoleSecurityAdmin,
		DelegatedRoleIntegrationsAdmin,
		DelegatedRoleSupportReadonly:
		return true
	default:
		return false
	}
}

// User represents a workspace principal (human, agent, or system).
type User struct {
	ID            string        `json:"id"`
	WorkspaceID        string        `json:"workspace_id"`
	Name          string        `json:"name"`
	RealName      string        `json:"real_name"`
	DisplayName   string        `json:"display_name"`
	Email         string        `json:"email"`
	PrincipalType PrincipalType `json:"principal_type"`
	OwnerID       string        `json:"owner_id,omitempty"` // For agents: the human who owns this agent
	AccountType   AccountType   `json:"account_type,omitempty"`
	IsBot         bool          `json:"is_bot"`
	Deleted       bool          `json:"deleted"`
	Profile       UserProfile   `json:"profile"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// UserProfile contains display and contact information for a user.
type UserProfile struct {
	Title            string                 `json:"title"`
	Phone            string                 `json:"phone"`
	StatusText       string                 `json:"status_text"`
	StatusEmoji      string                 `json:"status_emoji"`
	StatusExpiration int64                  `json:"status_expiration"`
	AvatarHash       string                 `json:"avatar_hash"`
	ImageOriginal    string                 `json:"image_original"`
	Image48          string                 `json:"image_48"`
	Image192         string                 `json:"image_192"`
	Image512         string                 `json:"image_512"`
	Fields           map[string]CustomField `json:"fields,omitempty"`
}

// CustomField is a workspace-defined custom profile field.
type CustomField struct {
	Value string `json:"value"`
	Alt   string `json:"alt"`
}

// CreateUserParams holds the parameters for creating a new user.
type CreateUserParams struct {
	WorkspaceID        string        `json:"workspace_id"`
	Name          string        `json:"name"`
	RealName      string        `json:"real_name"`
	DisplayName   string        `json:"display_name"`
	Email         string        `json:"email"`
	PrincipalType PrincipalType `json:"principal_type"`
	OwnerID       string        `json:"owner_id,omitempty"`
	AccountType   AccountType   `json:"account_type,omitempty"`
	IsBot         bool          `json:"is_bot"`
	Profile       UserProfile   `json:"profile"`
}

// UpdateUserParams holds the parameters for updating a user.
type UpdateUserParams struct {
	RealName    *string      `json:"real_name,omitempty"`
	DisplayName *string      `json:"display_name,omitempty"`
	Email       *string      `json:"email,omitempty"`
	AccountType *AccountType `json:"account_type,omitempty"`
	Deleted     *bool        `json:"deleted,omitempty"`
	Profile     *UserProfile `json:"profile,omitempty"`
}

// ListUsersParams holds pagination and filter options.
type ListUsersParams struct {
	WorkspaceID string `json:"workspace_id"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// EffectiveAccountType derives the canonical account type.
func (u User) EffectiveAccountType() AccountType {
	if u.PrincipalType != PrincipalTypeHuman {
		return AccountTypeNone
	}
	switch u.AccountType {
	case AccountTypePrimaryAdmin, AccountTypeAdmin, AccountTypeMember:
		return u.AccountType
	default:
		return AccountTypeNone
	}
}

// IsWorkspaceAdmin reports whether the user is a human workspace admin.
func (u User) IsWorkspaceAdmin() bool {
	switch u.EffectiveAccountType() {
	case AccountTypePrimaryAdmin, AccountTypeAdmin:
		return true
	default:
		return false
	}
}

// NormalizeAccountType resolves the effective account type for a principal.
func NormalizeAccountType(principalType PrincipalType, accountType AccountType) AccountType {
	if principalType != PrincipalTypeHuman {
		return AccountTypeNone
	}
	switch accountType {
	case AccountTypePrimaryAdmin, AccountTypeAdmin, AccountTypeMember:
		return accountType
	}
	return AccountTypeMember
}
