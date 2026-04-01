package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ConversationAccessService struct {
	repo          repository.ConversationAccessRepository
	convRepo      repository.ConversationRepository
	userRepo      repository.UserRepository
	roleRepo      repository.RoleAssignmentRepository
	auditRepo     repository.AuthorizationAuditRepository
	recorder      EventRecorder
	db            repository.TxBeginner
	logger        *slog.Logger
}

func NewConversationAccessService(
	repo repository.ConversationAccessRepository,
	convRepo repository.ConversationRepository,
	userRepo repository.UserRepository,
	roleRepo repository.RoleAssignmentRepository,
	recorder EventRecorder,
	db repository.TxBeginner,
	logger *slog.Logger,
) *ConversationAccessService {
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &ConversationAccessService{
		repo:          repo,
		convRepo:      convRepo,
		userRepo:      userRepo,
		roleRepo:      roleRepo,
		recorder:      recorder,
		db:            db,
		logger:        logger,
	}
}

func (s *ConversationAccessService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *ConversationAccessService) ListManagers(ctx context.Context, conversationID string) ([]string, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}
	assignments, err := s.repo.ListManagers(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		userIDs = append(userIDs, assignment.UserID)
	}
	return userIDs, nil
}

func (s *ConversationAccessService) SetManagers(ctx context.Context, conversationID string, userIDs []string) ([]string, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsManagersWrite); err != nil {
		return nil, err
	}
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}
	if err := s.authorizeManagerMutation(ctx, conv); err != nil {
		return nil, err
	}

	normalizedUserIDs, err := s.normalizeManagerUserIDs(ctx, conv.WorkspaceID, userIDs)
	if err != nil {
		return nil, err
	}
	assignedBy := ctxutil.GetActingUserID(ctx)
	if assignedBy == "" {
		assignedBy = conv.CreatorID
	}

	currentAssignments, err := s.repo.ListManagers(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	currentUserIDs := make([]string, 0, len(currentAssignments))
	for _, assignment := range currentAssignments {
		currentUserIDs = append(currentUserIDs, assignment.UserID)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.ReplaceManagers(ctx, conversationID, normalizedUserIDs, assignedBy); err != nil {
		return nil, err
	}

	added, removed := diffStringSets(currentUserIDs, normalizedUserIDs)
	for _, userID := range added {
		payload, _ := json.Marshal(map[string]string{
			"conversation_id": conversationID,
			"user_id":         userID,
		})
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventConversationManagerAdded,
			AggregateType: domain.AggregateConversation,
			AggregateID:   conversationID,
			WorkspaceID:        conv.WorkspaceID,
			ActorID:       assignedBy,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record conversation.manager_added event: %w", err)
		}
	}
	for _, userID := range removed {
		payload, _ := json.Marshal(map[string]string{
			"conversation_id": conversationID,
			"user_id":         userID,
		})
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventConversationManagerRemoved,
			AggregateType: domain.AggregateConversation,
			AggregateID:   conversationID,
			WorkspaceID:        conv.WorkspaceID,
			ActorID:       assignedBy,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record conversation.manager_removed event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, conv.WorkspaceID, domain.AuditActionConversationManagersUpdated, "conversation", conversationID, map[string]any{
		"user_ids": normalizedUserIDs,
	}); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	return normalizedUserIDs, nil
}

func (s *ConversationAccessService) GetPostingPolicy(ctx context.Context, conversationID string) (*domain.ConversationPostingPolicy, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureConversationVisible(ctx, conv); err != nil {
		return nil, err
	}
	policy, err := s.repo.GetPostingPolicy(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		defaultPolicy := domain.DefaultConversationPostingPolicy(conversationID)
		return &defaultPolicy, nil
	}
	return policy, nil
}

func (s *ConversationAccessService) SetPostingPolicy(ctx context.Context, policy domain.ConversationPostingPolicy) (*domain.ConversationPostingPolicy, error) {
	if err := requirePermission(ctx, domain.PermissionConversationsPostingPolicyWrite); err != nil {
		return nil, err
	}
	if policy.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id: %w", domain.ErrInvalidArgument)
	}
	conv, err := s.convRepo.Get(ctx, policy.ConversationID)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return nil, err
	}
	if err := s.authorizePostingPolicyMutation(ctx, conv); err != nil {
		return nil, err
	}
	if err := s.validatePostingPolicy(ctx, conv.WorkspaceID, &policy); err != nil {
		return nil, err
	}

	policy.UpdatedBy = ctxutil.GetActingUserID(ctx)
	if policy.UpdatedBy == "" {
		policy.UpdatedBy = conv.CreatorID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	updated, err := s.repo.WithTx(tx).UpsertPostingPolicy(ctx, policy)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(updated)
	if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventConversationPostingPolicyUpdated,
		AggregateType: domain.AggregateConversation,
		AggregateID:   policy.ConversationID,
		WorkspaceID:        conv.WorkspaceID,
		ActorID:       policy.UpdatedBy,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record conversation.posting_policy.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, conv.WorkspaceID, domain.AuditActionPostingPolicyUpdated, "conversation", policy.ConversationID, updated); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	return updated, nil
}

