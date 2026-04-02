package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockFileRepo struct {
	files map[string]*domain.File

	lastCreateFile        *domain.File
	lastGetWorkspaceID    string
	lastGetByID           string
	lastUpdateWorkspaceID string
	lastDeleteWorkspaceID string
	lastListParams        domain.ListFilesParams
	lastShareWorkspaceID  string
	lastShareFileID       string
	lastShareChannelID    string
	shareErr              error
}

func newMockFileRepo() *mockFileRepo {
	return &mockFileRepo{files: make(map[string]*domain.File)}
}

func (m *mockFileRepo) WithTx(_ pgx.Tx) repository.FileRepository { return m }

func (m *mockFileRepo) Create(_ context.Context, f *domain.File) error {
	copy := *f
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now()
	}
	if copy.UpdatedAt.IsZero() {
		copy.UpdatedAt = copy.CreatedAt
	}
	m.files[copy.ID] = &copy
	m.lastCreateFile = &copy
	return nil
}

func (m *mockFileRepo) Get(_ context.Context, workspaceID, id string) (*domain.File, error) {
	m.lastGetWorkspaceID = workspaceID
	f, ok := m.files[id]
	if !ok || f.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	copy := *f
	return &copy, nil
}

func (m *mockFileRepo) GetByID(_ context.Context, id string) (*domain.File, error) {
	m.lastGetByID = id
	f, ok := m.files[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := *f
	return &copy, nil
}

func (m *mockFileRepo) Update(_ context.Context, workspaceID string, f *domain.File) error {
	m.lastUpdateWorkspaceID = workspaceID
	existing, ok := m.files[f.ID]
	if !ok || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	copy := *f
	m.files[f.ID] = &copy
	return nil
}

func (m *mockFileRepo) Delete(_ context.Context, workspaceID, id string) error {
	m.lastDeleteWorkspaceID = workspaceID
	f, ok := m.files[id]
	if !ok || f.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(m.files, id)
	return nil
}

func (m *mockFileRepo) List(_ context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	m.lastListParams = params
	items := make([]domain.File, 0, len(m.files))
	for _, f := range m.files {
		if f.WorkspaceID != params.WorkspaceID {
			continue
		}
		if params.ChannelID != "" {
			found := false
			for _, channelID := range f.Channels {
				if channelID == params.ChannelID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if params.UserID != "" && f.UserID != params.UserID {
			continue
		}
		items = append(items, *f)
	}
	return &domain.CursorPage[domain.File]{Items: items, HasMore: false}, nil
}

func (m *mockFileRepo) ShareToChannel(_ context.Context, workspaceID, fileID, channelID string) error {
	m.lastShareWorkspaceID = workspaceID
	m.lastShareFileID = fileID
	m.lastShareChannelID = channelID
	if m.shareErr != nil {
		return m.shareErr
	}
	f, ok := m.files[fileID]
	if !ok || f.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	return nil
}

type captureRecorder struct {
	events []domain.InternalEvent
}

func (r *captureRecorder) Record(_ context.Context, event domain.InternalEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *captureRecorder) WithTx(_ pgx.Tx) EventRecorder { return r }

func TestFileService_AddRemoteFile_UsesAuthContext(t *testing.T) {
	repo := newMockFileRepo()
	recorder := &captureRecorder{}
	svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
	file, err := svc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title:       "Design Doc",
		ExternalURL: "https://example.com/design",
		Filetype:    "gdoc",
		UserID:      "should-not-win",
	})
	if err != nil {
		t.Fatalf("add remote file: %v", err)
	}
	if file.WorkspaceID != "T123" {
		t.Fatalf("workspace_id = %q, want T123", file.WorkspaceID)
	}
	if file.UserID != "U456" {
		t.Fatalf("user_id = %q, want U456", file.UserID)
	}
	if repo.lastCreateFile == nil || repo.lastCreateFile.WorkspaceID != "T123" {
		t.Fatalf("repo create did not persist workspace_id")
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected 1 recorded event, got %d", len(recorder.events))
	}
	if recorder.events[0].WorkspaceID != "T123" {
		t.Fatalf("event workspace_id = %q, want T123", recorder.events[0].WorkspaceID)
	}
	var payload domain.File
	if err := json.Unmarshal(recorder.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal event payload: %v", err)
	}
	if payload.WorkspaceID != "T123" {
		t.Fatalf("payload workspace_id = %q, want T123", payload.WorkspaceID)
	}
}

func TestFileService_AddRemoteFile_AllowsExternalMemberSharedWrite(t *testing.T) {
	repo := newMockFileRepo()
	recorder := &captureRecorder{}
	svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"C123|A123": {
				ID:                  "EM123",
				ConversationID:      "C123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionFilesWrite},
			},
		},
	})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U_EXT", "T999"), "A123", "")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	file, err := svc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title:       "Design Doc",
		ExternalURL: "https://example.com/design",
		Filetype:    "gdoc",
		ChannelID:   "C123",
	})
	if err != nil {
		t.Fatalf("add remote file: %v", err)
	}
	if file.WorkspaceID != "T123" {
		t.Fatalf("workspace_id = %q, want T123", file.WorkspaceID)
	}
	if file.UserID != "U_EXT" {
		t.Fatalf("user_id = %q, want U_EXT", file.UserID)
	}
	if repo.lastShareWorkspaceID != "T123" || repo.lastShareChannelID != "C123" {
		t.Fatalf("share target = %q/%q, want T123/C123", repo.lastShareWorkspaceID, repo.lastShareChannelID)
	}
	if len(recorder.events) != 2 {
		t.Fatalf("expected 2 recorded events, got %d", len(recorder.events))
	}
	if recorder.events[1].EventType != domain.EventFileShared {
		t.Fatalf("second event type = %q, want %q", recorder.events[1].EventType, domain.EventFileShared)
	}
}

