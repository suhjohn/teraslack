package service

import (
	"context"
	"fmt"
	"time"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ConversationReadService struct {
	repo     repository.ConversationReadRepository
	convRepo repository.ConversationRepository
}

func NewConversationReadService(repo repository.ConversationReadRepository, convRepo repository.ConversationRepository) *ConversationReadService {
	return &ConversationReadService{repo: repo, convRepo: convRepo}
}

func (s *ConversationReadService) MarkRead(ctx context.Context, params domain.MarkConversationReadParams) error {
	if err := requirePermission(ctx, domain.PermissionMessagesRead); err != nil {
		return err
	}
	if params.ConversationID == "" || params.LastReadTS == "" {
		return fmt.Errorf("conversation_id and last_read_ts: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, params.ConversationID)
	if err != nil {
		return err
	}
	accountID := actorAccountID(ctx)
	if accountID == "" {
		if ctxutil.GetPrincipalType(ctx) == domain.PrincipalTypeSystem {
			return nil
		}
		return fmt.Errorf("account_id: %w", domain.ErrInvalidAuth)
	}
	if isAccountOwnedConversation(conv) {
		if err := ensureAccountConversationAccess(ctx, s.convRepo, conv); err != nil {
			return err
		}
		return s.repo.UpsertByAccount(ctx, params.ConversationID, accountID, params.LastReadTS, time.Now())
	}

	if err := ensureWorkspaceAccess(ctx, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	switch conv.Type {
	case domain.ConversationTypePrivateChannel, domain.ConversationTypeIM, domain.ConversationTypeMPIM:
		isMember, err := s.convRepo.IsAccountMember(ctx, params.ConversationID, accountID)
		if err != nil {
			return err
		}
		if !isMember {
			return domain.ErrForbidden
		}
	}

	return s.repo.UpsertByAccount(ctx, params.ConversationID, accountID, params.LastReadTS, time.Now())
}
