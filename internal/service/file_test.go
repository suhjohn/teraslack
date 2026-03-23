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

	lastCreateFile     *domain.File
	lastGetTeamID      string
	lastUpdateTeamID   string
	lastDeleteTeamID   string
	lastListParams     domain.ListFilesParams
	lastShareTeamID    string
	lastShareFileID    string
	lastShareChannelID string
	shareErr           error
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

func (m *mockFileRepo) Get(_ context.Context, teamID, id string) (*domain.File, error) {
	m.lastGetTeamID = teamID
	f, ok := m.files[id]
	if !ok || f.TeamID != teamID {
		return nil, domain.ErrNotFound
	}
	copy := *f
	return &copy, nil
}

func (m *mockFileRepo) Update(_ context.Context, teamID string, f *domain.File) error {
	m.lastUpdateTeamID = teamID
	existing, ok := m.files[f.ID]
	if !ok || existing.TeamID != teamID {
		return domain.ErrNotFound
	}
	copy := *f
	m.files[f.ID] = &copy
	return nil
}

func (m *mockFileRepo) Delete(_ context.Context, teamID, id string) error {
	m.lastDeleteTeamID = teamID
	f, ok := m.files[id]
	if !ok || f.TeamID != teamID {
		return domain.ErrNotFound
	}
	delete(m.files, id)
	return nil
}

func (m *mockFileRepo) List(_ context.Context, params domain.ListFilesParams) (*domain.CursorPage[domain.File], error) {
	m.lastListParams = params
	items := make([]domain.File, 0, len(m.files))
	for _, f := range m.files {
		if f.TeamID != params.TeamID {
			continue
		}
		if params.ChannelID != "" {
			continue
		}
		if params.UserID != "" && f.UserID != params.UserID {
			continue
		}
		items = append(items, *f)
	}
	return &domain.CursorPage[domain.File]{Items: items, HasMore: false}, nil
}

func (m *mockFileRepo) ShareToChannel(_ context.Context, teamID, fileID, channelID string) error {
	m.lastShareTeamID = teamID
	m.lastShareFileID = fileID
	m.lastShareChannelID = channelID
	if m.shareErr != nil {
		return m.shareErr
	}
	f, ok := m.files[fileID]
	if !ok || f.TeamID != teamID {
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
	if file.TeamID != "T123" {
		t.Fatalf("team_id = %q, want T123", file.TeamID)
	}
	if file.UserID != "U456" {
		t.Fatalf("user_id = %q, want U456", file.UserID)
	}
	if repo.lastCreateFile == nil || repo.lastCreateFile.TeamID != "T123" {
		t.Fatalf("repo create did not persist team_id")
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected 1 recorded event, got %d", len(recorder.events))
	}
	if recorder.events[0].TeamID != "T123" {
		t.Fatalf("event team_id = %q, want T123", recorder.events[0].TeamID)
	}
	var payload domain.File
	if err := json.Unmarshal(recorder.events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal event payload: %v", err)
	}
	if payload.TeamID != "T123" {
		t.Fatalf("payload team_id = %q, want T123", payload.TeamID)
	}
}

func TestFileService_FileOperations_UseTeamContext(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", TeamID: "T123", UserID: "U1", Name: "doc"}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", nil, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		f, err := svc.Get(ctx, "F_1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if f.TeamID != "T123" {
			t.Fatalf("team_id = %q, want T123", f.TeamID)
		}
		if repo.lastGetTeamID != "T123" {
			t.Fatalf("repo saw team_id = %q, want T123", repo.lastGetTeamID)
		}
	})

	t.Run("list", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", TeamID: "T123", UserID: "U1", Name: "doc"}
		repo.files["F_2"] = &domain.File{ID: "F_2", TeamID: "T999", UserID: "U2", Name: "other"}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", nil, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		page, err := svc.List(ctx, domain.ListFilesParams{Limit: 100})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if repo.lastListParams.TeamID != "T123" {
			t.Fatalf("repo saw team_id = %q, want T123", repo.lastListParams.TeamID)
		}
		if len(page.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(page.Items))
		}
	})

	t.Run("delete", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", TeamID: "T123", UserID: "U456", Name: "doc"}
		recorder := &captureRecorder{}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		if err := svc.Delete(ctx, "F_1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if repo.lastDeleteTeamID != "T123" {
			t.Fatalf("repo saw team_id = %q, want T123", repo.lastDeleteTeamID)
		}
		if len(recorder.events) != 1 || recorder.events[0].TeamID != "T123" {
			t.Fatalf("delete event team_id = %q, want T123", recorder.events[0].TeamID)
		}
	})

	t.Run("share", func(t *testing.T) {
		repo := newMockFileRepo()
		repo.files["F_1"] = &domain.File{ID: "F_1", TeamID: "T123", UserID: "U456", Name: "doc"}
		recorder := &captureRecorder{}
		svc := NewFileService(repo, nil, "", "http://localhost:8080", recorder, mockTxBeginner{}, nil)

		ctx := ctxutil.WithUser(context.Background(), "U456", "T123")
		if err := svc.ShareRemoteFile(ctx, domain.ShareRemoteFileParams{FileID: "F_1", Channels: []string{"C1"}}); err != nil {
			t.Fatalf("share: %v", err)
		}
		if repo.lastShareTeamID != "T123" {
			t.Fatalf("repo saw team_id = %q, want T123", repo.lastShareTeamID)
		}
		if len(recorder.events) != 1 || recorder.events[0].TeamID != "T123" {
			t.Fatalf("share event team_id = %q, want T123", recorder.events[0].TeamID)
		}
	})
}

func TestFileService_ShareRemoteFile_DuplicateShareDoesNotRecordEvent(t *testing.T) {
	repo := newMockFileRepo()
	repo.files["F_1"] = &domain.File{ID: "F_1", TeamID: "T123", UserID: "U456", Name: "doc"}
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
	repo.files["F_1"] = &domain.File{ID: "F_1", TeamID: "T123", UserID: "U_OWNER", Name: "doc"}
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
