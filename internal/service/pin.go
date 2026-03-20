package service

import (
	"context"
	"fmt"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// PinService contains business logic for pin operations.
type PinService struct {
	repo     repository.PinRepository
	convRepo repository.ConversationRepository
	msgRepo  repository.MessageRepository
}

// NewPinService creates a new PinService.
func NewPinService(repo repository.PinRepository, convRepo repository.ConversationRepository, msgRepo repository.MessageRepository) *PinService {
	return &PinService{repo: repo, convRepo: convRepo, msgRepo: msgRepo}
}

func (s *PinService) Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error) {
	if params.ChannelID == "" || params.MessageTS == "" {
		return nil, fmt.Errorf("channel and timestamp: %w", domain.ErrInvalidArgument)
	}

	// Verify channel exists and is not archived
	conv, err := s.convRepo.Get(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}
	if conv.IsArchived {
		return nil, domain.ErrChannelArchived
	}

	// Verify message exists
	if _, err := s.msgRepo.Get(ctx, params.ChannelID, params.MessageTS); err != nil {
		return nil, fmt.Errorf("message: %w", err)
	}

	return s.repo.Add(ctx, params)
}

func (s *PinService) Remove(ctx context.Context, params domain.PinParams) error {
	if params.ChannelID == "" || params.MessageTS == "" {
		return fmt.Errorf("channel and timestamp: %w", domain.ErrInvalidArgument)
	}
	return s.repo.Remove(ctx, params)
}

func (s *PinService) List(ctx context.Context, channelID string) ([]domain.Pin, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, domain.ListPinsParams{ChannelID: channelID})
}
