package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// APIKeyService contains business logic for API key operations.
type APIKeyService struct {
	repo            repository.APIKeyRepository
	userRepo        repository.UserRepository
	externalMembers repository.ExternalMemberRepository
	auditRepo       repository.AuthorizationAuditRepository
	recorder        EventRecorder
	db              repository.TxBeginner
	logger          *slog.Logger
}

// NewAPIKeyService creates a new APIKeyService.
func NewAPIKeyService(repo repository.APIKeyRepository, userRepo repository.UserRepository, recorder EventRecorder, db repository.TxBeginner, logger *slog.Logger) *APIKeyService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &APIKeyService{repo: repo, userRepo: userRepo, recorder: recorder, db: db, logger: logger}
}

func (s *APIKeyService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMembers = repo
}

func (s *APIKeyService) SetIdentityRepositories(_ ...any) {}

func (s *APIKeyService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func normalizeAPIKeyScope(params *domain.CreateAPIKeyParams) {
	if params.Scope != "" {
		return
	}
	if strings.TrimSpace(params.AccountID) != "" || len(params.WorkspaceIDs) > 0 {
		params.Scope = domain.APIKeyScopeAccount
		return
	}
	params.Scope = domain.APIKeyScopeWorkspaceSystem
}

// Create creates a new API key and returns the raw key (only available at creation time).
func (s *APIKeyService) Create(ctx context.Context, params domain.CreateAPIKeyParams) (*domain.APIKey, string, error) {
	if err := requirePermission(ctx, domain.PermissionAPIKeysCreate); err != nil {
		return nil, "", err
	}
	if params.Name == "" {
		return nil, "", fmt.Errorf("name: %w", domain.ErrInvalidArgument)
	}

	normalizeAPIKeyScope(&params)
	if params.Scope != domain.APIKeyScopeAccount && params.Scope != domain.APIKeyScopeWorkspaceSystem {
		return nil, "", fmt.Errorf("scope: %w", domain.ErrInvalidArgument)
	}

	var actor *domain.User
	if !isInternalCallWithoutAuth(ctx) {
		var err error
		actor, err = loadActingUser(ctx, s.userRepo)
		if err != nil {
			return nil, "", err
		}
		if params.CreatedBy == "" {
			params.CreatedBy = actor.ID
		}
	}

	switch params.Scope {
	case domain.APIKeyScopeAccount:
		if params.WorkspaceID != "" {
			if _, err := resolveWorkspaceID(ctx, params.WorkspaceID); err != nil {
				return nil, "", err
			}
		}
		if params.AccountID == "" {
			params.AccountID = ctxutil.GetAccountID(ctx)
		}
		if params.AccountID == "" {
			return nil, "", fmt.Errorf("account_id: %w", domain.ErrInvalidArgument)
		}
		if actor != nil {
			if actor.AccountID != params.AccountID {
				return nil, "", domain.ErrForbidden
			}
			if err := validateAPIKeyPermissions(actor, params.Permissions); err != nil {
				return nil, "", err
			}
		}
		params.WorkspaceIDs = normalizeWorkspaceIDs(params.WorkspaceIDs)
		if err := s.validateAccountKeyWorkspaceIDs(ctx, params.AccountID, params.WorkspaceIDs); err != nil {
			return nil, "", err
		}
		params.WorkspaceID = ""
	case domain.APIKeyScopeWorkspaceSystem:
		workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
		if err != nil {
			return nil, "", err
		}
		params.WorkspaceID = workspaceID
		params.AccountID = ""
		params.WorkspaceIDs = nil
		if actor != nil && !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
			return nil, "", domain.ErrForbidden
		}
	}

	if params.CreatedBy == "" {
		return nil, "", fmt.Errorf("created_by: %w", domain.ErrInvalidArgument)
	}
	if err := s.validateAPIKeyCreator(ctx, creationAuditWorkspaceID(ctx, params), params.CreatedBy); err != nil {
		return nil, "", err
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
		WorkspaceID:   s.apiKeyEventWorkspaceID(ctx, key),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.created event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, s.apiKeyEventWorkspaceID(ctx, key), domain.AuditActionAPIKeyCreated, "api_key", key.ID, map[string]any{
		"scope":         key.Scope,
		"account_id":    key.AccountID,
		"workspace_id":  key.WorkspaceID,
		"workspace_ids": key.WorkspaceIDs,
		"permissions":   key.Permissions,
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
	if _, err := s.authorizeAPIKeyManagement(ctx, key); err != nil {
		return nil, err
	}
	key.KeyHash = ""
	return key, nil
}

// List retrieves API keys with pagination and filtering.
func (s *APIKeyService) List(ctx context.Context, params domain.ListAPIKeysParams) (*domain.CursorPage[domain.APIKey], error) {
	workspaceID, err := resolveWorkspaceID(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID
	if requiresAuthenticatedActor(ctx) {
		actor, err := loadActingUser(ctx, s.userRepo)
		if err != nil {
			return nil, err
		}
		if !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
			params.AccountID = actor.AccountID
			params.Scope = domain.APIKeyScopeAccount
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
	if _, err := s.authorizeAPIKeyManagement(ctx, key); err != nil {
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
		WorkspaceID:   s.apiKeyEventWorkspaceID(ctx, key),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record api_key.revoked event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, s.apiKeyEventWorkspaceID(ctx, key), domain.AuditActionAPIKeyRevoked, "api_key", key.ID, map[string]any{
		"scope": key.Scope,
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
	actor, err := s.authorizeAPIKeyManagement(ctx, existing)
	if err != nil {
		return nil, err
	}
	if params.Permissions != nil && !isInternalCallWithoutAuth(ctx) {
		if err := validateAPIKeyPermissions(actor, *params.Permissions); err != nil {
			return nil, err
		}
		if existing.Scope == domain.APIKeyScopeAccount {
			if err := s.validateExternalMemberPermissions(ctx, ctxutil.GetWorkspaceID(ctx), actor, *params.Permissions); err != nil {
				return nil, err
			}
		}
	}
	if params.WorkspaceIDs != nil {
		if existing.Scope != domain.APIKeyScopeAccount {
			return nil, fmt.Errorf("workspace_ids: %w", domain.ErrInvalidArgument)
		}
		normalized := normalizeWorkspaceIDs(*params.WorkspaceIDs)
		params.WorkspaceIDs = &normalized
		if err := s.validateAccountKeyWorkspaceIDs(ctx, existing.AccountID, normalized); err != nil {
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
		WorkspaceID:   s.apiKeyEventWorkspaceID(ctx, key),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record api_key.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, s.apiKeyEventWorkspaceID(ctx, key), domain.AuditActionAPIKeyUpdated, "api_key", key.ID, map[string]any{
		"scope":         key.Scope,
		"account_id":    key.AccountID,
		"workspace_ids": key.WorkspaceIDs,
		"permissions":   key.Permissions,
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
	if _, err := s.authorizeAPIKeyManagement(ctx, oldKey); err != nil {
		return nil, "", err
	}

	if oldKey.Revoked {
		return nil, "", fmt.Errorf("key is revoked: %w", domain.ErrInvalidArgument)
	}

	// Create the new key with the same properties as the old one
	newKey, rawKey, err := txRepo.Create(ctx, domain.CreateAPIKeyParams{
		Name:         oldKey.Name + " (rotated)",
		Description:  oldKey.Description,
		Scope:        oldKey.Scope,
		WorkspaceID:  oldKey.WorkspaceID,
		AccountID:    oldKey.AccountID,
		WorkspaceIDs: oldKey.WorkspaceIDs,
		CreatedBy:    oldKey.CreatedBy,
		Permissions:  oldKey.Permissions,
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
		WorkspaceID:   s.apiKeyEventWorkspaceID(ctx, newKey),
		ActorID:       actorUserID(ctx),
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
		WorkspaceID:   s.apiKeyEventWorkspaceID(ctx, oldKey),
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, "", fmt.Errorf("record api_key.rotated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, s.apiKeyEventWorkspaceID(ctx, oldKey), domain.AuditActionAPIKeyRotated, "api_key", oldKey.ID, map[string]any{
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

	// Update usage asynchronously (fire-and-forget, non-critical)
	go func() {
		if err := s.repo.UpdateUsage(context.Background(), key.ID); err != nil {
			s.logger.Warn("failed to update API key usage", "key_id", key.ID, "error", err)
		}
	}()

	if key.Scope == domain.APIKeyScopeWorkspaceSystem {
		return &domain.APIKeyValidation{
			Scope:         key.Scope,
			WorkspaceID:   key.WorkspaceID,
			PrincipalType: domain.PrincipalTypeSystem,
			AccountType:   domain.AccountTypePrimaryAdmin,
			KeyID:         key.ID,
			Permissions:   key.Permissions,
		}, nil
	}

	user, err := s.resolveAccountAPIKeyUser(ctx, key)
	if err != nil {
		return nil, domain.ErrInvalidAuth
	}
	if caps, capsErr := s.externalMemberCapabilitiesForUser(ctx, key.WorkspaceID, user); capsErr == nil && len(caps) > 0 {
		key.Permissions = intersectPermissions(key.Permissions, caps)
	}

	validation := &domain.APIKeyValidation{
		Scope:         key.Scope,
		WorkspaceID:   key.WorkspaceID,
		UserID:        user.ID,
		AccountID:     user.AccountID,
		PrincipalType: user.PrincipalType,
		AccountType:   user.EffectiveAccountType(),
		IsBot:         user.IsBot,
		KeyID:         key.ID,
		Permissions:   key.Permissions,
	}
	if user.WorkspaceID != "" {
		validation.WorkspaceID = user.WorkspaceID
	}

	return validation, nil
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

func (s *APIKeyService) validateAPIKeyCreator(ctx context.Context, workspaceID, createdBy string) error {
	if createdBy == "" {
		return nil
	}
	if workspaceID == "" {
		return nil
	}
	creator, err := loadUser(ctx, s.userRepo, createdBy)
	if err != nil {
		return fmt.Errorf("created_by: %w", err)
	}
	if creator.WorkspaceID != workspaceID {
		return fmt.Errorf("created_by: %w", domain.ErrForbidden)
	}
	return nil
}

func normalizeWorkspaceIDs(workspaceIDs []string) []string {
	if len(workspaceIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(workspaceIDs))
	out := make([]string, 0, len(workspaceIDs))
	for _, workspaceID := range workspaceIDs {
		workspaceID = strings.TrimSpace(workspaceID)
		if workspaceID == "" {
			continue
		}
		if _, ok := seen[workspaceID]; ok {
			continue
		}
		seen[workspaceID] = struct{}{}
		out = append(out, workspaceID)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func creationAuditWorkspaceID(ctx context.Context, params domain.CreateAPIKeyParams) string {
	if params.Scope == domain.APIKeyScopeWorkspaceSystem {
		return params.WorkspaceID
	}
	return ctxutil.GetWorkspaceID(ctx)
}

func (s *APIKeyService) apiKeyEventWorkspaceID(ctx context.Context, key *domain.APIKey) string {
	if key != nil && key.WorkspaceID != "" {
		return key.WorkspaceID
	}
	if workspaceID := ctxutil.GetWorkspaceID(ctx); workspaceID != "" {
		return workspaceID
	}
	if key == nil || key.Scope != domain.APIKeyScopeAccount {
		return ""
	}
	if len(key.WorkspaceIDs) == 1 {
		return key.WorkspaceIDs[0]
	}
	if strings.TrimSpace(key.AccountID) == "" || s.userRepo == nil {
		return ""
	}
	users, err := s.userRepo.ListByAccount(ctx, key.AccountID)
	if err != nil || len(users) != 1 {
		return ""
	}
	return users[0].WorkspaceID
}

func (s *APIKeyService) validateAccountKeyWorkspaceIDs(ctx context.Context, accountID string, workspaceIDs []string) error {
	for _, workspaceID := range workspaceIDs {
		if _, err := s.userRepo.GetByWorkspaceAndAccount(ctx, workspaceID, accountID); err != nil {
			return fmt.Errorf("workspace_ids: %w", domain.ErrInvalidArgument)
		}
	}
	return nil
}

func (s *APIKeyService) authorizeAPIKeyManagement(ctx context.Context, key *domain.APIKey) (*domain.User, error) {
	if key == nil {
		return nil, domain.ErrNotFound
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil, nil
	}
	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	switch key.Scope {
	case domain.APIKeyScopeWorkspaceSystem:
		if err := ensureWorkspaceAccess(ctx, key.WorkspaceID); err != nil {
			return nil, err
		}
		if !defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
			return nil, domain.ErrForbidden
		}
		return actor, nil
	case domain.APIKeyScopeAccount:
		if strings.TrimSpace(actor.AccountID) == "" || actor.AccountID != key.AccountID {
			return nil, domain.ErrForbidden
		}
		return actor, nil
	default:
		return nil, domain.ErrForbidden
	}
}

func workspaceAllowedForAccountKey(key *domain.APIKey, workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	if len(key.WorkspaceIDs) == 0 {
		return true
	}
	for _, candidate := range key.WorkspaceIDs {
		if candidate == workspaceID {
			return true
		}
	}
	return false
}

func (s *APIKeyService) resolveAccountAPIKeyUser(ctx context.Context, key *domain.APIKey) (*domain.User, error) {
	if key == nil || key.Scope != domain.APIKeyScopeAccount {
		return nil, domain.ErrInvalidAuth
	}
	if strings.TrimSpace(key.AccountID) == "" && strings.TrimSpace(key.CreatedBy) != "" {
		user, err := loadUser(ctx, s.userRepo, key.CreatedBy)
		if err != nil {
			return nil, err
		}
		key.AccountID = user.AccountID
		key.WorkspaceID = user.WorkspaceID
		return user, nil
	}
	if strings.TrimSpace(key.AccountID) == "" {
		return nil, domain.ErrInvalidAuth
	}
	if requestedWorkspaceID := strings.TrimSpace(ctxutil.GetWorkspaceID(ctx)); requestedWorkspaceID != "" {
		if !workspaceAllowedForAccountKey(key, requestedWorkspaceID) {
			return nil, domain.ErrForbidden
		}
		user, err := s.userRepo.GetByWorkspaceAndAccount(ctx, requestedWorkspaceID, key.AccountID)
		if err != nil {
			return nil, err
		}
		key.WorkspaceID = user.WorkspaceID
		return user, nil
	}
	users, err := s.userRepo.ListByAccount(ctx, key.AccountID)
	if err != nil {
		return nil, err
	}
	eligible := make([]domain.User, 0, len(users))
	for _, user := range users {
		if workspaceAllowedForAccountKey(key, user.WorkspaceID) {
			eligible = append(eligible, user)
		}
	}
	if len(eligible) != 1 {
		return nil, domain.ErrForbidden
	}
	key.WorkspaceID = eligible[0].WorkspaceID
	return &eligible[0], nil
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

func (s *APIKeyService) validateExternalMemberPermissions(ctx context.Context, workspaceID string, principal *domain.User, permissions []string) error {
	if principal == nil || principal.PrincipalType != domain.PrincipalTypeAgent || workspaceID == "" || principal.WorkspaceID == workspaceID {
		return nil
	}
	allowedCapabilities, err := s.externalMemberCapabilitiesForUser(ctx, workspaceID, principal)
	if err != nil || len(allowedCapabilities) == 0 {
		return domain.ErrForbidden
	}
	if len(permissions) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(allowedCapabilities))
	for _, capability := range allowedCapabilities {
		allowed[capability] = struct{}{}
	}
	for _, permission := range permissions {
		if _, ok := allowed[permission]; !ok {
			return domain.ErrForbidden
		}
	}
	return nil
}

func (s *APIKeyService) externalMemberCapabilitiesForUser(ctx context.Context, workspaceID string, principal *domain.User) ([]string, error) {
	if principal == nil || s.externalMembers == nil || workspaceID == "" || strings.TrimSpace(principal.AccountID) == "" {
		return nil, nil
	}
	items, err := s.externalMembers.ListActiveByAccountAndWorkspace(ctx, principal.AccountID, workspaceID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{})
	for _, item := range items {
		for _, capability := range item.AllowedCapabilities {
			allowed[capability] = struct{}{}
		}
	}
	out := make([]string, 0, len(allowed))
	for capability := range allowed {
		out = append(out, capability)
	}
	return out, nil
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
