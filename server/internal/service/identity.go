package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func syncIdentityForUser(ctx context.Context, accountRepo repository.AccountRepository, membershipRepo repository.WorkspaceMembershipRepository, user *domain.User) (*domain.Account, *domain.WorkspaceMembership, error) {
	if accountRepo == nil || membershipRepo == nil || user == nil {
		return nil, nil, nil
	}

	account, err := resolveOrCreateAccountForUser(ctx, accountRepo, user)
	if err != nil {
		return nil, nil, err
	}
	membership, err := ensureWorkspaceMembershipForUser(ctx, membershipRepo, account.ID, user)
	if err != nil {
		return nil, nil, err
	}
	return account, membership, nil
}

func resolveOrCreateAccountForUser(ctx context.Context, accountRepo repository.AccountRepository, user *domain.User) (*domain.Account, error) {
	if accountRepo == nil || user == nil {
		return nil, nil
	}
	email := strings.TrimSpace(user.Email)
	if email != "" {
		account, err := accountRepo.GetByEmail(ctx, email)
		if err == nil {
			return account, nil
		}
		if err != nil && err != domain.ErrNotFound {
			return nil, err
		}
	}
	return accountRepo.Create(ctx, domain.CreateAccountParams{
		PrincipalType: user.PrincipalType,
		Name:          user.Name,
		RealName:      user.RealName,
		DisplayName:   user.DisplayName,
		Email:         email,
		IsBot:         user.IsBot,
		Deleted:       user.Deleted,
		Profile:       user.Profile,
	})
}

func ensureWorkspaceMembershipForUser(ctx context.Context, membershipRepo repository.WorkspaceMembershipRepository, accountID string, user *domain.User) (*domain.WorkspaceMembership, error) {
	if membershipRepo == nil || user == nil || accountID == "" {
		return nil, nil
	}
	membership, err := membershipRepo.GetByLegacyUserID(ctx, user.ID)
	if err == nil {
		return membership, nil
	}
	if err != nil && err != domain.ErrNotFound {
		return nil, err
	}
	return membershipRepo.Create(ctx, domain.CreateWorkspaceMembershipParams{
		AccountID:   accountID,
		WorkspaceID: user.WorkspaceID,
		UserID:      user.ID,
		AccountType: user.EffectiveAccountType(),
	})
}

func resolveOrCreateAccount(ctx context.Context, accountRepo repository.AccountRepository, params domain.CreateExternalMemberParams) (*domain.Account, error) {
	if accountRepo == nil {
		return nil, domain.ErrInvalidArgument
	}
	if accountID := strings.TrimSpace(params.AccountID); accountID != "" {
		return accountRepo.Get(ctx, accountID)
	}
	email := strings.TrimSpace(params.Email)
	if email != "" {
		account, err := accountRepo.GetByEmail(ctx, email)
		if err == nil {
			return account, nil
		}
		if err != nil && err != domain.ErrNotFound {
			return nil, err
		}
	}
	return accountRepo.Create(ctx, domain.CreateAccountParams{
		PrincipalType: params.PrincipalType,
		Name:          strings.TrimSpace(params.Name),
		RealName:      strings.TrimSpace(params.RealName),
		DisplayName:   strings.TrimSpace(params.DisplayName),
		Email:         email,
	})
}

func decorateUserWithMembership(ctx context.Context, membershipRepo repository.WorkspaceMembershipRepository, user *domain.User) (*domain.User, error) {
	if user == nil || membershipRepo == nil {
		return user, nil
	}
	copy := *user
	membership, err := membershipRepo.GetByLegacyUserID(ctx, user.ID)
	if err != nil {
		if err == domain.ErrNotFound {
			return &copy, nil
		}
		return nil, err
	}
	if membership.WorkspaceID != "" {
		copy.WorkspaceID = membership.WorkspaceID
	}
	if membership.AccountType != "" {
		copy.AccountType = membership.AccountType
	}
	return &copy, nil
}

func loadUserWithMembership(ctx context.Context, userRepo repository.UserRepository, membershipRepo repository.WorkspaceMembershipRepository, userID string) (*domain.User, error) {
	user, err := userRepo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	return decorateUserWithMembership(ctx, membershipRepo, user)
}

