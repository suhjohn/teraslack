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
	Query          string   `json:"query"`
	Kinds          []string `json:"kinds,omitempty"`
	WorkspaceID    *string  `json:"workspace_id,omitempty"`
	ConversationID *string  `json:"conversation_id,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
	Limit          int      `json:"limit,omitempty"`
}

type SearchHit struct {
	Kind           string         `json:"kind"`
	ResourceID     string         `json:"resource_id"`
	Score          float64        `json:"score"`
	Title          *string        `json:"title,omitempty"`
	Snippet        *string        `json:"snippet,omitempty"`
	WorkspaceID    *string        `json:"workspace_id,omitempty"`
	ConversationID *string        `json:"conversation_id,omitempty"`
	Message        *Message       `json:"message,omitempty"`
	Conversation   *Conversation  `json:"conversation,omitempty"`
	Workspace      *Workspace     `json:"workspace,omitempty"`
	User           *User          `json:"user,omitempty"`
	Event          *ExternalEvent `json:"event,omitempty"`
}

type SearchResponse struct {
	Items      []SearchHit `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
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

type DashboardScope struct {
	WorkspaceID   *string `json:"workspace_id,omitempty"`
	WorkspaceName *string `json:"workspace_name,omitempty"`
}

type DashboardOverview struct {
	Scope    DashboardScope          `json:"scope"`
	APIKeys  DashboardAPIKeySummary  `json:"api_keys"`
	Traffic  DashboardTrafficSummary `json:"traffic"`
	Webhooks DashboardWebhookSummary `json:"webhooks"`
	Data     DashboardDataSummary    `json:"data"`
}

type DashboardAPIKeySummary struct {
	Total        int     `json:"total"`
	Active       int     `json:"active"`
	Revoked      int     `json:"revoked"`
	ExpiringSoon int     `json:"expiring_soon"`
	Stale        int     `json:"stale"`
	LastUsedAt   *string `json:"last_used_at,omitempty"`
}

type DashboardTrafficSummary struct {
	Requests24h    int `json:"requests_24h"`
	Requests7d     int `json:"requests_7d"`
	Success7d      int `json:"success_7d"`
	ClientErrors7d int `json:"client_errors_7d"`
	ServerErrors7d int `json:"server_errors_7d"`
	RateLimited7d  int `json:"rate_limited_7d"`
	AvgDurationMs  int `json:"avg_duration_ms"`
	P95DurationMs  int `json:"p95_duration_ms"`
}

type DashboardWebhookSummary struct {
	Subscriptions        int `json:"subscriptions"`
	EnabledSubscriptions int `json:"enabled_subscriptions"`
	PendingDeliveries    int `json:"pending_deliveries"`
	FailedDeliveries     int `json:"failed_deliveries"`
	Delivered24h         int `json:"delivered_24h"`
	Failed24h            int `json:"failed_24h"`
}

type DashboardDataSummary struct {
	Conversations          int `json:"conversations"`
	Messages7d             int `json:"messages_7d"`
	RecentEvents24h        int `json:"recent_events_24h"`
	MemberConversations    int `json:"member_conversations"`
	BroadcastConversations int `json:"broadcast_conversations"`
}

type DashboardTrafficResponse struct {
	Scope       DashboardScope            `json:"scope"`
	Days        int                       `json:"days"`
	Totals      DashboardTrafficTotals    `json:"totals"`
	Series      []DashboardTrafficPoint   `json:"series"`
	ByEndpoint  []DashboardEndpointStat   `json:"by_endpoint"`
	ByKey       []DashboardKeyTrafficStat `json:"by_key"`
	StatusCodes []DashboardStatusCodeStat `json:"status_codes"`
}

type DashboardTrafficTotals struct {
	Requests      int `json:"requests"`
	Success       int `json:"success"`
	ClientErrors  int `json:"client_errors"`
	ServerErrors  int `json:"server_errors"`
	RateLimited   int `json:"rate_limited"`
	SessionReqs   int `json:"session_requests"`
	APIKeyReqs    int `json:"api_key_requests"`
	AvgDurationMs int `json:"avg_duration_ms"`
	P95DurationMs int `json:"p95_duration_ms"`
}

type DashboardTrafficPoint struct {
	Date         string `json:"date"`
	Requests     int    `json:"requests"`
	Success      int    `json:"success"`
	ClientErrors int    `json:"client_errors"`
	ServerErrors int    `json:"server_errors"`
	RateLimited  int    `json:"rate_limited"`
}