func TestFileService_AddRemoteFile_ExternalMemberRequiresChannel(t *testing.T) {
	repo := newMockFileRepo()
	svc := NewFileService(repo, nil, "", "http://localhost:8080", &captureRecorder{}, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"C123|A123": {
				ID:                  "EM123",
				ConversationID:      "C123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionFilesWrite},
			},
		},
	})

	ctx := ctxutil.WithIdentity(ctxutil.WithUser(context.Background(), "U_EXT", "T999"), "A123", "")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	if _, err := svc.AddRemoteFile(ctx, domain.AddRemoteFileParams{
		Title:       "Design Doc",
		ExternalURL: "https://example.com/design",
		Filetype:    "gdoc",
	}); err == nil {
		t.Fatal("expected external member remote file creation without channel to be forbidden")
	}
}

func TestFileService_FileOperations_UseTeamContext(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U1", Name: "doc"}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", nil, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		f, err := svc.Get(ctx, "F_1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if f.WorkspaceID != "T123" {
			t.Fatalf("workspace_id = %q, want T123", f.WorkspaceID)
		}
		if repo.lastGetByID != "F_1" {
			t.Fatalf("repo saw file_id = %q, want F_1", repo.lastGetByID)
		}
	})

	t.Run("list", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U1", Name: "doc"}
		repo.files["F_2"] = &domain.File{ID: "F_2", WorkspaceID: "T999", UserID: "U2", Name: "other"}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", nil, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		page, err := svc.List(ctx, domain.ListFilesParams{Limit: 100})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if repo.lastListParams.WorkspaceID != "T123" {
			t.Fatalf("repo saw workspace_id = %q, want T123", repo.lastListParams.WorkspaceID)
		}
		if len(page.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(page.Items))
		}
	})

	t.Run("delete", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U456", Name: "doc"}
		recorder := &captureRecorder{}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		if err := svc.Delete(ctx, "F_1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if repo.lastDeleteWorkspaceID != "T123" {
			t.Fatalf("repo saw workspace_id = %q, want T123", repo.lastDeleteWorkspaceID)
		}
		if len(recorder.events) != 1 || recorder.events[0].WorkspaceID != "T123" {
			t.Fatalf("delete event workspace_id = %q, want T123", recorder.events[0].WorkspaceID)
		}
	})

	t.Run("share", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U456", Name: "doc"}
		recorder := &captureRecorder{}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		if err := svc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{FileID: "F_1", Channels: []string{"C1"}}); err != nil {
			t.Fatalf("share: %v", err)
		}
		if repo.lastShareWorkspaceID != "T123" {
			t.Fatalf("repo saw workspace_id = %q, want T123", repo.lastShareWorkspaceID)
		}
		if len(recorder.events) != 1 || recorder.events[0].WorkspaceID != "T123" {
			t.Fatalf("share event workspace_id = %q, want T123", recorder.events[0].WorkspaceID)
		}
	})
}

