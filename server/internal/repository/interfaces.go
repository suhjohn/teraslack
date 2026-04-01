package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
)

// TxBeginner abstracts the ability to begin a database transaction.
// Satisfied by *pgxpool.Pool.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WorkspaceRepository defines data access operations for workspace APIs.
type WorkspaceRepository interface {
	WithTx(tx pgx.Tx) WorkspaceRepository
	Create(ctx context.Context, params domain.CreateWorkspaceParams) (*domain.Workspace, error)
	Get(ctx context.Context, id string) (*domain.Workspace, error)
	List(ctx context.Context) ([]domain.Workspace, error)
	Update(ctx context.Context, id string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error)
	ListAdmins(ctx context.Context, workspaceID string) ([]domain.User, error)
	ListOwners(ctx context.Context, workspaceID string) ([]domain.User, error)
	ListBillableInfo(ctx context.Context, workspaceID string) ([]domain.WorkspaceBillableInfo, error)
	ListAccessLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceAccessLog, error)
	ListIntegrationLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceIntegrationLog, error)
	ListExternalWorkspaces(ctx context.Context, workspaceID string) ([]domain.ExternalWorkspace, error)
	DisconnectExternalWorkspace(ctx context.Context, workspaceID, externalWorkspaceID string) error
}

type WorkspaceInviteRepository interface {
	WithTx(tx pgx.Tx) WorkspaceInviteRepository
	Create(ctx context.Context, params domain.CreateWorkspaceInviteParams, tokenHash string) (*domain.WorkspaceInvite, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.WorkspaceInvite, error)
	MarkAccepted(ctx context.Context, id, acceptedByUserID string, acceptedAt time.Time) error
}

type InstallSessionRepository interface {
	WithTx(tx pgx.Tx) InstallSessionRepository
	Create(ctx context.Context, params domain.CreateInstallSessionParams) (*domain.InstallSession, string, error)
	Get(ctx context.Context, id string) (*domain.InstallSession, error)
	GetByPollTokenHash(ctx context.Context, id, pollTokenHash string) (*domain.InstallSession, error)
	Approve(ctx context.Context, params domain.ApproveInstallSessionParams) (*domain.InstallSession, error)
	Consume(ctx context.Context, params domain.ConsumeInstallSessionParams) (*domain.InstallSession, error)
	ExpirePending(ctx context.Context, now time.Time) error
}

// UserRepository defines data access operations for users.
type UserRepository interface {
	WithTx(tx pgx.Tx) UserRepository
	Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error)
	Get(ctx context.Context, id string) (*domain.User, error)
	GetByTeamEmail(ctx context.Context, workspaceID, email string) (*domain.User, error)
	ListByEmail(ctx context.Context, email string) ([]domain.User, error)
	Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error)
	List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error)
}

type RoleAssignmentRepository interface {
	WithTx(tx pgx.Tx) RoleAssignmentRepository
	ListByUser(ctx context.Context, workspaceID, userID string) ([]domain.DelegatedRole, error)
	ReplaceForUser(ctx context.Context, workspaceID, userID string, roles []domain.DelegatedRole, assignedBy string) error
}

type ConversationAccessRepository interface {
	WithTx(tx pgx.Tx) ConversationAccessRepository
	ListManagers(ctx context.Context, conversationID string) ([]domain.ConversationManagerAssignment, error)
	ReplaceManagers(ctx context.Context, conversationID string, userIDs []string, assignedBy string) error
	IsManager(ctx context.Context, conversationID, userID string) (bool, error)
	GetPostingPolicy(ctx context.Context, conversationID string) (*domain.ConversationPostingPolicy, error)
	UpsertPostingPolicy(ctx context.Context, policy domain.ConversationPostingPolicy) (*domain.ConversationPostingPolicy, error)
}

type ExternalPrincipalAccessRepository interface {
	WithTx(tx pgx.Tx) ExternalPrincipalAccessRepository
	Create(ctx context.Context, params domain.CreateExternalPrincipalAccessParams) (*domain.ExternalPrincipalAccess, error)
	Get(ctx context.Context, id string) (*domain.ExternalPrincipalAccess, error)
	GetActiveByPrincipal(ctx context.Context, hostWorkspaceID, principalID string) (*domain.ExternalPrincipalAccess, error)
	List(ctx context.Context, params domain.ListExternalPrincipalAccessParams) ([]domain.ExternalPrincipalAccess, error)
	Update(ctx context.Context, id string, params domain.UpdateExternalPrincipalAccessParams) (*domain.ExternalPrincipalAccess, error)
	Revoke(ctx context.Context, id string, revokedAt time.Time) error
	HasConversationAccess(ctx context.Context, accessID, conversationID string) (bool, error)
	ListConversationIDs(ctx context.Context, accessID string) ([]string, error)
	ReplaceConversationAssignments(ctx context.Context, accessID string, conversationIDs []string, grantedBy string) error
}

