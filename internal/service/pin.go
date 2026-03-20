package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// PinService contains business logic for pin operations.
type PinService struct {
	repo     repository.PinRepository
	convRepo repository.ConversationRepository
	msgRepo  repository.MessageRepository
	recorder EventRecorder
	logger   *slog.Logger
}

// NewPinService creates a new PinService.
func NewPinService(repo repository.PinRepository, convRepo repository.ConversationRepository, msgRepo repository.MessageRepository, recorder EventRecorder, logger *slog.Logger) *PinService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &PinService{repo: repo, convRepo: convRepo, msgRepo: msgRepo, recorder: recorder, logger: logger}
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

	pin, err := s.repo.Add(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(pin)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventPinAdded,
		AggregateType: domain.AggregatePin,
		AggregateID:   pin.ChannelID + ":" + pin.MessageTS,
		TeamID:        conv.TeamID,
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record pin.added event", "error", recErr)
	}
	return pin, nil
}

func (s *PinService) Remove(ctx context.Context, params domain.PinParams) error {
	if params.ChannelID == "" || params.MessageTS == "" {
		return fmt.Errorf("channel and timestamp: %w", domain.ErrInvalidArgument)
	}
	if err := s.repo.Remove(ctx, params); err != nil {
		return err
	}
	payload, _ := json.Marshal(params)
	if recErr := s.recorder.Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventPinRemoved,
		AggregateType: domain.AggregatePin,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		TeamID:        "",
		Payload:       payload,
	}); recErr != nil {
		s.logger.Warn("record pin.removed event", "error", recErr)
	}
	return nil
}

func (s *PinService) List(ctx context.Context, channelID string) ([]domain.Pin, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, domain.ListPinsParams{ChannelID: channelID})
}
