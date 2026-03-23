package domain

import "time"

// ConversationType represents the kind of conversation.
type ConversationType string

const (
	ConversationTypePublicChannel  ConversationType = "public_channel"
	ConversationTypePrivateChannel ConversationType = "private_channel"
	ConversationTypeIM             ConversationType = "im"
	ConversationTypeMPIM           ConversationType = "mpim"
)

// Conversation represents a channel, DM, or group DM.
type Conversation struct {
	ID         string           `json:"id"`
	TeamID     string           `json:"team_id"`
	Name       string           `json:"name"`
	Type       ConversationType `json:"type"`
	CreatorID  string           `json:"creator_id"`
	IsArchived bool             `json:"is_archived"`
	Topic      TopicPurpose     `json:"topic"`
	Purpose    TopicPurpose     `json:"purpose"`
	NumMembers int              `json:"num_members"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
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
	UserID         string    `json:"user_id"`
	JoinedAt       time.Time `json:"joined_at"`
}

type ConversationManagerAssignment struct {
	ConversationID string    `json:"conversation_id"`
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
	AllowedUserIDs        []string                      `json:"allowed_user_ids,omitempty"`
	AllowedUsergroupIDs   []string                      `json:"allowed_usergroup_ids,omitempty"`
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
	TeamID    string           `json:"team_id"`
	Name      string           `json:"name"`
	Type      ConversationType `json:"type"`
	CreatorID string           `json:"creator_id"`
	Topic     string           `json:"topic"`
	Purpose   string           `json:"purpose"`
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
	TeamID          string             `json:"team_id"`
	Types           []ConversationType `json:"types"`
	ExcludeArchived bool               `json:"exclude_archived"`
	Cursor          string             `json:"cursor"`
	Limit           int                `json:"limit"`
}
