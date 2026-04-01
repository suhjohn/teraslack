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

func (m *mockUserRepoDefault) GetByTeamEmail(_ context.Context, _, _ string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoDefault) ListByEmail(_ context.Context, _ string) ([]domain.User, error) {
	return nil, nil
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

func (m *mockUserRepoMap) GetByTeamEmail(_ context.Context, _, _ string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}

func (m *mockUserRepoMap) ListByEmail(_ context.Context, _ string) ([]domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoMap) Update(_ context.Context, _ string, _ domain.UpdateUserParams) (*domain.User, error) {
	return nil, nil
}

func (m *mockUserRepoMap) List(_ context.Context, _ domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	return nil, nil
}

func (m *mockUserRepoMap) WithTx(_ pgx.Tx) repository.UserRepository { return m }
