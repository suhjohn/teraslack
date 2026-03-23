package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// APIKeyService contains business logic for API key operations.
type APIKeyService struct {
	repo           repository.APIKeyRepository
	userRepo       repository.UserRepository
	externalAccess repository.ExternalPrincipalAccessRepository
	auditRepo      repository.AuthorizationAuditRepository
	recorder       EventRecorder
	db             repository.TxBeginner
	logger         *slog.Logger
}

// NewAPIKeyService creates a new APIKeyService.
func NewAPIKeyService(repo repository.APIKeyRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *APIKeyService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &APIKeyService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

func (s *APIKeyService) SetExternalAccessRepository(repo repository.ExternalPrincipalAccessRepository) {
	s.externalAccess = repo
}

func (s *APIKeyService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

// Create creates a new API key and returns the raw key (only available at creation time).
func (s *APIKeyService) Create(ctx context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error) {
	if err := requirePermission(ctx, domain.PermissionAPIKeysCreate); err != nil {
		return nil, "", err
	}
	if params.Name == "" {
		return nil, "", fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, "", err
	}
	params.TeamID = teamID
	if params.PrincipalID == "" {
		return nil, "", fmt.Errorf("principal_id: %w", domain.ErrInvalidArgument)
	}

	// Verify the principal exists
	principal, err := s.userRepo.Get(ctx, params.PrincipalID)
	if err != nil {
		return nil, "", fmt.Errorf("principal: %w", err)
	}
	actor, err := s.authorizePrincipalKeyAccess(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if params.CreatedBy == "" && actor != nil {
		params.CreatedBy = actor.ID
	}
	if actor != nil {
		if err := validateAPIKeyPermissions(principal, params.Permissions); err != nil {
			return nil, "", err
		}
		if err := s.validateExternalAccessPermissions(ctx, params.TeamID, principal, params.Permissions); err != nil {
			return nil, "", err
		}
	}

	// If on_behalf_of is set, verify that principal exists too
	if params.OnBehalfOf != "" {
		onBehalfOf, err := s.userRepo.Get(ctx, params.OnBehalfOf)
		if err != nil {
			return nil, "", fmt.Errorf("on_behalf_of principal: %w", err)
		}
		if err := s.authorizeDelegatedKeyIssuance(ctx, actor, principal, onBehalfOf); err != nil {
			return nil, "", err
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
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventAPIKeyCreated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   key.ID,
		TeamID:        key.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, key.TeamID, domain.AuditActionAPIKeyCreated, "api_key", key.ID, map[string]any{
		"principal_id": key.PrincipalID,
		"permissions":  key.Permissions,
	}); err != nil {
		return nil, "", fmt.Errorf("record authorization audit log: %w", err)
	}
	return key, rawKey, nil
}

// Get retrieves an API key by ID (without the raw key).
func (s *APIKeyService) Get(ctx context.Context, id string) (*domain.APIKey, error) {
	key, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, key.TeamID); err != nil {
		return nil, err
	}
	if _, err := s.authorizePrincipalKeyID(ctx, key.PrincipalID); err != nil {
		return nil, err
	}
	key.KeyHash = ""
	return key, nil
}

// List retrieves API keys with pagination and filtering.
func (s *APIKeyService) List(ctx context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error) {
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	if requiresAuthenticatedActor(ctx) {
		actor, err := loadActingUser(ctx, s.userRepo)
		if err != nil {
			return nil, err
		}
		if !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
			params.PrincipalID = actor.ID
		} else if params.PrincipalID != "" {
			if _, err := s.authorizePrincipalKeyID(ctx, params.PrincipalID); err != nil {
				return nil, err
			}
		}
	}
	return s.repo.List(ctx, params)
}

// Revoke revokes an API key. The key is marked as revoked but not deleted.
func (s *APIKeyService) Revoke(ctx context.Context, id string) error {
	key, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureTeamAccess(ctx, key.TeamID); err != nil {
		return err
	}
	if _, err := s.authorizePrincipalKeyID(ctx, key.PrincipalID); err != nil {
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
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventAPIKeyRevoked,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   key.ID,
		TeamID:        key.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record api_key.revoked event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, key.TeamID, domain.AuditActionAPIKeyRevoked, "api_key", key.ID, map[string]any{
		"principal_id": key.PrincipalID,
	}); err != nil {
		return fmt.Errorf("record authorization audit log: %w", err)
	}
	return nil
}

// Update modifies an API key's name, description, or permissions.
func (s *APIKeyService) Update(ctx context.Context, id string, params domain.UpdateAPIKeyParams) (*domain.APIKey, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, existing.TeamID); err != nil {
		return nil, err
	}
	principal, err := s.authorizePrincipalKeyID(ctx, existing.PrincipalID)
	if err != nil {
		return nil, err
	}
	if params.Permissions != nil && !isInternalCallWithoutAuth(ctx) {
		if err := validateAPIKeyPermissions(principal, *params.Permissions); err != nil {
			return nil, err
		}
		if err := s.validateExternalAccessPermissions(ctx, existing.TeamID, principal, *params.Permissions); err != nil {
			return nil, err
		}
	}

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
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventAPIKeyUpdated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   key.ID,
		TeamID:        key.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record api_key.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, key.TeamID, domain.AuditActionAPIKeyUpdated, "api_key", key.ID, map[string]any{
		"principal_id": key.PrincipalID,
		"permissions":  key.Permissions,
	}); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	key.KeyHash = ""
	return key, nil
}

