package domain

import "time"

// ConversationType represents the kind of conversation.
type ConversationType string

// ConversationOwnerType represents the owning subject for a conversation.
type ConversationOwnerType string

const (
	ConversationTypePublicChannel  ConversationType = "public_channel"
	ConversationTypePrivateChannel ConversationType = "private_channel"
	ConversationTypeIM             ConversationType = "im"
	ConversationTypeMPIM           ConversationType = "mpim"

	ConversationOwnerTypeAccount   ConversationOwnerType = "account"
	ConversationOwnerTypeWorkspace ConversationOwnerType = "workspace"
)

// Conversation represents a channel, DM, or group DM.
type Conversation struct {
	ID               string                `json:"id"`
	WorkspaceID      string                `json:"workspace_id,omitempty"`
	OwnerType        ConversationOwnerType `json:"owner_type"`
	OwnerAccountID   string                `json:"owner_account_id,omitempty"`
	OwnerWorkspaceID string                `json:"owner_workspace_id,omitempty"`
	Name             string                `json:"name"`
	Type             ConversationType      `json:"type"`
	CreatorID        string                `json:"creator_id,omitempty"`
	IsArchived       bool                  `json:"is_archived"`
	Topic            TopicPurpose          `json:"topic"`
	Purpose          TopicPurpose          `json:"purpose"`
	NumMembers       int                   `json:"num_members"`
	LastMessageTS    *string               `json:"last_message_ts,omitempty"`
	LastActivityTS   *string               `json:"last_activity_ts,omitempty"`
	LastReadTS       *string               `json:"last_read_ts,omitempty"`
	HasUnread        *bool                 `json:"has_unread,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

// TopicPurpose holds topic or purpose metadata.
type TopicPurpose struct {
	Value   string     `json:"value"`
	Creator string     `json:"creator"`
	LastSet *time.Time `json:"last_set"`
}

// ConversationMember represents a user's membership in a conversation.
type ConversationMember struct {
	ConversationID string    `json:"conversation_id"`
	AccountID      string    `json:"account_id,omitempty"`
	UserID         string    `json:"user_id"`
	JoinedAt       time.Time `json:"joined_at"`
}

type ConversationManagerAssignment struct {
	ConversationID string    `json:"conversation_id"`
	AccountID      string    `json:"account_id,omitempty"`
	UserID         string    `json:"user_id"`
	AssignedBy     string    `json:"assigned_by"`
	CreatedAt      time.Time `json:"created_at"`
}

type ConversationPostingPolicyType string

const (
	ConversationPostingPolicyEveryone              ConversationPostingPolicyType = "everyone"
	ConversationPostingPolicyAdminsOnly            ConversationPostingPolicyType = "admins_only"
	ConversationPostingPolicyMembersWithPermission ConversationPostingPolicyType = "members_with_permission"
	ConversationPostingPolicyCustom                ConversationPostingPolicyType = "custom"
)

type ConversationPostingPolicy struct {
	ConversationID        string                        `json:"conversation_id"`
	PolicyType            ConversationPostingPolicyType `json:"policy_type"`
	AllowedAccountTypes   []AccountType                 `json:"allowed_account_types,omitempty"`
	AllowedDelegatedRoles []DelegatedRole               `json:"allowed_delegated_roles,omitempty"`
	AllowedAccountIDs     []string                      `json:"allowed_account_ids,omitempty"`
	AllowedUserIDs        []string                      `json:"allowed_user_ids,omitempty"`
	UpdatedBy             string                        `json:"updated_by,omitempty"`
	UpdatedAt             time.Time                     `json:"updated_at"`
}

func DefaultConversationPostingPolicy(conversationID string) ConversationPostingPolicy {
	return ConversationPostingPolicy{
		ConversationID: conversationID,
		PolicyType:     ConversationPostingPolicyEveryone,
	}
}

func IsValidConversationPostingPolicyType(policyType ConversationPostingPolicyType) bool {
	switch policyType {
	case ConversationPostingPolicyEveryone,
		ConversationPostingPolicyAdminsOnly,
		ConversationPostingPolicyMembersWithPermission,
		ConversationPostingPolicyCustom:
		return true
	default:
		return false
	}
}

// CreateConversationParams holds the parameters for creating a conversation.
type CreateConversationParams struct {
	WorkspaceID      string                `json:"workspace_id,omitempty"`
	OwnerType        ConversationOwnerType `json:"owner_type"`
	OwnerAccountID   string                `json:"owner_account_id,omitempty"`
	OwnerWorkspaceID string                `json:"owner_workspace_id,omitempty"`
	Name             string                `json:"name"`
	Type             ConversationType      `json:"type"`
	CreatorID        string                `json:"creator_id,omitempty"`
	UserIDs          []string              `json:"user_ids,omitempty"`
	AccountIDs       []string              `json:"account_ids,omitempty"`
	Topic            string                `json:"topic"`
	Purpose          string                `json:"purpose"`
}

// UpdateConversationParams holds the parameters for updating a conversation.
type UpdateConversationParams struct {
	Name       *string `json:"name,omitempty"`
	IsArchived *bool   `json:"is_archived,omitempty"`
}

// SetTopicParams holds the parameters for setting a conversation topic.
type SetTopicParams struct {
	Topic   string `json:"topic"`
	SetByID string `json:"set_by_id"`
}

// SetPurposeParams holds the parameters for setting a conversation purpose.
type SetPurposeParams struct {
	Purpose string `json:"purpose"`
	SetByID string `json:"set_by_id"`
}

// ListConversationsParams holds pagination and filter options.
type ListConversationsParams struct {
	WorkspaceID     string             `json:"workspace_id"`
	AccountID       string             `json:"account_id"`
	UserID          string             `json:"user_id"`
	Types           []ConversationType `json:"types"`
	ExcludeArchived bool               `json:"exclude_archived"`
	Cursor          string             `json:"cursor"`
	Limit           int                `json:"limit"`
}

func IsValidConversationOwnerType(ownerType ConversationOwnerType) bool {
	switch ownerType {
	case ConversationOwnerTypeAccount, ConversationOwnerTypeWorkspace:
		return true
	default:
		return false
	}
}

func (c *Conversation) EffectiveWorkspaceID() string {
	if c == nil {
		return ""
	}
	if c.OwnerWorkspaceID != "" {
		return c.OwnerWorkspaceID
	}
	return c.WorkspaceID
}
