package repository

import (
	"context"

	"github.com/suhjohn/agentslack/internal/domain"
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
