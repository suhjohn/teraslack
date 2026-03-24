package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ExternalPrincipalAccessService struct {
	repo      repository.ExternalPrincipalAccessRepository
	userRepo  repository.UserRepository
	convRepo  repository.ConversationRepository
	auditRepo repository.AuthorizationAuditRepository
	recorder  EventRecorder
	db        repository.TxBeginner
	logger    *slog.Logger
}

func NewExternalPrincipalAccessService(
	repo repository.ExternalPrincipalAccessRepository,
	userRepo repository.UserRepository,
	convRepo repository.ConversationRepository,
	recorder EventRecorder,
	db repository.TxBeginner,
	logger *slog.Logger,
) *ExternalPrincipalAccessService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &ExternalPrincipalAccessService{
		repo:     repo,
		userRepo: userRepo,
		convRepo: convRepo,
		recorder: recorder,
		db:       db,
		logger:   logger,
	}
}

func (s *ExternalPrincipalAccessService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *ExternalPrincipalAccessService) Create(ctx context.Context, params domain.CreateExternalPrincipalAccessParams) (*domain.ExternalPrincipalAccess, error) {
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	teamID, err := resolveTeamID(ctx, params.HostTeamID)
	if err != nil {
		return nil, err
	}
	params.HostTeamID = teamID
	params.GrantedBy = actor.ID
	if params.HomeTeamID == "" {
		principal, err := s.userRepo.Get(ctx, params.PrincipalID)
		if err != nil {
			return nil, fmt.Errorf("principal: %w", err)
		}
		params.HomeTeamID = principal.TeamID
	}
	if err := s.validateCreateOrUpdate(ctx, params.HostTeamID, params.HomeTeamID, params.PrincipalID, params.PrincipalType, params.AccessMode, params.AllowedCapabilities, params.ConversationIDs); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	access, err := s.repo.WithTx(tx).Create(ctx, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(access)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventExternalPrincipalAccessGranted,
		AggregateType: domain.AggregateUser,
		AggregateID:   access.PrincipalID,
		TeamID:        access.HostTeamID,
		ActorID:       actor.ID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record external_principal_access.granted event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, access.HostTeamID, domain.AuditActionExternalPrincipalAccessGranted, "external_principal_access", access.ID, access); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	return access, nil
}

func (s *ExternalPrincipalAccessService) Get(ctx context.Context, id string) (*domain.ExternalPrincipalAccess, error) {
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	_ = actor
	access, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, access.HostTeamID); err != nil {
		return nil, err
	}
	return access, nil
}

func (s *ExternalPrincipalAccessService) List(ctx context.Context, params domain.ListExternalPrincipalAccessParams) ([]domain.ExternalPrincipalAccess, error) {
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	_ = actor
	teamID, err := resolveTeamID(ctx, params.HostTeamID)
	if err != nil {
		return nil, err
	}
	params.HostTeamID = teamID
	return s.repo.List(ctx, params)
}

func (s *ExternalPrincipalAccessService) Update(ctx context.Context, id string, params domain.UpdateExternalPrincipalAccessParams) (*domain.ExternalPrincipalAccess, error) {
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ensureTeamAccess(ctx, existing.HostTeamID); err != nil {
		return nil, err
	}

	mode := existing.AccessMode
	if params.AccessMode != nil {
		mode = *params.AccessMode
	}
	caps := existing.AllowedCapabilities
	if params.AllowedCapabilities != nil {
		caps = append([]string(nil), (*params.AllowedCapabilities)...)
	}
	conversationIDs := existing.ConversationIDs
	if params.ConversationIDs != nil {
		conversationIDs = append([]string(nil), (*params.ConversationIDs)...)
	}
	if err := s.validateCreateOrUpdate(ctx, existing.HostTeamID, existing.HomeTeamID, existing.PrincipalID, existing.PrincipalType, mode, caps, conversationIDs); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	updated, err := s.repo.WithTx(tx).Update(ctx, id, params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(updated)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventExternalPrincipalAccessUpdated,
		AggregateType: domain.AggregateUser,
		AggregateID:   updated.PrincipalID,
		TeamID:        updated.HostTeamID,
		ActorID:       actor.ID,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record external_principal_access.updated event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, updated.HostTeamID, domain.AuditActionExternalPrincipalAccessUpdated, "external_principal_access", updated.ID, updated); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	return updated, nil
}

func (s *ExternalPrincipalAccessService) Revoke(ctx context.Context, id string) error {
	actor, err := requireWorkspaceAdminActor(ctx, s.userRepo)
	if err != nil {
		return err
	}
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ensureTeamAccess(ctx, existing.HostTeamID); err != nil {
		return err
	}
	now := time.Now().UTC()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.WithTx(tx).Revoke(ctx, id, now); err != nil {
		return err
	}
	existing.RevokedAt = &now
	payload, _ := json.Marshal(existing)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventExternalPrincipalAccessRevoked,
		AggregateType: domain.AggregateUser,
		AggregateID:   existing.PrincipalID,
		TeamID:        existing.HostTeamID,
		ActorID:       actor.ID,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("record external_principal_access.revoked event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, existing.HostTeamID, domain.AuditActionExternalPrincipalAccessRevoked, "external_principal_access", existing.ID, existing); err != nil {
		return fmt.Errorf("record authorization audit log: %w", err)
	}
	return nil
}

