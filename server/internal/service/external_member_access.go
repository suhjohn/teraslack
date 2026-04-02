package service

import (
	"context"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func activeExternalMember(ctx context.Context, repo repository.ExternalMemberRepository, conversationID string) (*domain.ExternalMember, error) {
	if repo == nil || conversationID == "" {
		return nil, nil
	}
	accountID := ctxutil.GetAccountID(ctx)
	if accountID == "" {
		return nil, nil
	}
	member, err := repo.GetActiveByConversationAndAccount(ctx, conversationID, accountID)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return member, nil
}

func ensureExternalMemberConversationAccess(ctx context.Context, repo repository.ExternalMemberRepository, conv *domain.Conversation, capability string, requireWrite bool) (bool, error) {
	if conv == nil {
		return false, domain.ErrNotFound
	}
	member, err := activeExternalMember(ctx, repo, conv.ID)
	if err != nil {
		return false, err
	}
	if member == nil {
		return false, nil
	}
	if conv.Type == domain.ConversationTypeIM || conv.Type == domain.ConversationTypeMPIM {
		return true, domain.ErrForbidden
	}
	if capability != "" && !capabilityAllowed(member.AllowedCapabilities, capability) {
		return true, domain.ErrForbidden
	}
	if requireWrite && member.AccessMode == domain.ExternalPrincipalAccessModeSharedReadOnly {
		return true, domain.ErrForbidden
	}
	return true, nil
}

func ensureConversationAccess(ctx context.Context, externalMembers repository.ExternalMemberRepository, conv *domain.Conversation, capability string, requireWrite bool) (bool, error) {
	if hasWorkspaceMembershipContext(ctx, conv.WorkspaceID) {
		if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
			return false, err
		}
		return false, nil
	}
	authorized, err := ensureExternalMemberConversationAccess(ctx, externalMembers, conv, capability, requireWrite)
	if authorized || err != nil {
		return authorized, err
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return false, err
	}
	return false, nil
}

func isConversationExternalActor(ctx context.Context, externalMembers repository.ExternalMemberRepository, conv *domain.Conversation) (bool, error) {
	if conv == nil {
		return false, nil
	}
	if hasWorkspaceMembershipContext(ctx, conv.WorkspaceID) {
		return false, nil
	}
	if member, err := activeExternalMember(ctx, externalMembers, conv.ID); err != nil {
		return false, err
	} else if member != nil {
		return true, nil
	}
	return false, nil
}

func filterExternalSharedConversations(ctx context.Context, externalMembers repository.ExternalMemberRepository, conversations []domain.Conversation) ([]domain.Conversation, error) {
	if ctxutil.GetAccountID(ctx) == "" || externalMembers == nil || hasWorkspaceMembershipContext(ctx, conversationsWorkspaceID(conversations)) {
		return conversations, nil
	}
	filtered := make([]domain.Conversation, 0, len(conversations))
	for _, conv := range conversations {
		member, err := externalMembers.GetActiveByConversationAndAccount(ctx, conv.ID, ctxutil.GetAccountID(ctx))
		if err != nil && err != domain.ErrNotFound {
			return nil, err
		}
		if member != nil {
			filtered = append(filtered, conv)
		}
	}
	return filtered, nil
}

func conversationsWorkspaceID(conversations []domain.Conversation) string {
	if len(conversations) == 0 {
		return ""
	}
	return conversations[0].WorkspaceID
}

func ensureExternalMemberFileAccess(ctx context.Context, repo repository.ExternalMemberRepository, f *domain.File, capability string, requireWrite bool) (bool, error) {
	if repo == nil || f == nil {
		return false, nil
	}
	accountID := ctxutil.GetAccountID(ctx)
	if accountID == "" {
		return false, nil
	}
	found := false
	for _, channelID := range f.Channels {
		member, err := repo.GetActiveByConversationAndAccount(ctx, channelID, accountID)
		if err != nil {
			if err == domain.ErrNotFound {
				continue
			}
			return false, err
		}
		found = true
		if capability != "" && !capabilityAllowed(member.AllowedCapabilities, capability) {
			continue
		}
		if requireWrite && member.AccessMode == domain.ExternalPrincipalAccessModeSharedReadOnly {
			continue
		}
		return true, nil
	}
	if found {
		return true, domain.ErrForbidden
	}
	return false, nil
}

func ensureFileAccess(ctx context.Context, externalMembers repository.ExternalMemberRepository, f *domain.File, capability string, requireWrite bool) (bool, error) {
	if hasWorkspaceMembershipContext(ctx, f.WorkspaceID) {
		if err := ensureWorkspaceAccess(ctx, f.WorkspaceID); err != nil {
			return false, err
		}
		return false, nil
	}
	authorized, err := ensureExternalMemberFileAccess(ctx, externalMembers, f, capability, requireWrite)
	if authorized || err != nil {
		return authorized, err
	}
	if err := ensureWorkspaceAccess(ctx, f.WorkspaceID); err != nil {
		return false, err
	}
	return false, nil
}

func capabilityAllowed(allowed []string, required string) bool {
	if required == "" {
		return true
	}
	for _, candidate := range allowed {
		if candidate == "*" || candidate == required {
			return true
		}
		if strings.HasSuffix(candidate, ".*") {
			prefix := strings.TrimSuffix(candidate, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
	}
	return false
}
