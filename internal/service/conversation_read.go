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
	userID := ctxutil.GetUserID(ctx)
	if userID == "" {
		return fmt.Errorf("user_id: %w", domain.ErrInvalidAuth)
	}
	params.UserID = userID

	conv, err := s.convRepo.Get(ctx, params.ConversationID)
	if err != nil {
		return err
	}
	if err := ensureTeamAccess(ctx, conv.TeamID); err != nil {
		return err
	}
	isMember, err := s.convRepo.IsMember(ctx, params.ConversationID, userID)
	if err != nil {
		return err
	}
	if !isMember {
		return domain.ErrForbidden
	}

	return s.repo.Upsert(ctx, domain.ConversationRead{
		TeamID:         conv.TeamID,
		ConversationID: params.ConversationID,
		UserID:         userID,
		LastReadTS:     params.LastReadTS,
		LastReadAt:     time.Now(),
	})
}
