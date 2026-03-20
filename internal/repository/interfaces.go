package repository

import (
	"context"

	"github.com/suhjohn/workspace/internal/domain"
)

// UserRepository defines data access operations for users.
type UserRepository interface {
	Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error)
	Get(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error)
	List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error)
}

// ConversationRepository defines data access operations for conversations.
type ConversationRepository interface {
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
	Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error)
	Remove(ctx context.Context, params domain.PinParams) error
	List(ctx context.Context, params domain.ListPinsParams) ([]domain.Pin, error)
}

// BookmarkRepository defines data access operations for bookmarks.
type BookmarkRepository interface {
	Create(ctx context.Context, params domain.CreateBookmarkParams) (*domain.Bookmark, error)
	Get(ctx context.Context, id string) (*domain.Bookmark, error)
	Update(ctx context.Context, id string, params domain.UpdateBookmarkParams) (*domain.Bookmark, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, params domain.ListBookmarksParams) ([]domain.Bookmark, error)
}

// FileRepository defines data access operations for files.
type FileRepository interface {
	Create(ctx context.Context, f *domain.File) error
	Get(ctx context.Context, id string) (*domain.File, error)
	Update(ctx context.Context, f *domain.File) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error)
	ShareToChannel(ctx context.Context, fileID, channelID string) error
}

// EventRepository defines data access operations for events and subscriptions.
type EventRepository interface {
	CreateEvent(ctx context.Context, event *domain.Event) error
	CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error)
	GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error)
	UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error)
	ListSubscriptionsByTeamAndEvent(ctx context.Context, teamID, eventType string) ([]domain.EventSubscription, error)
}

// AuthRepository defines data access operations for authentication tokens.
type AuthRepository interface {
	CreateToken(ctx context.Context, params domain.CreateTokenParams) (*domain.Token, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.Token, error)
	RevokeToken(ctx context.Context, token string) error
}

// EventLogRepository defines data access operations for the append-only event log.
type EventLogRepository interface {
	Append(ctx context.Context, entry domain.EventLogEntry) (*domain.EventLogEntry, error)
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.EventLogEntry, error)
	GetByAggregateSince(ctx context.Context, aggregateType, aggregateID string, sinceSequenceID int64) ([]domain.EventLogEntry, error)
	GetByType(ctx context.Context, eventType string, limit int) ([]domain.EventLogEntry, error)
	GetAllSince(ctx context.Context, sinceSequenceID int64, limit int) ([]domain.EventLogEntry, error)
}