// Rotate creates a new API key and marks the old one as rotated with a grace period.
// During the grace period, both the old and new keys are valid.
func (s *APIKeyService) Rotate(ctx context.Context, id string, params domain.RotateAPIKeyParams) (*domain.APIKey, string, error) {
	gracePeriod := 24 * time.Hour
	if params.GracePeriod != "" {
		var err error
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

	// Read old key inside the transaction to prevent TOCTOU race with concurrent Revoke
	oldKey, err := txRepo.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if err := ensureTeamAccess(ctx, oldKey.TeamID); err != nil {
		return nil, "", err
	}
	if _, err := s.authorizePrincipalKeyID(ctx, oldKey.PrincipalID); err != nil {
		return nil, "", err
	}

	if oldKey.Revoked {
		return nil, "", fmt.Errorf("key is revoked: %w", domain.ErrInvalidArgument)
	}

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
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventAPIKeyCreated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   newKey.ID,
		TeamID:        newKey.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       newKeyPayload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.created event for rotated key: %w", err)
	}

	gracePeriodEndsAt := time.Now().Add(gracePeriod)
	if err := txRepo.SetRotated(ctx, oldKey.ID, newKey.ID, gracePeriodEndsAt); err != nil {
		return nil, "", fmt.Errorf("set rotated: %w", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"old_key_id":           oldKey.ID,
		"new_key_id":           newKey.ID,
		"grace_period_ends_at": gracePeriodEndsAt,
	})
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventAPIKeyRotated,
		AggregateType: domain.AggregateAPIKey,
		AggregateID:   oldKey.ID,
		TeamID:        oldKey.TeamID,
		ActorID:       ctxutil.GetActingUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.rotated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, oldKey.TeamID, domain.AuditActionAPIKeyRotated, "api_key", oldKey.ID, map[string]any{
		"new_key_id": newKey.ID,
	}); err != nil {
		return nil, "", fmt.Errorf("record authorization audit log: %w", err)
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

	principal, err := s.userRepo.Get(ctx, key.PrincipalID)
	if err != nil {
		return nil, domain.ErrInvalidAuth
	}
	if s.externalAccess != nil {
		if access, accessErr := s.externalAccess.GetActiveByPrincipal(ctx, key.TeamID, key.PrincipalID); accessErr == nil && access != nil {
			key.Permissions = intersectPermissions(key.Permissions, access.AllowedCapabilities)
		}
	}

	// Update usage asynchronously (fire-and-forget, non-critical)
	go func() {
		if err := s.repo.UpdateUsage(context.Background(), key.ID); err != nil {
			s.logger.Warn("failed to update API key usage", "key_id", key.ID, "error", err)
		}
	}()

	return &domain.APIKeyValidation{
		TeamID:        key.TeamID,
		PrincipalID:   key.PrincipalID,
		PrincipalType: principal.PrincipalType,
		AccountType:   principal.EffectiveAccountType(),
		IsBot:         principal.IsBot,
		OnBehalfOf:    key.OnBehalfOf,
		KeyID:         key.ID,
		Permissions:   key.Permissions,
		Environment:   key.Environment,
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

func (s *APIKeyService) authorizePrincipalKeyID(ctx context.Context, principalID string) (*domain.User, error) {
	principal, err := s.userRepo.Get(ctx, principalID)
	if err != nil {
		return nil, err
	}
	_, err = s.authorizePrincipalKeyAccess(ctx, principal)
	if err != nil {
		return nil, err
	}
	return principal, nil
}

func (s *APIKeyService) authorizePrincipalKeyAccess(ctx context.Context, principal *domain.User) (*domain.User, error) {
	if principal == nil {
		return nil, domain.ErrNotFound
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil, nil
	}
	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, principal.TeamID); err != nil {
		if ctxutil.GetTeamID(ctx) == "" || principal.PrincipalType != domain.PrincipalTypeAgent || s.externalAccess == nil {
			return nil, err
		}
		access, accessErr := s.externalAccess.GetActiveByPrincipal(ctx, ctxutil.GetTeamID(ctx), principal.ID)
		if accessErr != nil || access == nil {
			return nil, err
		}
		if !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
			return nil, domain.ErrForbidden
		}
	}
	if actor.ID == principal.ID {
		return actor, nil
	}
	if canManagePrincipal(actor, principal) {
		return actor, nil
	}
	if principal.PrincipalType == domain.PrincipalTypeAgent && s.externalAccess != nil {
		access, err := s.externalAccess.GetActiveByPrincipal(ctx, ctxutil.GetTeamID(ctx), principal.ID)
		if err == nil && access != nil && defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
			return actor, nil
		}
	}
	return nil, domain.ErrForbidden
}

