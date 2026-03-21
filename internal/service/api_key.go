package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/suhjohn/workspace/internal/crypto"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// APIKeyService contains business logic for API key operations.
type APIKeyService struct {
	repo     repository.APIKeyRepository
	userRepo repository.UserRepository
	recorder EventRecorder
	db       repository.TxBeginner
	logger   *slog.Logger
}

// NewAPIKeyService creates a new APIKeyService.
func NewAPIKeyService(repo repository.APIKeyRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *APIKeyService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &APIKeyService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

// Create creates a new API key and returns the raw key (only available at creation time).
func (s *APIKeyService) Create(ctx context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error) {
	if params.Name == "" {
		return nil, "", fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	if params.TeamID == "" {
		return nil, "", fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	if params.PrincipalID == "" {
		return nil, "", fmt.Errorf("principal_id: %w", domain.ErrInvalidArgument)
	}

	// Verify the principal exists
	if _, err := s.userRepo.Get(ctx, params.PrincipalID); err != nil {
		return nil, "", fmt.Errorf("principal: %w", err)
	}

	// If on_behalf_of is set, verify that principal exists too
	if params.OnBehalfOf != "" {
		if _, err := s.userRepo.Get(ctx, params.OnBehalfOf); err != nil {
			return nil, "", fmt.Errorf("on_behalf_of principal: %w", err)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	key, rawKey, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, "", err
	}

	payload, _ := json.Marshal(key.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventAPIKeyCreated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   key.ID,
		TeamID:        key.TeamID,
		Payload:       payload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}
	return key, rawKey, nil
}

// Get retrieves an API key by ID (without the raw key).
func (s *APIKeyService) Get(ctx context.Context, id string) (*domain.APIKey, error) {
	key, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	key.KeyHash = ""
	return key, nil
}

// List retrieves API keys with pagination and filtering.
func (s *APIKeyService) List(ctx context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error) {
	if params.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}
	return s.repo.List(ctx, params)
}

// Revoke revokes an API key. The key is marked as revoked but not deleted.
func (s *APIKeyService) Revoke(ctx context.Context, id string) error {
	key, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Revoke(ctx, id); err != nil {
		return err
	}

	// Re-fetch after revoke so event payload reflects post-mutation state (revoked_at set)
	key, err = s.repo.WithTx(tx).Get(ctx, id)
	if err != nil {
		return fmt.Errorf("re-fetch revoked key: %w", err)
	}

	payload, _ := json.Marshal(key.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventAPIKeyRevoked,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   key.ID,
		TeamID:        key.TeamID,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record api_key.revoked event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// Update modifies an API key's name, description, or permissions.
func (s *APIKeyService) Update(ctx context.Context, id string, params domain.UpdateAPIKeyParams) (*domain.APIKey, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	key, err := s.repo.WithTx(tx).Update(ctx, id, params)
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(key.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventAPIKeyUpdated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   key.ID,
		TeamID:        key.TeamID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record api_key.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return key, nil
}

// Rotate creates a new API key and marks the old one as rotated with a grace period.
// During the grace period, both the old and new keys are valid.
func (s *APIKeyService) Rotate(ctx context.Context, id string, params domain.RotateAPIKeyParams) (*domain.APIKey, string, error) {
	oldKey, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}

	if oldKey.Revoked {
		return nil, "", fmt.Errorf("key is revoked: %w", domain.ErrInvalidArgument)
	}

	gracePeriod := 24 * time.Hour
	if params.GracePeriod != "" {
		gracePeriod, err = parseDuration(params.GracePeriod)
		if err != nil {
			return nil, "", fmt.Errorf("parse grace_period: %w", err)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)

	// Create the new key with the same properties as the old one
	newKey, rawKey, err := txRepo.Create(ctx, domain.CreateAPIKeyParams{
		Name:        oldKey.Name + " (rotated)",
		Description: oldKey.Description,
		TeamID:      oldKey.TeamID,
		PrincipalID: oldKey.PrincipalID,
		CreatedBy:   oldKey.CreatedBy,
		OnBehalfOf:  oldKey.OnBehalfOf,
		Type:        oldKey.Type,
		Environment: oldKey.Environment,
		Permissions: oldKey.Permissions,
	})
	if err != nil {
		return nil, "", fmt.Errorf("create rotated key: %w", err)
	}

	// Record creation event for the new key (required for projection rebuild)
	newKeyPayload, _ := json.Marshal(newKey.Redacted())
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventAPIKeyCreated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   newKey.ID,
		TeamID:        newKey.TeamID,
		Payload:       newKeyPayload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.created event for rotated key: %w", err)
	}

	gracePeriodEndsAt := time.Now().Add(gracePeriod)
	if err := txRepo.SetRotated(ctx, oldKey.ID, newKey.ID, gracePeriodEndsAt); err != nil {
		return nil, "", fmt.Errorf("set rotated: %w", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"old_key_id":            oldKey.ID,
		"new_key_id":            newKey.ID,
		"grace_period_ends_at":  gracePeriodEndsAt,
	})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.ServiceEvent{
		EventType:     domain.EventAPIKeyRotated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   oldKey.ID,
		TeamID:        oldKey.TeamID,
		Payload:       payload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.rotated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}
	return newKey, rawKey, nil
}

// ValidateAPIKey validates a raw API key string and returns validation info.
func (s *APIKeyService) ValidateAPIKey(ctx context.Context, rawKey string) (*domain.APIKeyValidation, error) {
	keyHash := crypto.HashToken(rawKey)

	key, err := s.repo.GetByHash(ctx, keyHash)
	if err != nil {
		return nil, domain.ErrInvalidAuth
	}

	if key.Revoked {
		// Check if we're still in the grace period (rotated key)
		if key.GracePeriodEndsAt != nil && key.GracePeriodEndsAt.After(time.Now()) {
			// Still valid during grace period
		} else {
			return nil, domain.ErrTokenRevoked
		}
	}

	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return nil, domain.ErrTokenRevoked
	}

	// Update usage asynchronously (fire-and-forget, non-critical)
	go func() {
		if err := s.repo.UpdateUsage(context.Background(), key.ID); err != nil {
			s.logger.Warn("failed to update API key usage", "key_id", key.ID, "error", err)
		}
	}()

	return &domain.APIKeyValidation{
		TeamID:      key.TeamID,
		PrincipalID: key.PrincipalID,
		OnBehalfOf:  key.OnBehalfOf,
		KeyID:       key.ID,
		Permissions: key.Permissions,
		Environment: key.Environment,
	}, nil
}

// parseDuration parses a human-friendly duration (supports "Nd" for days).
func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}