func (s *ExternalPrincipalAccessService) validateCreateOrUpdate(ctx context.Context, hostTeamID, homeTeamID, principalID string, principalType domain.PrincipalType, accessMode domain.ExternalPrincipalAccessMode, allowedCapabilities, conversationIDs []string) error {
	if principalID == "" {
		return fmt.Errorf("principal_id: %w", domain.ErrInvalidArgument)
	}
	if principalType != domain.PrincipalTypeAgent {
		return fmt.Errorf("principal_type: %w", domain.ErrInvalidArgument)
	}
	if !domain.IsValidExternalPrincipalAccessMode(accessMode) {
		return fmt.Errorf("access_mode: %w", domain.ErrInvalidArgument)
	}
	principal, err := s.userRepo.Get(ctx, principalID)
	if err != nil {
		return fmt.Errorf("principal: %w", err)
	}
	if principal.PrincipalType != domain.PrincipalTypeAgent {
		return fmt.Errorf("principal_id: %w", domain.ErrInvalidArgument)
	}
	if homeTeamID != "" && principal.TeamID != homeTeamID {
		return fmt.Errorf("home_team_id: %w", domain.ErrInvalidArgument)
	}
	caps := dedupeStrings(allowedCapabilities)
	for _, capability := range caps {
		if capability == "" {
			return fmt.Errorf("allowed_capabilities: %w", domain.ErrInvalidArgument)
		}
	}
	for _, conversationID := range dedupeStrings(conversationIDs) {
		conv, err := s.convRepo.Get(ctx, conversationID)
		if err != nil {
			return fmt.Errorf("conversation_ids: %w", err)
		}
		if conv.TeamID != hostTeamID {
			return fmt.Errorf("conversation_ids: %w", domain.ErrInvalidArgument)
		}
		if conv.Type == domain.ConversationTypeIM || conv.Type == domain.ConversationTypeMPIM {
			return fmt.Errorf("conversation_ids: %w", domain.ErrInvalidArgument)
		}
	}
	return nil
}

func activeExternalSharedAccess(ctx context.Context, repo repository.ExternalPrincipalAccessRepository) (*domain.ExternalPrincipalAccess, error) {
	if repo == nil {
		return nil, nil
	}
	teamID := ctxutil.GetTeamID(ctx)
	principalID := ctxutil.GetUserID(ctx)
	if teamID == "" || principalID == "" {
		return nil, nil
	}
	return repo.GetActiveByPrincipal(ctx, teamID, principalID)
}

func ensureExternalSharedConversationAccess(ctx context.Context, repo repository.ExternalPrincipalAccessRepository, conv *domain.Conversation, capability string, requireWrite bool) error {
	access, err := activeExternalSharedAccess(ctx, repo)
	if err != nil {
		return err
	}
	if access == nil {
		return nil
	}
	if conv.Type == domain.ConversationTypeIM || conv.Type == domain.ConversationTypeMPIM {
		return domain.ErrForbidden
	}
	ok, err := repo.HasConversationAccess(ctx, access.ID, conv.ID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrForbidden
	}
	if capability != "" && !capabilityAllowed(access.AllowedCapabilities, capability) {
		return domain.ErrForbidden
	}
	if requireWrite && access.AccessMode == domain.ExternalPrincipalAccessModeSharedReadOnly {
		return domain.ErrForbidden
	}
	return nil
}

func filterExternalSharedConversations(ctx context.Context, repo repository.ExternalPrincipalAccessRepository, conversations []domain.Conversation) ([]domain.Conversation, error) {
	access, err := activeExternalSharedAccess(ctx, repo)
	if err != nil {
		return nil, err
	}
	if access == nil {
		return conversations, nil
	}
	allowed := make(map[string]struct{}, len(access.ConversationIDs))
	for _, id := range access.ConversationIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]domain.Conversation, 0, len(conversations))
	for _, conv := range conversations {
		if conv.Type == domain.ConversationTypeIM || conv.Type == domain.ConversationTypeMPIM {
			continue
		}
		if _, ok := allowed[conv.ID]; ok {
			filtered = append(filtered, conv)
		}
	}
	return filtered, nil
}

func ensureExternalSharedFileAccess(ctx context.Context, repo repository.ExternalPrincipalAccessRepository, f *domain.File, capability string, requireWrite bool) error {
	access, err := activeExternalSharedAccess(ctx, repo)
	if err != nil {
		return err
	}
	if access == nil {
		return nil
	}
	if capability != "" && !capabilityAllowed(access.AllowedCapabilities, capability) {
		return domain.ErrForbidden
	}
	if requireWrite && access.AccessMode == domain.ExternalPrincipalAccessModeSharedReadOnly {
		return domain.ErrForbidden
	}
	allowed := make(map[string]struct{}, len(access.ConversationIDs))
	for _, id := range access.ConversationIDs {
		allowed[id] = struct{}{}
	}
	for _, channelID := range f.Channels {
		if _, ok := allowed[channelID]; ok {
			return nil
		}
	}
	return domain.ErrForbidden
}

func isExternalSharedActor(ctx context.Context, repo repository.ExternalPrincipalAccessRepository) (bool, error) {
	access, err := activeExternalSharedAccess(ctx, repo)
	if err != nil {
		return false, err
	}
	return access != nil, nil
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

func normalizeCapabilities(values []string) []string {
	values = dedupeStrings(values)
	sort.Strings(values)
	return values
}