func resolveAuthContextUser(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	accountRepo repository.AccountRepository,
	membershipRepo repository.WorkspaceMembershipRepository,
	auth *domain.AuthContext,
	recorder EventRecorder,
) (*domain.User, *domain.WorkspaceMembership, error) {
	if auth == nil {
		return nil, nil, fmt.Errorf("auth: %w", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(auth.UserID) != "" {
		user, err := loadUserWithMembership(ctx, userRepo, membershipRepo, auth.UserID)
		if err != nil {
			return nil, nil, err
		}
		if membershipRepo == nil {
			return user, nil, nil
		}
		membership, err := membershipRepo.GetByLegacyUserID(ctx, auth.UserID)
		if err != nil {
			if err == domain.ErrNotFound {
				return user, nil, nil
			}
			return nil, nil, err
		}
		return user, membership, nil
	}
	if membershipRepo == nil || accountRepo == nil || strings.TrimSpace(auth.WorkspaceID) == "" || strings.TrimSpace(auth.AccountID) == "" {
		return nil, nil, domain.ErrInvalidAuth
	}
	membership, err := membershipRepo.GetByWorkspaceAndAccount(ctx, auth.WorkspaceID, auth.AccountID)
	if err != nil {
		return nil, nil, err
	}
	user, membership, err := ensureMembershipUser(ctx, tx, userRepo, accountRepo, membershipRepo, membership, recorder)
	if err != nil {
		return nil, nil, err
	}
	return user, membership, nil
}

func ensureMembershipUser(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	accountRepo repository.AccountRepository,
	memberRepo repository.WorkspaceMembershipRepository,
	membership *domain.WorkspaceMembership,
	recorder EventRecorder,
) (*domain.User, *domain.WorkspaceMembership, error) {
	if userRepo == nil || accountRepo == nil || memberRepo == nil || membership == nil {
		return nil, nil, fmt.Errorf("membership user: %w", domain.ErrInvalidArgument)
	}

	if membership.UserID != "" {
		user, err := userRepo.Get(ctx, membership.UserID)
		if err != nil {
			return nil, nil, err
		}
		return user, membership, nil
	}

	account, err := accountRepo.Get(ctx, membership.AccountID)
	if err != nil {
		return nil, nil, err
	}

	user, err := createCompatibilityUserForAccount(ctx, tx, userRepo, account, membership.WorkspaceID, membership.AccountType, recorder)
	if err != nil {
		return nil, nil, err
	}
	attached, err := memberRepo.AttachUser(ctx, membership.ID, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, attached, nil
}

func compatibilityUserFromIdentity(account *domain.Account, membership *domain.WorkspaceMembership) *domain.User {
	if account == nil || membership == nil {
		return nil
	}

	name := strings.TrimSpace(account.Name)
	if name == "" {
		name = emailLocalPart(account.Email)
	}
	realName := strings.TrimSpace(account.RealName)
	if realName == "" {
		realName = name
	}
	displayName := strings.TrimSpace(account.DisplayName)
	if displayName == "" {
		displayName = realName
	}

	return &domain.User{
		ID:            membership.UserID,
		WorkspaceID:   membership.WorkspaceID,
		Name:          name,
		RealName:      realName,
		DisplayName:   displayName,
		Email:         account.Email,
		PrincipalType: account.PrincipalType,
		AccountType:   membership.AccountType,
		IsBot:         account.IsBot,
		Deleted:       account.Deleted,
		Profile:       account.Profile,
		CreatedAt:     membership.CreatedAt,
		UpdatedAt:     membership.UpdatedAt,
	}
}

func materializeMembershipUser(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	accountRepo repository.AccountRepository,
	memberRepo repository.WorkspaceMembershipRepository,
	membership *domain.WorkspaceMembership,
	recorder EventRecorder,
) (*domain.User, *domain.WorkspaceMembership, error) {
	if membership == nil {
		return nil, nil, domain.ErrNotFound
	}
	if membership.UserID != "" {
		user, err := userRepo.Get(ctx, membership.UserID)
		if err == nil {
			if membership.WorkspaceID != "" {
				user.WorkspaceID = membership.WorkspaceID
			}
			if membership.AccountType != "" {
				user.AccountType = membership.AccountType
			}
			return user, membership, nil
		}
		if err != nil && err != domain.ErrNotFound {
			return nil, nil, err
		}
	}

	user, attachedMembership, err := ensureMembershipUser(ctx, tx, userRepo, accountRepo, memberRepo, membership, recorder)
	if err != nil {
		return nil, nil, err
	}
	if attachedMembership.WorkspaceID != "" {
		user.WorkspaceID = attachedMembership.WorkspaceID
	}
	if attachedMembership.AccountType != "" {
		user.AccountType = attachedMembership.AccountType
	}
	return user, attachedMembership, nil
}

func loadMembershipBackedUser(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	accountRepo repository.AccountRepository,
	memberRepo repository.WorkspaceMembershipRepository,
	membership *domain.WorkspaceMembership,
	recorder EventRecorder,
) (*domain.User, *domain.WorkspaceMembership, error) {
	if userRepo == nil || accountRepo == nil || memberRepo == nil || membership == nil {
		return nil, nil, fmt.Errorf("membership user view: %w", domain.ErrInvalidArgument)
	}

	user, attachedMembership, err := materializeMembershipUser(ctx, tx, userRepo, accountRepo, memberRepo, membership, recorder)
	if err == nil {
		return user, attachedMembership, nil
	}
	if err != nil && err != domain.ErrNotFound {
		return nil, nil, err
	}

	account, accountErr := accountRepo.Get(ctx, membership.AccountID)
	if accountErr != nil {
		return nil, nil, accountErr
	}
	return compatibilityUserFromIdentity(account, membership), membership, nil
}

func createCompatibilityUserForAccount(
	ctx context.Context,
	tx pgx.Tx,
	userRepo repository.UserRepository,
	account *domain.Account,
	workspaceID string,
	accountType domain.AccountType,
	recorder EventRecorder,
) (*domain.User, error) {
	if userRepo == nil || account == nil {
		return nil, fmt.Errorf("compatibility user: %w", domain.ErrInvalidArgument)
	}
	if recorder == nil {
		recorder = noopRecorder{}
	}

	name := strings.TrimSpace(account.Name)
	if name == "" {
		name = emailLocalPart(account.Email)
	}
	realName := strings.TrimSpace(account.RealName)
	if realName == "" {
		realName = name
	}
	displayName := strings.TrimSpace(account.DisplayName)
	if displayName == "" {
		displayName = realName
	}

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspaceID,
		Name:          name,
		RealName:      realName,
		DisplayName:   displayName,
		Email:         account.Email,
		PrincipalType: account.PrincipalType,
		AccountType:   accountType,
		IsBot:         account.IsBot,
		Profile:       account.Profile,
	})
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(user)
	if err := recorder.WithTx(tx).Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		WorkspaceID:   user.WorkspaceID,
		ActorID:       compatibilityActorID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	return user, nil
}
