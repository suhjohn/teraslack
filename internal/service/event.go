package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// EventService contains business logic for event operations.
type EventService struct {
	repo       repository.EventRepository
	httpClient *http.Client
	logger     *slog.Logger
}

// NewEventService creates a new EventService.
func NewEventService(repo repository.EventRepository, logger *slog.Logger) *EventService {
	return &EventService{
		repo: repo,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: logger,
	}
}

// Publish creates an event and dispatches it to all matching subscriptions.
func (s *EventService) Publish(ctx context.Context, teamID, eventType string, payload any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	eventID := fmt.Sprintf("Ev%d", time.Now().UnixNano())
	event := &domain.Event{
		ID:        eventID,
		Type:      eventType,
		TeamID:    teamID,
		Payload:   json.RawMessage(payloadBytes),
		CreatedAt: time.Now(),
	}

	if err := s.repo.CreateEvent(ctx, event); err != nil {
		return fmt.Errorf("create event: %w", err)
	}

	// Dispatch to subscribers asynchronously
	go s.dispatch(teamID, eventType, event)

	return nil
}

func (s *EventService) dispatch(teamID, eventType string, event *domain.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	subs, err := s.repo.ListSubscriptionsByTeamAndEvent(ctx, teamID, eventType)
	if err != nil {
		s.logger.Error("list subscribers", "error", err, "team_id", teamID, "event_type", eventType)
		return
	}

	for _, sub := range subs {
		s.deliverWebhook(ctx, sub, event)
	}
}

func (s *EventService) deliverWebhook(ctx context.Context, sub domain.EventSubscription, event *domain.Event) {
	wrapper := map[string]any{
		"type":       "event_callback",
		"team_id":    event.TeamID,
		"event_id":   event.ID,
		"event_time": event.CreatedAt.Unix(),
		"event": map[string]any{
			"type":    event.Type,
			"payload": json.RawMessage(event.Payload),
		},
	}

	body, err := json.Marshal(wrapper)
	if err != nil {
		s.logger.Error("marshal webhook", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(body))
	if err != nil {
		s.logger.Error("create webhook request", "error", err, "url", sub.URL)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Sign the request if a secret is configured
	if sub.Secret != "" {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
		mac := hmac.New(sha256.New, []byte(sub.Secret))
		mac.Write([]byte(sigBase))
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Slack-Signature", sig)
		req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error("deliver webhook", "error", err, "url", sub.URL)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("webhook non-200", "status", resp.StatusCode, "url", sub.URL)
	}
}

func (s *EventService) CreateSubscription(ctx context.Context, params domain.CreateEventSubscriptionParams) (*domain.EventSubscription, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.URL == "" {
		return nil, fmt.Errorf("url: %w", domain.ErrInvalidArgument)
	}
	if len(params.EventTypes) == 0 {
		return nil, fmt.Errorf("event_types: %w", domain.ErrInvalidArgument)
	}
	return s.repo.CreateSubscription(ctx, params)
}

func (s *EventService) GetSubscription(ctx context.Context, id string) (*domain.EventSubscription, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.GetSubscription(ctx, id)
}

func (s *EventService) UpdateSubscription(ctx context.Context, id string, params domain.UpdateEventSubscriptionParams) (*domain.EventSubscription, error) {
	if id == "" {
		return nil, fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.UpdateSubscription(ctx, id, params)
}

func (s *EventService) DeleteSubscription(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.DeleteSubscription(ctx, id)
}

func (s *EventService) ListSubscriptions(ctx context.Context, params domain.ListEventSubscriptionsParams) ([]domain.EventSubscription, error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.ListSubscriptions(ctx, params)
}
