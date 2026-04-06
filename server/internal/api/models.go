package api

type ErrorResponse struct {
	Code      string             `json:"code"`
	Message   string             `json:"message"`
	RequestID string             `json:"request_id,omitempty"`
	Errors    []ValidationDetail `json:"errors,omitempty"`
}

type ValidationDetail struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CollectionResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type SessionEnvelope struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type UserProfile struct {
	Handle      string  `json:"handle"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	Bio         *string `json:"bio,omitempty"`
}

type User struct {
	ID            string      `json:"id"`
	PrincipalType string      `json:"principal_type"`
	Status        string      `json:"status"`
	Email         *string     `json:"email,omitempty"`
	Profile       UserProfile `json:"profile"`
}

type WorkspaceMembershipSummary struct {
	WorkspaceID string `json:"workspace_id"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	Name        string `json:"name"`
}

type MeResponse struct {
	User       User                         `json:"user"`
	Workspaces []WorkspaceMembershipSummary `json:"workspaces"`
}

type UpdateProfileRequest struct {
	Handle      *string `json:"handle,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	Bio         *string `json:"bio,omitempty"`
}

type StartEmailLoginRequest struct {
	Email string `json:"email"`
}

type GenericStatusResponse struct {
	Status string `json:"status"`
}

type VerifyEmailLoginRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type AuthResponse struct {
	Session SessionEnvelope `json:"session"`
	User    User            `json:"user"`
}

type OAuthStartRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type OAuthStartResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

type APIKey struct {
	ID               string  `json:"id"`
	Label            string  `json:"label"`
	ScopeType        string  `json:"scope_type"`
	ScopeWorkspaceID *string `json:"scope_workspace_id,omitempty"`
	ExpiresAt        *string `json:"expires_at,omitempty"`
	LastUsedAt       *string `json:"last_used_at,omitempty"`
	RevokedAt        *string `json:"revoked_at,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Label            string  `json:"label"`
	ScopeType        string  `json:"scope_type"`
	ScopeWorkspaceID *string `json:"scope_workspace_id"`
	ExpiresAt        *string `json:"expires_at,omitempty"`
}

type CreateAPIKeyResponse struct {
	APIKey APIKey `json:"api_key"`
	Secret string `json:"secret"`
}

type SearchRequest struct {
	Query       string   `json:"query"`
	EntityTypes []string `json:"entity_types,omitempty"`
	WorkspaceID *string  `json:"workspace_id,omitempty"`
	Limit       *int     `json:"limit,omitempty"`
	Cursor      *string  `json:"cursor,omitempty"`
}

type SearchItem struct {
	EntityType  string  `json:"entity_type"`
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Subtitle    string  `json:"subtitle,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

type Workspace struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	CreatedByUserID string `json:"created_by_user_id"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type CreateWorkspaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UpdateWorkspaceRequest struct {
	Name *string `json:"name,omitempty"`
	Slug *string `json:"slug,omitempty"`
}

type WorkspaceMember struct {
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	User        User   `json:"user"`
}

type CreateWorkspaceInviteRequest struct {
	Email *string `json:"email,omitempty"`
}

type CreateWorkspaceInviteResponse struct {
	InviteToken string `json:"invite_token"`
	InviteURL   string `json:"invite_url"`
}

type UpdateWorkspaceMemberRequest struct {
	Role   *string `json:"role,omitempty"`
	Status *string `json:"status,omitempty"`
}

type Conversation struct {
	ID               string  `json:"id"`
	WorkspaceID      *string `json:"workspace_id,omitempty"`
	AccessPolicy     string  `json:"access_policy"`
	ParticipantCount int     `json:"participant_count"`
	Title            *string `json:"title,omitempty"`
	Description      *string `json:"description,omitempty"`
	CreatedByUserID  string  `json:"created_by_user_id"`
	Archived         bool    `json:"archived"`
	LastMessageAt    *string `json:"last_message_at,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type CreateConversationRequest struct {
	WorkspaceID        *string  `json:"workspace_id"`
	AccessPolicy       string   `json:"access_policy"`
	ParticipantUserIDs []string `json:"participant_user_ids,omitempty"`
	Title              *string  `json:"title"`
	Description        *string  `json:"description"`
}

type UpdateConversationRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Archived    *bool   `json:"archived,omitempty"`
}

type AddParticipantsRequest struct {
	UserIDs []string `json:"user_ids"`
}

type CreateConversationInviteRequest struct {
	ExpiresAt      *string  `json:"expires_at"`
	Mode           string   `json:"mode"`
	AllowedUserIDs []string `json:"allowed_user_ids,omitempty"`
	AllowedEmails  []string `json:"allowed_emails,omitempty"`
}

type CreateConversationInviteResponse struct {
	InviteToken string `json:"invite_token"`
	InviteURL   string `json:"invite_url"`
}

type Message struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversation_id"`
	AuthorUserID   string         `json:"author_user_id"`
	BodyText       string         `json:"body_text"`
	BodyRich       map[string]any `json:"body_rich,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	EditedAt       *string        `json:"edited_at,omitempty"`
	DeletedAt      *string        `json:"deleted_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
}

type CreateMessageRequest struct {
	BodyText string         `json:"body_text"`
	BodyRich map[string]any `json:"body_rich"`
	Metadata map[string]any `json:"metadata"`
}

type UpdateMessageRequest struct {
	BodyText string         `json:"body_text"`
	BodyRich map[string]any `json:"body_rich"`
	Metadata map[string]any `json:"metadata"`
}

type UpdateReadStateRequest struct {
	LastReadMessageID string `json:"last_read_message_id"`
}

type ExternalEvent struct {
	ID           int64          `json:"id"`
	WorkspaceID  *string        `json:"workspace_id,omitempty"`
	Type         string         `json:"type"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	OccurredAt   string         `json:"occurred_at"`
	Payload      map[string]any `json:"payload"`
}

type EventSubscription struct {
	ID           string  `json:"id"`
	WorkspaceID  *string `json:"workspace_id,omitempty"`
	URL          string  `json:"url"`
	Enabled      bool    `json:"enabled"`
	EventType    *string `json:"event_type,omitempty"`
	ResourceType *string `json:"resource_type,omitempty"`
	ResourceID   *string `json:"resource_id,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type CreateEventSubscriptionRequest struct {
	WorkspaceID  *string `json:"workspace_id,omitempty"`
	URL          string  `json:"url"`
	EventType    *string `json:"event_type,omitempty"`
	ResourceType *string `json:"resource_type,omitempty"`
	ResourceID   *string `json:"resource_id,omitempty"`
	Secret       string  `json:"secret"`
}

type UpdateEventSubscriptionRequest struct {
	Enabled bool `json:"enabled"`
}