type DashboardEndpointStat struct {
	Method        string  `json:"method"`
	Path          string  `json:"path"`
	Requests      int     `json:"requests"`
	SuccessRate   float64 `json:"success_rate"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P95DurationMs int     `json:"p95_duration_ms"`
	LastSeenAt    *string `json:"last_seen_at,omitempty"`
}

type DashboardKeyTrafficStat struct {
	APIKeyID         string  `json:"api_key_id"`
	Label            string  `json:"label"`
	ScopeType        string  `json:"scope_type"`
	ScopeWorkspaceID *string `json:"scope_workspace_id,omitempty"`
	Requests         int     `json:"requests"`
	SuccessRate      float64 `json:"success_rate"`
	LastUsedAt       *string `json:"last_used_at,omitempty"`
	LastRequestAt    *string `json:"last_request_at,omitempty"`
}

type DashboardStatusCodeStat struct {
	StatusCode int `json:"status_code"`
	Count      int `json:"count"`
}

type DashboardWebhooksResponse struct {
	Scope            DashboardScope                 `json:"scope"`
	Summary          DashboardWebhookSummary        `json:"summary"`
	Subscriptions    []DashboardWebhookSubscription `json:"subscriptions"`
	RecentDeliveries []DashboardWebhookDelivery     `json:"recent_deliveries"`
}

type DashboardWebhookSubscription struct {
	SubscriptionID  string  `json:"subscription_id"`
	URL             string  `json:"url"`
	Enabled         bool    `json:"enabled"`
	EventType       *string `json:"event_type,omitempty"`
	ResourceType    *string `json:"resource_type,omitempty"`
	ResourceID      *string `json:"resource_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	TotalDeliveries int     `json:"total_deliveries"`
	DeliveredCount  int     `json:"delivered_count"`
	FailedCount     int     `json:"failed_count"`
	PendingCount    int     `json:"pending_count"`
	LastDeliveryAt  *string `json:"last_delivery_at,omitempty"`
	LastStatus      *string `json:"last_status,omitempty"`
	LastError       *string `json:"last_error,omitempty"`
}

type DashboardWebhookDelivery struct {
	DeliveryID     int64   `json:"delivery_id"`
	SubscriptionID string  `json:"subscription_id"`
	URL            string  `json:"url"`
	EventID        int64   `json:"event_id"`
	EventType      string  `json:"event_type"`
	ResourceType   string  `json:"resource_type"`
	ResourceID     string  `json:"resource_id"`
	Status         string  `json:"status"`
	AttemptCount   int     `json:"attempt_count"`
	LastError      *string `json:"last_error,omitempty"`
	DeliveredAt    *string `json:"delivered_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

type DashboardDataActivityResponse struct {
	Scope            DashboardScope                  `json:"scope"`
	Days             int                             `json:"days"`
	Summary          DashboardDataSummary            `json:"summary"`
	Series           []DashboardDataPoint            `json:"series"`
	RoomMix          []DashboardCountBucket          `json:"room_mix"`
	TopConversations []DashboardConversationActivity `json:"top_conversations"`
}

type DashboardDataPoint struct {
	Date                 string `json:"date"`
	ConversationsCreated int    `json:"conversations_created"`
	MessagesCreated      int    `json:"messages_created"`
	EventsPublished      int    `json:"events_published"`
}

type DashboardCountBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type DashboardConversationActivity struct {
	ConversationID   string  `json:"conversation_id"`
	Title            *string `json:"title,omitempty"`
	AccessPolicy     string  `json:"access_policy"`
	ParticipantCount int     `json:"participant_count"`
	LastMessageAt    *string `json:"last_message_at,omitempty"`
	MessageCount     int     `json:"message_count"`
}

type DashboardAuditResponse struct {
	Scope    DashboardScope         `json:"scope"`
	Items    []DashboardAuditItem   `json:"items"`
	TopTypes []DashboardCountBucket `json:"top_types"`
}

type DashboardAuditItem struct {
	ID            int64          `json:"id"`
	EventType     string         `json:"event_type"`
	AggregateType string         `json:"aggregate_type"`
	AggregateID   string         `json:"aggregate_id"`
	WorkspaceID   *string        `json:"workspace_id,omitempty"`
	ActorUserID   *string        `json:"actor_user_id,omitempty"`
	CreatedAt     string         `json:"created_at"`
	Payload       map[string]any `json:"payload"`
}