type AuthorizationAuditRepository interface {
	WithTx(tx pgx.Tx) AuthorizationAuditRepository
	Create(ctx context.Context, params domain.CreateAuthorizationAuditLogParams) (*domain.AuthorizationAuditLog, error)
	List(ctx context.Context, params domain.ListAuthorizationAuditLogsParams) ([]domain.AuthorizationAuditLog, error)
}

// ConversationRepository defines data access operations for conversations.
type ConversationRepository interface {
	WithTx(tx pgx.Tx) ConversationRepository
	Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error)
	GetCanonicalDM(ctx context.Context, workspaceID, userAID, userBID string) (*domain.Conversation, error)
	Get(ctx context.Context, id string) (*domain.Conversation, error)
	Update(ctx context.Context, id string, params domain.UpdateConversationParams) (*domain.Conversation, error)
	SetTopic(ctx context.Context, id string, params domain.SetTopicParams) (*domain.Conversation, error)
	SetPurpose(ctx context.Context, id string, params domain.SetPurposeParams) (*domain.Conversation, error)
	List(ctx context.Context, params domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error)
	Archive(ctx context.Context, id string) error
	Unarchive(ctx context.Context, id string) error

	AddMember(ctx context.Context, conversationID, userID string) error
	RemoveMember(ctx context.Context, conversationID, userID string) error
	ListMembers(ctx context.Context, conversationID string, cursor string, limit int) (*domain.CursorPage[domain.ConversationMember], error)
	IsMember(ctx context.Context, conversationID, userID string) (bool, error)
}

// MessageRepository defines data access operations for messages.
type MessageRepository interface {
	WithTx(tx pgx.Tx) MessageRepository
	Create(ctx context.Context, params domain.PostMessageParams) (*domain.Message, error)
	GetRow(ctx context.Context, channelID, ts string) (*domain.Message, error)
	Get(ctx context.Context, channelID, ts string) (*domain.Message, error)
	Update(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error)
	Delete(ctx context.Context, channelID, ts string) error
	ListHistory(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error)
	ListReplies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error)

	AddReaction(ctx context.Context, params domain.AddReactionParams) error
	RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error
	GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error)
}

// ConversationReadRepository defines data access operations for per-user conversation read state.
type ConversationReadRepository interface {
	WithTx(tx pgx.Tx) ConversationReadRepository
	Upsert(ctx context.Context, read domain.ConversationRead) error
	Get(ctx context.Context, conversationID, userID string) (*domain.ConversationRead, error)
}

// ProjectorCheckpointRepository stores durable progress for background projectors.
type ProjectorCheckpointRepository interface {
	WithTx(tx pgx.Tx) ProjectorCheckpointRepository
	Get(ctx context.Context, name string) (int64, error)
	Set(ctx context.Context, name string, lastEventID int64) error
}

// UsergroupRepository defines data access operations for usergroups.
type UsergroupRepository interface {
	WithTx(tx pgx.Tx) UsergroupRepository
	Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error)
	Get(ctx context.Context, id string) (*domain.Usergroup, error)
	Update(ctx context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error)
	List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error)
	Enable(ctx context.Context, id string) error
	Disable(ctx context.Context, id string) error
	AddUser(ctx context.Context, usergroupID, userID string) error
	ListUsers(ctx context.Context, usergroupID string) ([]string, error)
	SetUsers(ctx context.Context, usergroupID string, userIDs []string) error
}

// PinRepository defines data access operations for pins.
type PinRepository interface {
	WithTx(tx pgx.Tx) PinRepository
	Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error)
	Remove(ctx context.Context, params domain.PinParams) error
	List(ctx context.Context, params domain.ListPinsParams) ([]domain.Pin, error)
}

// BookmarkRepository defines data access operations for bookmarks.
type BookmarkRepository interface {
	WithTx(tx pgx.Tx) BookmarkRepository
	Create(ctx context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error)
	Get(ctx context.Context, id string) (*domain.Bookmark, error)
	Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, params domain.ListBookmarksParams) ([]domain.Bookmark, error)
}

// FileRepository defines data access operations for files.
type FileRepository interface {
	WithTx(tx pgx.Tx) FileRepository
	Create(ctx context.Context, f *domain.File) error
	Get(ctx context.Context, workspaceID, id string) (*domain.File, error)
	Update(ctx context.Context, workspaceID string, f *domain.File) error
	Delete(ctx context.Context, workspaceID, id string) error
	List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error)
	ShareToChannel(ctx context.Context, workspaceID, fileID, channelID string) error
}

