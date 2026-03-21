package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/workspace/internal/domain"
)

// TxBeginner abstracts the ability to begin a database transaction.
// Satisfied by *pgxpool.Pool.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// UserRepository defines data access operations for users.
type UserRepository interface {
	WithTx(tx pgx.Tx) UserRepository
	Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error)
	Get(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error)
	List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error)
}

// ConversationRepository defines data access operations for conversations.
type ConversationRepository interface {
	WithTx(tx pgx.Tx) ConversationRepository
	Create(ctx context.Context, params domain.CreateConversationParams) (*domain.Conversation, error)
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
	Get(ctx context.Context, channelID, ts string) (*domain.Message, error)
	Update(ctx context.Context, channelID, ts string, params domain.UpdateMessageParams) (*domain.Message, error)
	Delete(ctx context.Context, channelID, ts string) error
	ListHistory(ctx context.Context, params domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error)
	ListReplies(ctx context.Context, params domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error)

	AddReaction(ctx context.Context, params domain.AddReactionParams) error
	RemoveReaction(ctx context.Context, params domain.RemoveReactionParams) error
	GetReactions(ctx context.Context, channelID, messageTS string) ([]domain.Reaction, error)
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
	Get(ctx context.Context, id string) (*domain.File, error)
	Update(ctx context.Context, f *domain.File) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error)
	ShareToChannel(ctx context.Context, fileID, channelID string) error
}

// EventRepository defines data access operations for event subscriptions.
type EventRepository interface {
	WithTx(tx pgx.Tx) EventRepository
	CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error)
	GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error)
	UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error)
	ListSubscriptionsByTeamAndEvent(ctx context.Context, teamID, eventType string) ([]domain.EventSubscription, error)
}

// AuthRepository defines data access operations for authentication tokens.
type AuthRepository interface {
	WithTx(tx pgx.Tx) AuthRepository
	CreateToken(ctx context.Context, params domain.CreateTokenParams) (*domain.Token, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.Token, error)
	RevokeToken(ctx context.Context, token string) error
}

// EventStoreRepository defines data access for the service-level event store.
type EventStoreRepository interface {
	WithTx(tx pgx.Tx) EventStoreRepository
	// Append writes a service event and creates outbox entries for matching subscriptions atomically.
	Append(ctx context.Context, event domain.ServiceEvent) (*domain.ServiceEvent, error)
	// GetByAggregate returns all events for an aggregate ordered by ID.
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.ServiceEvent, error)
	// GetAllSince returns events since a given ID for incremental projection rebuilds.
	GetAllSince(ctx context.Context, sinceID int64, limit int) ([]domain.ServiceEvent, error)
}

// OutboxRepository defines data access for the transactional outbox.
type OutboxRepository interface {
	// ClaimBatch claims up to `limit` pending outbox entries using FOR UPDATE SKIP LOCKED.
	ClaimBatch(ctx context.Context, limit int) ([]domain.OutboxEntry, error)
	// MarkDelivered marks an outbox entry as successfully delivered.
	MarkDelivered(ctx context.Context, id int64) error
	// MarkFailed marks an outbox entry as permanently failed.
	MarkFailed(ctx context.Context, id int64, lastError string) error
	// ScheduleRetry schedules an outbox entry for retry at a future time.
	ScheduleRetry(ctx context.Context, id int64, nextAttemptAt time.Time, lastError string) error
	// CleanupDelivered removes delivered outbox entries older than the given duration.
	CleanupDelivered(ctx context.Context, olderThan time.Duration) (int64, error)
}