func TestFileService_Get_AllowsExternalMemberSharedFile(t *testing.T) {
	repo := newMockFileRepo()
	repo.files["F_1"] = &domain.File{
		ID:          "F_1",
		WorkspaceID: "T123",
		UserID:      "U_HOST",
		Name:        "doc",
		Channels:    []string{"C123"},
	}
	svc := NewFileService(repo, nil, "", "http://localhost:8080", nil, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"C123|A123": {
				ID:                  "EM123",
				ConversationID:      "C123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionFilesRead},
			},
		},
	})

	ctx := ctxutil.WithUser(context.Background(), "U_EXT", "T999")
	ctx = ctxutil.WithIdentity(ctx, "A123", "WM_EXT")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	f, err := svc.Get(ctx, "F_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if f.ID != "F_1" {
		t.Fatalf("file id = %q, want F_1", f.ID)
	}
}

func TestFileService_List_AllowsExternalMemberChannelScope(t *testing.T) {
	repo := newMockFileRepo()
	repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U1", Name: "doc", Channels: []string{"C123"}}
	svc := NewFileService(repo, nil, "", "http://localhost:8080", nil, mockTxBeginner{}, nil)
	svc.SetExternalMemberRepository(&externalMemberRepoStub{
		byConversationAccount: map[string]*domain.ExternalMember{
			"C123|A123": {
				ID:                  "EM123",
				ConversationID:      "C123",
				HostWorkspaceID:     "T123",
				ExternalWorkspaceID: "T999",
				AccountID:           "A123",
				AccessMode:          domain.ExternalPrincipalAccessModeShared,
				AllowedCapabilities: []string{domain.PermissionFilesRead},
			},
		},
	})

	ctx := ctxutil.WithUser(context.Background(), "U_EXT", "T999")
	ctx = ctxutil.WithIdentity(ctx, "A123", "WM_EXT")
	ctx = ctxutil.WithPrincipal(ctx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)

	page, err := svc.List(ctx, domain.ListFilesParams{ChannelID: "C123", Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.lastListParams.WorkspaceID != "T123" {
		t.Fatalf("repo saw workspace_id = %q, want T123", repo.lastListParams.WorkspaceID)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
}

func TestFileService_ShareRemoteFile_DuplicateShareDoesNotRecordEvent(t *testing.T) {
	repo := newMockFileRepo()
	repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U456", Name: "doc"}
	repo.shareErr = domain.ErrAlreadyShared
	recorder := &captureRecorder{}
	svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)

	ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
	if err := svc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{FileID: "F_1", Channels: []string{"C1"}}); err != nil {
		t.Fatalf("share: %v", err)
	}
	if len(recorder.events) != 0 {
		t.Fatalf("expected no events for duplicate share, got %d", len(recorder.events))
	}
}

func TestFileService_DeleteAndShareRequireOwnerOrAdmin(t *testing.T) {
	repo := newMockFileRepo()
	repo.files["F_1"] = &domain.File{ID: "F_1", WorkspaceID: "T123", UserID: "U_OWNER", Name: "doc"}
	svc := NewFileService(repo, nil, "", "http://localhost:8080", &captureRecorder{}, mockTxBeginner{}, nil)

	memberCtx := ctxutil.WithUser(context.Background(), "U_MEMBER", "T123")
	memberCtx = ctxutil.WithPrincipal(memberCtx, domain.PrincipalTypeHuman, domain.AccountTypeMember, false)
	if err := svc.Delete(memberCtx, "F_1"); err == nil {
		t.Fatal("expected non-owner member delete to be forbidden")
	}
	if err := svc.ShareRemoteFile(memberCtx, domain.ShareRemoteFileParams{FileID: "F_1", Channels: []string{"C1"}}); err == nil {
		t.Fatal("expected non-owner member share to be forbidden")
	}

	adminCtx := ctxutil.WithUser(context.Background(), "U_ADMIN", "T123")
	adminCtx = ctxutil.WithPrincipal(adminCtx, domain.PrincipalTypeHuman, domain.AccountTypeAdmin, false)
	if err := svc.ShareRemoteFile(adminCtx, domain.ShareRemoteFileParams{FileID: "F_1", Channels: []string{"C1"}}); err != nil {
		t.Fatalf("admin share should succeed: %v", err)
	}
}
