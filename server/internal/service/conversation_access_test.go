package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type conversationAccessRepoStub struct {
	managers map[string][]domain.ConversationManagerAssignment
	policies map[string]*domain.ConversationPostingPolicy
}

func (r *conversationAccessRepoStub) WithTx(tx pgx.Tx) repository.ConversationAccessRepository {
	return r
}

func (r *conversationAccessRepoStub) ListManagers(ctx context.Context, conversationID string) ([]domain.ConversationManagerAssignment, error) {
	if r.managers == nil {
		return []domain.ConversationManagerAssignment{}, nil
	}
	return append([]domain.ConversationManagerAssignment(nil), r.managers[conversationID]...), nil
}

func (r *conversationAccessRepoStub) ReplaceManagers(ctx context.Context, conversationID string, userIDs []string, assignedBy string) error {
	if r.managers == nil {
		r.managers = map[string][]domain.ConversationManagerAssignment{}
	}
	assignments := make([]domain.ConversationManagerAssignment, 0, len(userIDs))
	for _, userID := range userIDs {
		assignments = append(assignments, domain.ConversationManagerAssignment{
			ConversationID: conversationID,
			UserID:         userID,
			AssignedBy:     assignedBy,
			CreatedAt:      time.Now(),
		})
	}
	r.managers[conversationID] = assignments
	return nil
}

func (r *conversationAccessRepoStub) IsManager(ctx context.Context, conversationID, userID string) (bool, error) {
	for _, assignment := range r.managers[conversationID] {
		if assignment.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (r *conversationAccessRepoStub) GetPostingPolicy(ctx context.Context, conversationID string) (*domain.ConversationPostingPolicy, error) {
	if r.policies == nil {
		return nil, nil
	}
	return r.policies[conversationID], nil
}

func (r *conversationAccessRepoStub) UpsertPostingPolicy(ctx context.Context, policy domain.ConversationPostingPolicy) (*domain.ConversationPostingPolicy, error) {
	if r.policies == nil {
		r.policies = map[string]*domain.ConversationPostingPolicy{}
	}
	copy := policy
	if copy.UpdatedAt.IsZero() {
		copy.UpdatedAt = time.Now()
	}
	r.policies[policy.ConversationID] = &copy
	return &copy, nil
}

type roleAssignmentRepoStub struct {
	roles map[string][]domain.DelegatedRole
}

func (r *roleAssignmentRepoStub) WithTx(tx pgx.Tx) repository.RoleAssignmentRepository { return r }

func (r *roleAssignmentRepoStub) ListByUser(ctx context.Context, workspaceID, userID string) ([]domain.DelegatedRole, error) {
	return append([]domain.DelegatedRole(nil), r.roles[userID]...), nil
}

func (r *roleAssignmentRepoStub) ReplaceForUser(ctx context.Context, workspaceID, userID string, roles []domain.DelegatedRole, assignedBy string) error {
	if r.roles == nil {
		r.roles = map[string][]domain.DelegatedRole{}
	}
	r.roles[userID] = append([]domain.DelegatedRole(nil), roles...)
	return nil
}

func TestConversationAccessService_SetManagers_AllowsChannelsAdminOnPrivateChannel(t *testing.T) {
	repo := &conversationAccessRepoStub{}
	convRepo := &conversationRepoStub{
		conversation: &domain.Conversation{
			ID:        "G123",
			WorkspaceID:    "T123",
			Type:      domain.ConversationTypePrivateChannel,
			CreatorID: "U_CREATOR",
		},
		isMember: true,
	}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_ACTOR": {ID: "U_ACTOR", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
			"U_MGR":   {ID: "U_MGR", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}
	roleRepo := &roleAssignmentRepoStub{
		roles: map[string][]domain.DelegatedRole{
			"U_ACTOR": {domain.DelegatedRoleChannelsAdmin},
		},
	}
	svc := NewConversationAccessService(
		repo,
		convRepo,
		userRepo,
		roleRepo,
		nil,
		mockTxBeginner{},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	ctx := ctxutil.WithUser(context.Background(), "U_ACTOR", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	got, err := svc.SetManagers(ctx, "G123", []string{"U_MGR"})
	if err != nil {
		t.Fatalf("SetManagers() error = %v", err)
	}
	if len(got) != 1 || got[0] != "U_MGR" {
		t.Fatalf("SetManagers() managers = %v, want [U_MGR]", got)
	}
}

func TestMessageService_PostMessage_DeniesAdminsOnlyPolicyForMember(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T123", Type: domain.ConversationTypePublicChannel, CreatorID: "U_CREATOR"}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U123": {ID: "U123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}
	accessSvc := NewConversationAccessService(
		&conversationAccessRepoStub{
			policies: map[string]*domain.ConversationPostingPolicy{
				"C123": {ConversationID: "C123", PolicyType: domain.ConversationPostingPolicyAdminsOnly},
			},
		},
		&conversationRepoStub{conversation: conv},
		userRepo,
		nil,
		nil,
		mockTxBeginner{},
		nil,
	)
	msgSvc := NewMessageService(
		&messageRepoStub{created: &domain.Message{TS: "1", ChannelID: "C123", UserID: "U123", Text: "hello"}},
		&conversationRepoStub{conversation: conv},
		nil,
		mockTxBeginner{},
		nil,
	)
	msgSvc.SetAccessService(accessSvc)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	if _, err := msgSvc.PostMessage(ctx, domain.PostMessageParams{ChannelID: "C123", Text: "hello"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("PostMessage() error = %v, want forbidden", err)
	}
}

func TestMessageService_PostMessage_AllowsConversationManagerWhenRestricted(t *testing.T) {
	conv := &domain.Conversation{ID: "C123", WorkspaceID: "T123", Type: domain.ConversationTypePublicChannel, CreatorID: "U_CREATOR"}
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U123": {ID: "U123", WorkspaceID: "T123", PrincipalType: domain.PrincipalTypeHuman, AccountType: domain.AccountTypeMember},
		},
	}
	accessRepo := &conversationAccessRepoStub{
		managers: map[string][]domain.ConversationManagerAssignment{
			"C123": {{ConversationID: "C123", UserID: "U123", AssignedBy: "U_ADMIN"}},
		},
		policies: map[string]*domain.ConversationPostingPolicy{
			"C123": {ConversationID: "C123", PolicyType: domain.ConversationPostingPolicyMembersWithPermission},
		},
	}
	accessSvc := NewConversationAccessService(accessRepo, &conversationRepoStub{conversation: conv}, userRepo, nil, nil, mockTxBeginner{}, nil)
	msgSvc := NewMessageService(
		&messageRepoStub{created: &domain.Message{TS: "1", ChannelID: "C123", UserID: "U123", Text: "hello"}},
		&conversationRepoStub{conversation: conv},
		nil,
		mockTxBeginner{},
		nil,
	)
	msgSvc.SetAccessService(accessSvc)

	ctx := ctxutil.WithUser(context.Background(), "U123", "T123")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	if _, err := msgSvc.PostMessage(ctx, domain.PostMessageParams{ChannelID: "C123", Text: "hello"}); err != nil {
		t.Fatalf("PostMessage() error = %v, want nil", err)
	}
}