func (s *APIKeyService) authorizeDelegatedKeyIssuance(ctx context.Context, actor, principal, onBehalfOf *domain.User) error {
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actor == nil || principal == nil || onBehalfOf == nil {
		return domain.ErrForbidden
	}
	if !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
		return domain.ErrForbidden
	}
	if actor.ID != principal.ID && !canManagePrincipal(actor, principal) {
		return domain.ErrForbidden
	}
	if actor.ID != onBehalfOf.ID && !canManagePrincipal(actor, onBehalfOf) {
		return domain.ErrForbidden
	}
	return nil
}

func validateAPIKeyPermissions(principal *domain.User, permissions []string) error {
	if principal == nil {
		return nil
	}
	if principal.PrincipalType != domain.PrincipalTypeHuman {
		return nil
	}
	switch principal.EffectiveAccountType() {
	case domain.AccountTypePrimaryAdmin, domain.AccountTypeAdmin:
		return nil
	case domain.AccountTypeMember:
		for _, permission := range permissions {
			switch permission {
			case domain.PermissionMessagesRead,
				domain.PermissionMessagesWrite,
				domain.PermissionConversationsCreate,
				domain.PermissionConversationsMembersWrite,
				domain.PermissionFilesRead,
				domain.PermissionFilesWrite:
				continue
			default:
				return domain.ErrForbidden
			}
		}
	}
	return nil
}

func (s *APIKeyService) validateExternalAccessPermissions(ctx context.Context, teamID string, principal *domain.User, permissions []string) error {
	if principal == nil || principal.PrincipalType != domain.PrincipalTypeAgent || s.externalAccess == nil || teamID == "" || principal.TeamID == teamID {
		return nil
	}
	access, err := s.externalAccess.GetActiveByPrincipal(ctx, teamID, principal.ID)
	if err != nil || access == nil {
		return domain.ErrForbidden
	}
	if len(permissions) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(access.AllowedCapabilities))
	for _, capability := range access.AllowedCapabilities {
		allowed[capability] = struct{}{}
	}
	for _, permission := range permissions {
		if _, ok := allowed[permission]; !ok {
			return domain.ErrForbidden
		}
	}
	return nil
}

func intersectPermissions(keyPermissions, allowedCapabilities []string) []string {
	if len(allowedCapabilities) == 0 {
		return []string{}
	}
	allowed := make(map[string]struct{}, len(allowedCapabilities))
	for _, capability := range allowedCapabilities {
		allowed[capability] = struct{}{}
	}
	out := make([]string, 0, len(keyPermissions))
	for _, permission := range keyPermissions {
		if _, ok := allowed[permission]; ok {
			out = append(out, permission)
		}
	}
	return out
}
