package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// mockUsergroupRepo is a mock implementation of repository.UsergroupRepository.
type mockUsergroupRepo struct {
	groups map[string]*domain.Usergroup
	users  map[string][]string
}

func newMockUsergroupRepo() *mockUsergroupRepo {
	return &mockUsergroupRepo{
		groups: make(map[string]*domain.Usergroup),
		users:  make(map[string][]string),
	}
}

func (m *mockUsergroupRepo) Create(_ context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	ug := &domain.Usergroup{
		ID:        "S123",
		TeamID:    params.TeamID,
		Name:      params.Name,
		Handle:    params.Handle,
		Description: params.Description,
		Enabled:   true,
		CreatedBy: params.CreatedBy,
		UpdatedBy: params.CreatedBy,
	}
	m.groups[ug.ID] = ug
	return ug, nil
}

func (m *mockUsergroupRepo) Get(_ context.Context, id string) (*domain.Usergroup, error) {
	ug, ok := m.groups[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return ug, nil
}

func (m *mockUsergroupRepo) Update(_ context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error) {
	ug, ok := m.groups[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if params.Name != nil {
		ug.Name = *params.Name
	}
	if params.Handle != nil {
		ug.Handle = *params.Handle
	}
	if params.Description != nil {
		ug.Description = *params.Description
	}
	return ug, nil
}

func (m *mockUsergroupRepo) List(_ context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	var result []domain.Usergroup
	for _, ug := range m.groups {
		if ug.TeamID == params.TeamID {
			if !params.IncludeDisabled && !ug.Enabled {
				continue
			}
			result = append(result, *ug)
		}
	}
	if result == nil {
		result = []domain.Usergroup{}
	}
	return result, nil
}

func (m *mockUsergroupRepo) Enable(_ context.Context, id string) error {
	ug, ok := m.groups[id]
	if !ok {
		return domain.ErrNotFound
	}
	ug.Enabled = true
	return nil
}

func (m *mockUsergroupRepo) Disable(_ context.Context, id string) error {
	ug, ok := m.groups[id]
	if !ok {
		return domain.ErrNotFound
	}
	ug.Enabled = false
	return nil
}

func (m *mockUsergroupRepo) AddUser(_ context.Context, usergroupID, userID string) error {
	m.users[usergroupID] = append(m.users[usergroupID], userID)
	return nil
}

func (m *mockUsergroupRepo) ListUsers(_ context.Context, usergroupID string) ([]string, error) {
	users := m.users[usergroupID]
	if users == nil {
		users = []string{}
	}
	return users, nil
}

func (m *mockUsergroupRepo) SetUsers(_ context.Context, usergroupID string, userIDs []string) error {
	m.users[usergroupID] = userIDs
	return nil
}

func (m *mockUsergroupRepo) WithTx(_ pgx.Tx) repository.UsergroupRepository { return m }

// mockUserRepoForUG is a minimal user repo mock for usergroup tests.
type mockUserRepoForUG struct{}

func (m *mockUserRepoForUG) Create(_ context.Context, _ domain.CreateUserParams) (*domain.User, error) {
	return nil, nil
}
func (m *mockUserRepoForUG) Get(_ context.Context, id string) (*domain.User, error) {
	return &domain.User{ID: id}, nil
}
func (m *mockUserRepoForUG) GetByEmail(_ context.Context, _ string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}
func (m *mockUserRepoForUG) Update(_ context.Context, _ string, _ domain.UpdateUserParams) (*domain.User, error) {
	return nil, nil
}
func (m *mockUserRepoForUG) List(_ context.Context, _ domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	return nil, nil
}
func (m *mockUserRepoForUG) WithTx(_ pgx.Tx) repository.UserRepository { return m }

func TestUsergroupService_Create(t *testing.T) {
	repo := newMockUsergroupRepo()
	svc := NewUsergroupService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	tests := []struct {
		name    string
		params  domain.CreateUsergroupParams
		wantErr bool
	}{
		{
			name: "valid create",
			params: domain.CreateUsergroupParams{
				TeamID:    "T123",
				Name:      "Engineering",
				Handle:    "engineering",
				CreatedBy: "U123",
			},
			wantErr: false,
		},
		{
			name: "missing team_id",
			params: domain.CreateUsergroupParams{
				Name:      "Engineering",
				Handle:    "engineering",
				CreatedBy: "U123",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			params: domain.CreateUsergroupParams{
				TeamID:    "T123",
				Handle:    "engineering",
				CreatedBy: "U123",
			},
			wantErr: true,
		},
		{
			name: "missing handle",
			params: domain.CreateUsergroupParams{
				TeamID:    "T123",
				Name:      "Engineering",
				CreatedBy: "U123",
			},
			wantErr: true,
		},
		{
			name: "missing created_by",
			params: domain.CreateUsergroupParams{
				TeamID: "T123",
				Name:   "Engineering",
				Handle: "engineering",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ug, err := svc.Create(context.Background(), tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ug.Name != tt.params.Name {
				t.Errorf("got name %q, want %q", ug.Name, tt.params.Name)
			}
			if ug.Handle != tt.params.Handle {
				t.Errorf("got handle %q, want %q", ug.Handle, tt.params.Handle)
			}
		})
	}
}

func TestUsergroupService_SetUsers(t *testing.T) {
	repo := newMockUsergroupRepo()
	svc := NewUsergroupService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	// Create a group first
	ug, err := svc.Create(context.Background(), domain.CreateUsergroupParams{
		TeamID:    "T123",
		Name:      "Team",
		Handle:    "team",
		CreatedBy: "U123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Set users
	err = svc.SetUsers(context.Background(), ug.ID, []string{"U1", "U2", "U3"})
	if err != nil {
		t.Fatalf("set users: %v", err)
	}

	// List users
	users, err := svc.ListUsers(context.Background(), ug.ID)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("got %d users, want 3", len(users))
	}

	// Set users on non-existent group
	err = svc.SetUsers(context.Background(), "S_NONEXISTENT", []string{"U1"})
	if err == nil {
		t.Fatal("expected error for non-existent group")
	}
}

func TestUsergroupService_EnableDisable(t *testing.T) {
	repo := newMockUsergroupRepo()
	svc := NewUsergroupService(repo, &mockUserRepoForUG{}, nil, mockTxBeginner{}, nil)

	ug, err := svc.Create(context.Background(), domain.CreateUsergroupParams{
		TeamID:    "T123",
		Name:      "Team",
		Handle:    "team",
		CreatedBy: "U123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Disable(context.Background(), ug.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if err := svc.Enable(context.Background(), ug.ID); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// Error cases
	if err := svc.Enable(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := svc.Disable(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty id")
	}
}
