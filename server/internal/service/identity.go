package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

func applyAccountIdentityToUser(user *domain.User, account *domain.Account) *domain.User {
	if user == nil || account == nil {
		return user
	}
	copy := *user
	copy.AccountID = firstNonEmptyString(strings.TrimSpace(copy.AccountID), strings.TrimSpace(account.ID))
	copy.Email = account.Email
	copy.PrincipalType = account.PrincipalType
	copy.IsBot = account.IsBot
	return &copy
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ensureAccountForUser(ctx context.Context, accountRepo repository.AccountRepository, user *domain.User) (*domain.Account, error) {
	if accountRepo == nil || user == nil {
		return nil, nil
	}
	return resolveOrCreateAccountForUser(ctx, accountRepo, user)
}

func resolveOrCreateAccountForUser(ctx context.Context, accountRepo repository.AccountRepository, user *domain.User) (*domain.Account, error) {
	if accountRepo == nil || user == nil {
		return nil, nil
	}
	if accountID := strings.TrimSpace(user.AccountID); accountID != "" {
		return accountRepo.Get(ctx, accountID)
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
		Email:         email,
		IsBot:         user.IsBot,
		Deleted:       user.Deleted,
	})
}

func resolveOrCreateAccountForCreateUserParams(ctx context.Context, accountRepo repository.AccountRepository, params domain.CreateUserParams) (*domain.Account, error) {
	if accountRepo == nil {
		return nil, nil
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
		Email:         email,
		IsBot:         params.IsBot,
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
		Email:         email,
	})
}

func loadUser(ctx context.Context, userRepo repository.UserRepository, userID string) (*domain.User, error) {
	if userRepo == nil {
		return nil, fmt.Errorf("user: %w", domain.ErrInvalidArgument)
	}
	return userRepo.Get(ctx, userID)
}

func resolveAuthContextUser(
	ctx context.Context,
	userRepo repository.UserRepository,
	auth *domain.AuthContext,
) (*domain.User, error) {
	if auth == nil {
		return nil, fmt.Errorf("auth: %w", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(auth.UserID) != "" {
		user, err := userRepo.Get(ctx, auth.UserID)
		if err != nil {
			return nil, err
		}
		return user, nil
	}
	if userRepo != nil && strings.TrimSpace(auth.WorkspaceID) != "" && strings.TrimSpace(auth.AccountID) != "" {
		user, err := userRepo.GetByWorkspaceAndAccount(ctx, auth.WorkspaceID, auth.AccountID)
		if err == nil {
			return user, nil
		}
		if err != nil && err != domain.ErrNotFound {
			return nil, err
		}
	}
	return nil, domain.ErrInvalidAuth
}

func createWorkspaceUserForAccount(
	ctx context.Context,
	userRepo repository.UserRepository,
	account *domain.Account,
	workspaceID string,
	accountType domain.AccountType,
	recorder EventRecorder,
) (*domain.User, error) {
	if userRepo == nil || account == nil {
		return nil, fmt.Errorf("workspace user: %w", domain.ErrInvalidArgument)
	}
	if recorder == nil {
		recorder = noopRecorder{}
	}

	name := emailLocalPart(account.Email)
	realName := name
	displayName := realName

	user, err := userRepo.Create(ctx, domain.CreateUserParams{
		AccountID:     account.ID,
		WorkspaceID:   workspaceID,
		Name:          name,
		RealName:      realName,
		DisplayName:   displayName,
		Email:         account.Email,
		PrincipalType: account.PrincipalType,
		AccountType:   accountType,
		IsBot:         account.IsBot,
		Profile:       domain.UserProfile{},
	})
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(user)
	if err := recorder.Record(ctx, domain.InternalEvent{
		EventType:     domain.EventUserCreated,
		AggregateType: domain.AggregateUser,
		AggregateID:   user.ID,
		WorkspaceID:   user.WorkspaceID,
		ActorID:       actorUserID(ctx),
		Payload:       payload,
	}); err != nil {
		return nil, fmt.Errorf("record user.created event: %w", err)
	}

	return user, nil
}
