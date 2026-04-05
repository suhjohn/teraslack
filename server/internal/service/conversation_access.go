package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ConversationAccessService struct {
	repo      repository.ConversationAccessRepository
	convRepo  repository.ConversationRepository
	userRepo  repository.UserRepository
	roleRepo  repository.RoleAssignmentRepository
	auditRepo repository.AuthorizationAuditRepository
	recorder  EventRecorder
	db        repository.TxBeginner
	logger    *slog.Logger
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
		repo:     repo,
		convRepo: convRepo,
		userRepo: userRepo,
		roleRepo: roleRepo,
		recorder: recorder,
		db:       db,
		logger:   logger,
	}
}

func (s *ConversationAccessService) SetAuthorizationAuditRepository(repo repository.AuthorizationAuditRepository) {
	s.auditRepo = repo
}

func (s *ConversationAccessService) SetIdentityRepositories(_ ...any) {}

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
	accountIDs := make([]string, 0, len(assignments))
	seen := make(map[string]struct{}, len(assignments))
	for _, assignment := range assignments {
		accountID := assignment.AccountID
		if accountID == "" {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	sort.Strings(accountIDs)
	return accountIDs, nil
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
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if err := s.authorizeManagerMutation(ctx, conv); err != nil {
		return nil, err
	}

	_, normalizedAccountIDs, err := s.normalizeManagerIDs(ctx, conv, userIDs)
	if err != nil {
		return nil, err
	}
	assignedBy, err := resolveActorAccountID(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	if isAccountOwnedConversation(conv) && assignedBy == "" {
		assignedBy = conv.OwnerAccountID
	}
	if assignedBy == "" {
		assignedBy = conv.CreatorID
	}

	currentAssignments, err := s.repo.ListManagers(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	currentAccountIDs := make([]string, 0, len(currentAssignments))
	for _, assignment := range currentAssignments {
		accountID := assignment.AccountID
		if accountID != "" {
			currentAccountIDs = append(currentAccountIDs, accountID)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := s.repo.WithTx(tx)
	if err := txRepo.ReplaceManagers(ctx, conversationID, normalizedAccountIDs, assignedBy); err != nil {
		return nil, err
	}

	added, removed := diffStringSets(currentAccountIDs, normalizedAccountIDs)
	for _, accountID := range added {
		payload, _ := json.Marshal(map[string]string{
			"conversation_id": conversationID,
			"account_id":      accountID,
		})
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventConversationManagerAdded,
			AggregateType: domain.AggregateConversation,
			AggregateID:   conversationID,
			WorkspaceID:   conversationWorkspaceID(conv),
			ActorID:       assignedBy,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record conversation.manager_added event: %w", err)
		}
	}
	for _, accountID := range removed {
		payload, _ := json.Marshal(map[string]string{
			"conversation_id": conversationID,
			"account_id":      accountID,
		})
		if err := s.recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
			EventType:     domain.EventConversationManagerRemoved,
			AggregateType: domain.AggregateConversation,
			AggregateID:   conversationID,
			WorkspaceID:   conversationWorkspaceID(conv),
			ActorID:       assignedBy,
			Payload:       payload,
		}); err != nil {
			return nil, fmt.Errorf("record conversation.manager_removed event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, conversationWorkspaceID(conv), domain.AuditActionConversationManagersUpdated, "conversation", conversationID, map[string]any{
		"account_ids": normalizedAccountIDs,
	}); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	return normalizedAccountIDs, nil
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
	s.normalizePostingPolicy(ctx, conv, policy)
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
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return nil, err
	}
	if err := s.authorizePostingPolicyMutation(ctx, conv); err != nil {
		return nil, err
	}
	s.normalizePostingPolicy(ctx, conv, &policy)
	if err := s.validatePostingPolicy(ctx, conv, &policy); err != nil {
		return nil, err
	}

	policy.UpdatedBy, err = resolveActorAccountID(ctx, s.userRepo)
	if err != nil {
		return nil, err
	}
	if isAccountOwnedConversation(conv) && policy.UpdatedBy == "" {
		policy.UpdatedBy = conv.OwnerAccountID
	}
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
		WorkspaceID:   conversationWorkspaceID(conv),
		ActorID:       policy.UpdatedBy,
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record conversation.posting_policy.updated event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	if err := recordAuthorizationAudit(ctx, s.auditRepo, nil, conversationWorkspaceID(conv), domain.AuditActionPostingPolicyUpdated, "conversation", policy.ConversationID, updated); err != nil {
		return nil, fmt.Errorf("record authorization audit log: %w", err)
	}
	return updated, nil
}

func (s *ConversationAccessService) CanManageConversation(ctx context.Context, conv *domain.Conversation) error {
	if conv == nil {
		return fmt.Errorf("conversation: %w", domain.ErrInvalidArgument)
	}
	if isAccountOwnedConversation(conv) {
		if isInternalCallWithoutAuth(ctx) || isConversationOwnerAccount(ctx, conv) {
			return nil
		}
		return domain.ErrForbidden
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	isCreator, err := s.actorMatchesConversationCreator(ctx, conv)
	if err != nil {
		return err
	}
	if isCreator {
		return nil
	}
	if actorIsWorkspaceAdmin(ctx, s.userRepo, conversationWorkspaceID(conv)) {
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
	if isAccountOwnedConversation(conv) {
		if isInternalCallWithoutAuth(ctx) || isConversationOwnerAccount(ctx, conv) {
			return nil
		}
		return domain.ErrForbidden
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	isCreator, err := s.actorMatchesConversationCreator(ctx, conv)
	if err != nil {
		return err
	}
	if isCreator {
		return nil
	}
	if actorIsWorkspaceAdmin(ctx, s.userRepo, conversationWorkspaceID(conv)) {
		return nil
	}
	return domain.ErrForbidden
}

func (s *ConversationAccessService) actorMatchesConversationCreator(ctx context.Context, conv *domain.Conversation) (bool, error) {
	if conv == nil || strings.TrimSpace(conv.CreatorID) == "" {
		return false, nil
	}
	if accountID := actorAccountID(ctx); accountID != "" && s.userRepo != nil {
		creatorAccountID, err := resolveUserAccountID(ctx, s.userRepo, conv.CreatorID)
		if err != nil {
			return false, err
		}
		if creatorAccountID != "" {
			return creatorAccountID == accountID, nil
		}
	}
	return actorUserID(ctx) != "" && actorUserID(ctx) == conv.CreatorID, nil
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
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if isAccountOwnedConversation(conv) {
		accountID, err := resolveActorAccountID(ctx, s.userRepo)
		if err != nil {
			return err
		}
		if accountID == "" {
			return domain.ErrForbidden
		}
		if err := ensureAccountConversationAccess(ctx, s.convRepo, conv); err != nil {
			return err
		}

		policy, err := s.repo.GetPostingPolicy(ctx, conv.ID)
		if err != nil {
			return err
		}
		if policy == nil || policy.PolicyType == domain.ConversationPostingPolicyEveryone {
			return nil
		}
		switch policy.PolicyType {
		case domain.ConversationPostingPolicyAdminsOnly, domain.ConversationPostingPolicyMembersWithPermission:
			if isConversationOwnerAccount(ctx, conv) {
				return nil
			}
			isManager, err := s.repo.IsManager(ctx, conv.ID, accountID)
			if err != nil {
				return err
			}
			if isManager {
				return nil
			}
			return domain.ErrForbidden
		case domain.ConversationPostingPolicyCustom:
			if isConversationOwnerAccount(ctx, conv) || containsString(policy.AllowedAccountIDs, accountID) {
				return nil
			}
			return domain.ErrForbidden
		default:
			return domain.ErrForbidden
		}
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	if actorID == "" {
		actorID = actorUserID(ctx)
	}
	accountID, err := resolveActorAccountID(ctx, s.userRepo)
	if err != nil {
		return err
	}
	if actorID == "" {
		actorID = accountID
	}
	if actorID == "" {
		return domain.ErrForbidden
	}
	if actorIsWorkspaceAdmin(ctx, s.userRepo, conversationWorkspaceID(conv)) {
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
		isManager, err := s.repo.IsManager(ctx, conv.ID, accountID)
		if err != nil {
			return err
		}
		if isManager {
			return nil
		}
		return domain.ErrForbidden
	case domain.ConversationPostingPolicyCustom:
		allowed, err := s.actorMatchesCustomPostingPolicy(ctx, conversationWorkspaceID(conv), actorID, policy)
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
	if isAccountOwnedConversation(conv) {
		return ensureAccountConversationAccess(ctx, s.convRepo, conv)
	}
	if err := ensureWorkspaceMembershipAccess(ctx, s.userRepo, conversationWorkspaceID(conv)); err != nil {
		return err
	}
	switch conv.Type {
	case domain.ConversationTypePrivateChannel, domain.ConversationTypeIM, domain.ConversationTypeMPIM:
		if isInternalCallWithoutAuth(ctx) {
			return nil
		}
		if actorIsWorkspaceAdmin(ctx, s.userRepo, conversationWorkspaceID(conv)) {
			return nil
		}
		accountID, err := resolveActorAccountID(ctx, s.userRepo)
		if err != nil {
			return err
		}
		if accountID == "" {
			return domain.ErrForbidden
		}
		isMember, err := s.convRepo.IsAccountMember(ctx, conv.ID, accountID)
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
	if isAccountOwnedConversation(conv) {
		if isInternalCallWithoutAuth(ctx) || isConversationOwnerAccount(ctx, conv) {
			return nil
		}
		return domain.ErrForbidden
	}
	if isInternalCallWithoutAuth(ctx) {
		return nil
	}
	if actorIsWorkspaceAdmin(ctx, s.userRepo, conversationWorkspaceID(conv)) {
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
	if isAccountOwnedConversation(conv) {
		return false
	}
	membership, err := actorWorkspaceMembership(ctx, s.userRepo, conversationWorkspaceID(conv))
	if err != nil {
		return false
	}
	if !defaultAuthorizer.IsWorkspaceAdminAccount(membership.EffectiveAccountType()) {
		return false
	}
	if conv.Type != domain.ConversationTypePrivateChannel {
		return true
	}
	accountID := actorAccountID(ctx)
	if accountID == "" {
		return false
	}
	isMember, err := s.convRepo.IsAccountMember(ctx, conv.ID, accountID)
	if err != nil {
		return false
	}
	return isMember
}

func (s *ConversationAccessService) isActorConversationManager(ctx context.Context, conversationID string) (bool, error) {
	accountID, err := resolveActorAccountID(ctx, s.userRepo)
	if err != nil {
		return false, err
	}
	if accountID == "" {
		return false, nil
	}
	isManager, err := s.repo.IsManager(ctx, conversationID, accountID)
	if err != nil {
		return false, err
	}
	return isManager, nil
}

func (s *ConversationAccessService) normalizeManagerIDs(ctx context.Context, conv *domain.Conversation, ids []string) ([]string, []string, error) {
	seenAccounts := make(map[string]struct{}, len(ids))
	accountIDs := make([]string, 0, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if isAccountOwnedConversation(conv) {
			accountID := id
			if user, err := loadUser(ctx, s.userRepo, id); err == nil {
				if user.AccountID == "" {
					return nil, nil, fmt.Errorf("manager account_ids: %w", domain.ErrInvalidArgument)
				}
				accountID = user.AccountID
			}
			if _, ok := seenAccounts[accountID]; ok {
				continue
			}
			seenAccounts[accountID] = struct{}{}
			accountIDs = append(accountIDs, accountID)
			continue
		}

		if user, err := loadUser(ctx, s.userRepo, id); err == nil {
			if user.WorkspaceID != conversationWorkspaceID(conv) || user.PrincipalType != domain.PrincipalTypeHuman {
				return nil, nil, fmt.Errorf("manager user_ids: %w", domain.ErrInvalidArgument)
			}
			accountID := user.AccountID
			if accountID == "" {
				return nil, nil, fmt.Errorf("manager account_ids: %w", domain.ErrInvalidArgument)
			}
			if _, ok := seenAccounts[accountID]; !ok {
				seenAccounts[accountID] = struct{}{}
				accountIDs = append(accountIDs, accountID)
			}
			continue
		}

		if _, err := s.userRepo.GetWorkspaceMembershipID(ctx, conversationWorkspaceID(conv), id); err != nil {
			return nil, nil, fmt.Errorf("manager account_ids: %w", err)
		}
		if _, ok := seenAccounts[id]; !ok {
			seenAccounts[id] = struct{}{}
			accountIDs = append(accountIDs, id)
		}
	}
	sort.Strings(accountIDs)
	return nil, accountIDs, nil
}

func (s *ConversationAccessService) normalizePostingPolicy(ctx context.Context, conv *domain.Conversation, policy *domain.ConversationPostingPolicy) {
	_ = ctx
	_ = conv
	policy.AllowedAccountTypes = dedupeAccountTypes(policy.AllowedAccountTypes)
	policy.AllowedDelegatedRoles = dedupeDelegatedRoles(policy.AllowedDelegatedRoles)
	policy.AllowedAccountIDs = dedupeStrings(policy.AllowedAccountIDs)
	policy.AllowedUserIDs = nil
	if s.userRepo == nil {
		return
	}

	seen := make(map[string]struct{}, len(policy.AllowedAccountIDs))
	accountIDs := make([]string, 0, len(policy.AllowedAccountIDs))
	for _, accountID := range policy.AllowedAccountIDs {
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	sort.Strings(accountIDs)
	policy.AllowedAccountIDs = accountIDs
}

func (s *ConversationAccessService) normalizeAccountOwnedPostingPolicy(ctx context.Context, policy *domain.ConversationPostingPolicy) *domain.ConversationPostingPolicy {
	clone := *policy
	s.normalizePostingPolicy(ctx, &domain.Conversation{OwnerType: domain.ConversationOwnerTypeAccount}, &clone)
	return &clone
}

func (s *ConversationAccessService) validatePostingPolicy(ctx context.Context, conv *domain.Conversation, policy *domain.ConversationPostingPolicy) error {
	if !domain.IsValidConversationPostingPolicyType(policy.PolicyType) {
		return fmt.Errorf("policy_type: %w", domain.ErrInvalidArgument)
	}
	if isAccountOwnedConversation(conv) {
		for _, accountID := range policy.AllowedAccountIDs {
			if accountID == "" {
				return fmt.Errorf("allowed_account_ids: %w", domain.ErrInvalidArgument)
			}
		}
		return nil
	}
	workspaceID := conversationWorkspaceID(conv)
	if s.userRepo == nil && len(policy.AllowedAccountIDs) > 0 {
		return fmt.Errorf("allowed_account_ids: %w", domain.ErrInvalidArgument)
	}

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
	for _, accountID := range policy.AllowedAccountIDs {
		if accountID == "" {
			return fmt.Errorf("allowed_account_ids: %w", domain.ErrInvalidArgument)
		}
		if _, err := s.userRepo.GetWorkspaceMembershipID(ctx, workspaceID, accountID); err != nil {
			return fmt.Errorf("allowed_account_ids: %w", err)
		}
	}
	if len(policy.AllowedUserIDs) > 0 {
		return fmt.Errorf("allowed_user_ids: %w", domain.ErrInvalidArgument)
	}
	return nil
}

func (s *ConversationAccessService) actorMatchesCustomPostingPolicy(ctx context.Context, workspaceID, _ string, policy *domain.ConversationPostingPolicy) (bool, error) {
	if workspaceID == "" {
		accountID, err := resolveActorAccountID(ctx, s.userRepo)
		if err != nil {
			return false, err
		}
		return containsString(policy.AllowedAccountIDs, accountID), nil
	}
	accountID, err := resolveActorAccountID(ctx, s.userRepo)
	if err != nil {
		return false, err
	}
	if containsString(policy.AllowedAccountIDs, accountID) {
		return true, nil
	}
	membership, err := actorWorkspaceMembership(ctx, s.userRepo, workspaceID)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			return false, nil
		}
		return false, err
	}
	if membership == nil {
		return false, nil
	}
	if containsAccountType(policy.AllowedAccountTypes, membership.EffectiveAccountType()) {
		return true, nil
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
