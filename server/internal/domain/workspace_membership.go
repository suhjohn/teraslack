package domain

import "time"

const (
	WorkspaceMembershipStatusActive   = "active"
	WorkspaceMembershipStatusInvited  = "invited"
	WorkspaceMembershipStatusDisabled = "disabled"

	WorkspaceMembershipKindFull  = "full"
	WorkspaceMembershipKindGuest = "guest"

	WorkspaceGuestScopeSingleConversation = "single_conversation"
	WorkspaceGuestScopeConversationAllow  = "conversation_allowlist"
	WorkspaceGuestScopeWorkspaceFull      = "workspace_full"

	WorkspaceMembershipRoleGuest = "guest"
)

type WorkspaceMembership struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	AccountID      string    `json:"account_id"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	MembershipKind string    `json:"membership_kind"`
	GuestScope     string    `json:"guest_scope"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (m WorkspaceMembership) EffectiveAccountType() AccountType {
	switch m.Role {
	case string(AccountTypePrimaryAdmin):
		return AccountTypePrimaryAdmin
	case string(AccountTypeAdmin):
		return AccountTypeAdmin
	default:
		return AccountTypeMember
	}
}

func (m WorkspaceMembership) IsGuest() bool {
	return m.MembershipKind == WorkspaceMembershipKindGuest || m.Role == WorkspaceMembershipRoleGuest
}

func (m WorkspaceMembership) HasWorkspaceWideAccess() bool {
	return !m.IsGuest() || m.GuestScope == WorkspaceGuestScopeWorkspaceFull
}
