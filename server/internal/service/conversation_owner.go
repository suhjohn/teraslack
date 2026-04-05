package service

import (
	"context"
	"errors"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func actorAccountID(ctx context.Context) string {
	return strings.TrimSpace(ctxutil.GetAccountID(ctx))
}

func resolveUserAccountID(ctx context.Context, userRepo repository.UserRepository, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || userRepo == nil {
		return "", nil
	}
	user, err := userRepo.Get(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(user.AccountID), nil
}

func resolveActorAccountID(ctx context.Context, userRepo repository.UserRepository) (string, error) {
	if accountID := actorAccountID(ctx); accountID != "" {
		return accountID, nil
	}
	return resolveUserAccountID(ctx, userRepo, actorUserID(ctx))
}

func normalizedConversationOwnerType(conv *domain.Conversation) domain.ConversationOwnerType {
	if conv == nil {
		return ""
	}
	if conv.OwnerType != "" {
		return conv.OwnerType
	}
	if conv.OwnerAccountID != "" && conv.OwnerWorkspaceID == "" && conv.WorkspaceID == "" {
		return domain.ConversationOwnerTypeAccount
	}
	return domain.ConversationOwnerTypeWorkspace
}

func conversationWorkspaceID(conv *domain.Conversation) string {
	if conv == nil {
		return ""
	}
	if conv.OwnerWorkspaceID != "" {
		return conv.OwnerWorkspaceID
	}
	return conv.WorkspaceID
}

func isAccountOwnedConversation(conv *domain.Conversation) bool {
	return normalizedConversationOwnerType(conv) == domain.ConversationOwnerTypeAccount
}

func isWorkspaceOwnedConversation(conv *domain.Conversation) bool {
	return normalizedConversationOwnerType(conv) == domain.ConversationOwnerTypeWorkspace
}

func isConversationOwnerAccount(ctx context.Context, conv *domain.Conversation) bool {
	if conv == nil || conv.OwnerAccountID == "" {
		return false
	}
	return actorAccountID(ctx) == conv.OwnerAccountID
}

func ensureAccountConversationAccess(ctx context.Context, repo repository.ConversationRepository, conv *domain.Conversation) error {
	if conv == nil {
		return domain.ErrNotFound
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	accountID := actorAccountID(ctx)
	if accountID == "" {
		return domain.ErrForbidden
	}
	if conv.OwnerAccountID == accountID {
		return nil
	}
	if repo == nil {
		return domain.ErrForbidden
	}
	isMember, err := repo.IsAccountMember(ctx, conv.ID, accountID)
	if err != nil {
		return err
	}
	if !isMember {
		return domain.ErrForbidden
	}
	return nil
}
