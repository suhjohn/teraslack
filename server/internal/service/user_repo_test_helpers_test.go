package service

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockUserRepoDefault struct{}

func (m *mockUserRepoDefault) Create(_ context.Context, _ domain.CreateUserParams) (*domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoDefault) Get(_ context.Context, id string) (*domain.User, error) {
	return &domain.User{
		ID:            id,
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	}, nil
}

func (m *mockUserRepoDefault) GetByWorkspaceAndAccount(_ context.Context, _, _ string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoDefault) GetWorkspaceMembership(_ context.Context, workspaceID, accountID string) (*domain.WorkspaceMembership, error) {
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoDefault) GetWorkspaceMembershipID(_ context.Context, _, _ string) (string, error) {
	return "", domain.ErrNotFound
}

func (m *mockUserRepoDefault) ListWorkspaceMembershipsByAccount(_ context.Context, _ string) ([]domain.WorkspaceMembership, error) {
	return nil, nil
}

func (m *mockUserRepoDefault) ListByAccount(_ context.Context, _ string) ([]domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoDefault) GetByTeamEmail(_ context.Context, _, _ string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoDefault) Update(_ context.Context, _ string, _ domain.UpdateUserParams) (*domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoDefault) List(_ context.Context, _ domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	return nil, nil
}

func (m *mockUserRepoDefault) WithTx(_ pgx.Tx) repository.UserRepository { return m }

type mockUserRepoMap struct {
	users map[string]*domain.User
}

func (m *mockUserRepoMap) Create(_ context.Context, _ domain.CreateUserParams) (*domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoMap) Get(_ context.Context, id string) (*domain.User, error) {
	user, ok := m.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return user, nil
}

func (m *mockUserRepoMap) GetByWorkspaceAndAccount(_ context.Context, workspaceID, accountID string) (*domain.User, error) {
	for _, user := range m.users {
		if user.WorkspaceID == workspaceID && user.AccountID == accountID {
			return user, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoMap) GetWorkspaceMembership(_ context.Context, workspaceID, accountID string) (*domain.WorkspaceMembership, error) {
	for _, user := range m.users {
		if user.WorkspaceID == workspaceID && user.AccountID == accountID {
			return &domain.WorkspaceMembership{
				ID:             "WM_" + user.ID,
				WorkspaceID:    workspaceID,
				AccountID:      accountID,
				Role:           string(user.EffectiveAccountType()),
				Status:         domain.WorkspaceMembershipStatusActive,
				MembershipKind: domain.WorkspaceMembershipKindFull,
				GuestScope:     domain.WorkspaceGuestScopeWorkspaceFull,
			}, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoMap) GetWorkspaceMembershipID(_ context.Context, workspaceID, accountID string) (string, error) {
	for _, user := range m.users {
		if user.WorkspaceID == workspaceID && user.AccountID == accountID {
			return "WM_" + user.ID, nil
		}
	}
	return "", domain.ErrNotFound
}

func (m *mockUserRepoMap) ListWorkspaceMembershipsByAccount(_ context.Context, accountID string) ([]domain.WorkspaceMembership, error) {
	items := make([]domain.WorkspaceMembership, 0)
	for _, user := range m.users {
		if user.AccountID != accountID {
			continue
		}
		items = append(items, domain.WorkspaceMembership{
			ID:             "WM_" + user.ID,
			WorkspaceID:    user.WorkspaceID,
			AccountID:      user.AccountID,
			Role:           string(user.EffectiveAccountType()),
			Status:         domain.WorkspaceMembershipStatusActive,
			MembershipKind: domain.WorkspaceMembershipKindFull,
			GuestScope:     domain.WorkspaceGuestScopeWorkspaceFull,
		})
	}
	return items, nil
}

func (m *mockUserRepoMap) ListByAccount(_ context.Context, accountID string) ([]domain.User, error) {
	items := make([]domain.User, 0)
	for _, user := range m.users {
		if user.AccountID == accountID {
			items = append(items, *user)
		}
	}
	return items, nil
}

func (m *mockUserRepoMap) GetByTeamEmail(_ context.Context, workspaceID, email string) (*domain.User, error) {
	for _, user := range m.users {
		if user.WorkspaceID == workspaceID && user.Email == email {
			return user, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoMap) Update(_ context.Context, _ string, _ domain.UpdateUserParams) (*domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoMap) List(_ context.Context, _ domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	return nil, nil
}

func (m *mockUserRepoMap) WithTx(_ pgx.Tx) repository.UserRepository { return m }