// EventRepository defines data access operations for event subscriptions.
type EventRepository interface {
	WithTx(tx pgx.Tx) EventRepository
	CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error)
	GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error)
	UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error)
	ListSubscriptionsByEvent(ctx context.Context, workspaceID, eventType, resourceType, resourceID string) ([]domain.EventSubscription, error)
}

// AuthRepository defines data access operations for OAuth-backed auth.
type AuthRepository interface {
	WithTx(tx pgx.Tx) AuthRepository
	CreateSession(ctx context.Context, params domain.CreateAuthSessionParams) (*domain.AuthSession, error)
	GetSessionByHash(ctx context.Context, sessionHash string) (*domain.AuthSession, error)
	RevokeSessionByHash(ctx context.Context, sessionHash string) error
	GetOAuthAccount(ctx context.Context, workspaceID string, provider domain.AuthProvider, providerSubject string) (*domain.OAuthAccount, error)
	ListOAuthAccountsBySubject(ctx context.Context, provider domain.AuthProvider, providerSubject string) ([]domain.OAuthAccount, error)
	UpsertOAuthAccount(ctx context.Context, params domain.UpsertOAuthAccountParams) (*domain.OAuthAccount, error)
}

type MCPOAuthRepository interface {
	WithTx(tx pgx.Tx) MCPOAuthRepository
	CreateAuthorizationCode(ctx context.Context, params domain.CreateMCPOAuthAuthorizationCodeParams) (*domain.MCPOAuthAuthorizationCode, string, error)
	GetAuthorizationCodeByHash(ctx context.Context, codeHash string) (*domain.MCPOAuthAuthorizationCode, error)
	MarkAuthorizationCodeUsed(ctx context.Context, id string, usedAt time.Time) error
	CreateRefreshToken(ctx context.Context, params domain.CreateMCPOAuthRefreshTokenParams) (*domain.MCPOAuthRefreshToken, string, error)
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.MCPOAuthRefreshToken, error)
	RotateRefreshToken(ctx context.Context, oldID, newID string, revokedAt time.Time) error
	RevokeRefreshToken(ctx context.Context, id string, revokedAt time.Time) error
}

// APIKeyRepository defines data access operations for API keys.
type APIKeyRepository interface {
	WithTx(tx pgx.Tx) APIKeyRepository
	Create(ctx context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error)
	Get(ctx context.Context, id string) (*domain.APIKey, error)
	GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error)
	List(ctx context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error)
	Update(ctx context.Context, id string, params domain.UpdateAPIKeyParams) (*domain.APIKey, error)
	Revoke(ctx context.Context, id string) error
	SetRotated(ctx context.Context, oldKeyID, newKeyID string, gracePeriodEndsAt time.Time) error
	UpdateUsage(ctx context.Context, id string) error
}

// InternalEventStoreRepository defines data access for the internal event store.
type InternalEventStoreRepository interface {
	WithTx(tx pgx.Tx) InternalEventStoreRepository
	// Append writes an internal event to the event store (pure INSERT).
	// Webhook fan-out is handled by WebhookProducer via S3 queue.
	Append(ctx context.Context, event domain.InternalEvent) (*domain.InternalEvent, error)
	// GetByAggregate returns all events for an aggregate ordered by ID.
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.InternalEvent, error)
	// GetAllSince returns events since a given ID for incremental projection rebuilds.
	GetAllSince(ctx context.Context, sinceID int64, limit int) ([]domain.InternalEvent, error)
	// GetAllSinceByShard returns events for one shard since a given shard-local checkpoint.
	GetAllSinceByShard(ctx context.Context, shardID int, sinceID int64, limit int) ([]domain.InternalEvent, error)
}

type ExternalEventRepository interface {
	WithTx(tx pgx.Tx) ExternalEventRepository
	Insert(ctx context.Context, event domain.ExternalEvent) (*domain.ExternalEvent, error)
	RecordProjectionFailure(ctx context.Context, internalEventID int64, message string) error
	ListVisible(ctx context.Context, principal ExternalEventPrincipal, params domain.ListExternalEventsParams) (*domain.CursorPage[domain.ExternalEvent], error)
	GetSince(ctx context.Context, afterID int64, limit int) ([]domain.ExternalEvent, error)
	Rebuild(ctx context.Context, events []domain.ExternalEvent) error
	RebuildFeeds(ctx context.Context) error
}

type ExternalEventPrincipal struct {
	WorkspaceID string
	UserID      string
	APIKeyID    string
	Permissions []string
}