func (s *ConversationAccessService) CanManageConversation(ctx context.Context, conv *domain.Conversation) error {
	if conv == nil {
		return fmt.Errorf("conversation: %w", domain.ErrInvalidArgument)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actorID := ctxutil.GetActingUserID(ctx); actorID != "" && actorID == conv.CreatorID {
		return nil
	}
	if actor, err := loadActingUser(ctx, s.userRepo); err == nil && defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
		return nil
	}
	if s.actorCanAdministerConversation(ctx, conv) {
		return nil
	}
	isManager, err := s.isActorConversationManager(ctx, conv.ID)
	if err != nil {
		return err
	}
	if isManager {
		return nil
	}
	return domain.ErrForbidden
}

func (s *ConversationAccessService) CanArchiveConversation(ctx context.Context, conv *domain.Conversation) error {
	if conv == nil {
		return fmt.Errorf("conversation: %w", domain.ErrInvalidArgument)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actorID := ctxutil.GetActingUserID(ctx); actorID != "" && actorID == conv.CreatorID {
		return nil
	}
	if actor, err := loadActingUser(ctx, s.userRepo); err == nil && defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
		return nil
	}
	if s.actorCanAdministerConversation(ctx, conv) {
		return nil
	}
	return domain.ErrForbidden
}

func (s *ConversationAccessService) CanManageMembers(ctx context.Context, conv *domain.Conversation) error {
	if err := s.CanManageConversation(ctx, conv); err == nil {
		return nil
	}
	return domain.ErrForbidden
}

func (s *ConversationAccessService) CanPost(ctx context.Context, conv *domain.Conversation, actorID string) error {
	if conv == nil {
		return fmt.Errorf("conversation: %w", domain.ErrInvalidArgument)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actorID == "" {
		actorID = ctxutil.GetActingUserID(ctx)
	}
	if actorID == "" {
		return domain.ErrForbidden
	}
	if actor, err := loadActingUser(ctx, s.userRepo); err == nil && defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
		return nil
	}

	policy, err := s.repo.GetPostingPolicy(ctx, conv.ID)
	if err != nil {
		return err
	}
	if policy == nil {
		defaultPolicy := domain.DefaultConversationPostingPolicy(conv.ID)
		policy = &defaultPolicy
	}

	switch policy.PolicyType {
	case domain.ConversationPostingPolicyEveryone:
		return nil
	case domain.ConversationPostingPolicyAdminsOnly:
		return domain.ErrForbidden
	case domain.ConversationPostingPolicyMembersWithPermission:
		if actorID == conv.CreatorID || s.actorCanAdministerConversation(ctx, conv) {
			return nil
		}
		isManager, err := s.repo.IsManager(ctx, conv.ID, actorID)
		if err != nil {
			return err
		}
		if isManager {
			return nil
		}
		return domain.ErrForbidden
	case domain.ConversationPostingPolicyCustom:
		allowed, err := s.actorMatchesCustomPostingPolicy(ctx, conv.WorkspaceID, actorID, policy)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		return domain.ErrForbidden
	default:
		return domain.ErrForbidden
	}
}

func (s *ConversationAccessService) ensureConversationVisible(ctx context.Context, conv *domain.Conversation) error {
	if conv == nil {
		return fmt.Errorf("conversation: %w", domain.ErrInvalidArgument)
	}
	if err := ensureWorkspaceAccess(ctx, conv.WorkspaceID); err != nil {
		return err
	}
	switch conv.Type {
	case domain.ConversationTypePrivateChannel, domain.ConversationTypeIM, domain.ConversationTypeMPIM:
		if isInternalCallWithoutAuth(ctx) {
			return nil
		}
		if contextIsWorkspaceAdmin(ctx) {
			return nil
		}
		actorID := ctxutil.GetActingUserID(ctx)
		if actorID == "" {
			return domain.ErrForbidden
		}
		isMember, err := s.convRepo.IsMember(ctx, conv.ID, actorID)
		if err != nil {
			return err
		}
		if !isMember {
			return domain.ErrForbidden
		}
	}
	return nil
}

func (s *ConversationAccessService) authorizeManagerMutation(ctx context.Context, conv *domain.Conversation) error {
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actor, err := loadActingUser(ctx, s.userRepo); err == nil && defaultAuthorizer.IsWorkspaceAdminAccount(actor.EffectiveAccountType()) {
		return nil
	}
	if s.actorCanAdministerConversation(ctx, conv) {
		return nil
	}
	return domain.ErrForbidden
}

func (s *ConversationAccessService) authorizePostingPolicyMutation(ctx context.Context, conv *domain.Conversation) error {
	return s.authorizeManagerMutation(ctx, conv)
}

