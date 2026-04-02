package service

import (
	"context"
	"errors"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestResolveAuthContextUserSelectsWorkspaceLocalUserForAccount(t *testing.T) {
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_T1": {
				ID:          "U_T1",
				AccountID:   "A123",
				WorkspaceID: "T1",
			},
			"U_T2": {
				ID:          "U_T2",
				AccountID:   "A123",
				WorkspaceID: "T2",
			},
		},
	}

	user, err := resolveAuthContextUser(context.Background(), userRepo, &domain.AuthContext{
		AccountID:   "A123",
		WorkspaceID: "T2",
	})
	if err != nil {
		t.Fatalf("resolveAuthContextUser() error = %v", err)
	}
	if user.ID != "U_T2" {
		t.Fatalf("resolveAuthContextUser() selected user %q, want U_T2", user.ID)
	}
}

func TestMockUserRepoMapAllowsOneAccountAcrossMultipleWorkspaces(t *testing.T) {
	userRepo := &mockUserRepoMap{
		users: map[string]*domain.User{
			"U_T1": {
				ID:          "U_T1",
				AccountID:   "A123",
				WorkspaceID: "T1",
			},
			"U_T2": {
				ID:          "U_T2",
				AccountID:   "A123",
				WorkspaceID: "T2",
			},
			"U_OTHER": {
				ID:          "U_OTHER",
				AccountID:   "A999",
				WorkspaceID: "T1",
			},
		},
	}

	users, err := userRepo.ListByAccount(context.Background(), "A123")
	if err != nil {
		t.Fatalf("ListByAccount() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListByAccount() returned %d users, want 2", len(users))
	}

	for _, tc := range []struct {
		workspaceID string
		wantUserID  string
	}{
		{workspaceID: "T1", wantUserID: "U_T1"},
		{workspaceID: "T2", wantUserID: "U_T2"},
	} {
		user, err := userRepo.GetByWorkspaceAndAccount(context.Background(), tc.workspaceID, "A123")
		if err != nil {
			t.Fatalf("GetByWorkspaceAndAccount(%q) error = %v", tc.workspaceID, err)
		}
		if user.ID != tc.wantUserID {
			t.Fatalf("GetByWorkspaceAndAccount(%q) = %q, want %q", tc.workspaceID, user.ID, tc.wantUserID)
		}
	}

	_, err = userRepo.GetByWorkspaceAndAccount(context.Background(), "T3", "A123")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByWorkspaceAndAccount(T3) error = %v, want ErrNotFound", err)
	}
}