func (s *ConversationAccessService) actorCanAdministerConversation(ctx context.Context, conv *domain.Conversation) bool {
	actor, err := loadActingUser(ctx, s.userRepo)
	if err != nil {
		return false
	}
	if !hasDelegatedRole(ctx, s.roleRepo, actor, domain.DelegatedRoleChannelsAdmin) {
		return false
	}
	if conv.Type != domain.ConversationTypePrivateChannel {
		return true
	}
	isMember, err := s.convRepo.IsMember(ctx, conv.ID, actor.ID)
	if err != nil {
		return false
	}
	return isMember
}

func (s *ConversationAccessService) isActorConversationManager(ctx context.Context, conversationID string) (bool, error) {
	actorID := ctxutil.GetActingUserID(ctx)
	if actorID == "" {
		return false, nil
	}
	return s.repo.IsManager(ctx, conversationID, actorID)
}

func (s *ConversationAccessService) normalizeManagerUserIDs(ctx context.Context, workspaceID string, userIDs []string) ([]string, error) {
	seen := make(map[string]struct{}, len(userIDs))
	normalized := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		user, err := s.userRepo.Get(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("manager user %s: %w", userID, err)
		}
		if user.WorkspaceID != workspaceID || user.PrincipalType != domain.PrincipalTypeHuman {
			return nil, fmt.Errorf("manager user_ids: %w", domain.ErrInvalidArgument)
		}
		seen[userID] = struct{}{}
		normalized = append(normalized, userID)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func (s *ConversationAccessService) validatePostingPolicy(ctx context.Context, workspaceID string, policy *domain.ConversationPostingPolicy) error {
	if !domain.IsValidConversationPostingPolicyType(policy.PolicyType) {
		return fmt.Errorf("policy_type: %w", domain.ErrInvalidArgument)
	}
	policy.AllowedAccountTypes = dedupeAccountTypes(policy.AllowedAccountTypes)
	policy.AllowedDelegatedRoles = dedupeDelegatedRoles(policy.AllowedDelegatedRoles)
	policy.AllowedUserIDs = dedupeStrings(policy.AllowedUserIDs)

	for _, accountType := range policy.AllowedAccountTypes {
		switch accountType {
		case domain.AccountTypePrimaryAdmin, domain.AccountTypeAdmin, domain.AccountTypeMember:
		default:
			return fmt.Errorf("allowed_account_types: %w", domain.ErrInvalidArgument)
		}
	}
	for _, role := range policy.AllowedDelegatedRoles {
		if !domain.IsValidDelegatedRole(role) {
			return fmt.Errorf("allowed_delegated_roles: %w", domain.ErrInvalidArgument)
		}
	}
	for _, userID := range policy.AllowedUserIDs {
		user, err := s.userRepo.Get(ctx, userID)
		if err != nil {
			return fmt.Errorf("allowed_user_ids: %w", err)
		}
		if user.WorkspaceID != workspaceID {
			return fmt.Errorf("allowed_user_ids: %w", domain.ErrInvalidArgument)
		}
	}
	return nil
}

func (s *ConversationAccessService) actorMatchesCustomPostingPolicy(ctx context.Context, workspaceID, actorID string, policy *domain.ConversationPostingPolicy) (bool, error) {
	if containsString(policy.AllowedUserIDs, actorID) {
		return true, nil
	}
	actor, err := s.userRepo.Get(ctx, actorID)
	if err != nil {
		return false, err
	}
	if actor.WorkspaceID != workspaceID {
		return false, domain.ErrForbidden
	}
	if containsAccountType(policy.AllowedAccountTypes, actor.EffectiveAccountType()) {
		return true, nil
	}
	if s.roleRepo != nil {
		roles, err := s.roleRepo.ListByUser(ctx, workspaceID, actorID)
		if err == nil {
			for _, role := range roles {
				if containsDelegatedRole(policy.AllowedDelegatedRoles, role) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func dedupeAccountTypes(values []domain.AccountType) []domain.AccountType {
	seen := make(map[domain.AccountType]struct{}, len(values))
	out := make([]domain.AccountType, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func dedupeDelegatedRoles(values []domain.DelegatedRole) []domain.DelegatedRole {
	seen := make(map[domain.DelegatedRole]struct{}, len(values))
	out := make([]domain.DelegatedRole, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAccountType(values []domain.AccountType, target domain.AccountType) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsDelegatedRole(values []domain.DelegatedRole, target domain.DelegatedRole) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func diffStringSets(before, after []string) (added, removed []string) {
	beforeSet := make(map[string]struct{}, len(before))
	afterSet := make(map[string]struct{}, len(after))
	for _, value := range before {
		beforeSet[value] = struct{}{}
	}
	for _, value := range after {
		afterSet[value] = struct{}{}
		if _, ok := beforeSet[value]; !ok {
			added = append(added, value)
		}
	}
	for _, value := range before {
		if _, ok := afterSet[value]; !ok {
			removed = append(removed, value)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}
