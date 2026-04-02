package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type execCaptureDB struct {
	sql  string
	args []any
}

func (db *execCaptureDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, nil
}

func (db *execCaptureDB) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	db.sql = sql
	db.args = append([]any(nil), arguments...)
	return pgconn.CommandTag{}, nil
}

func (db *execCaptureDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (db *execCaptureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return captureRow{values: nil}
}

func TestExternalEventRepoInsertFeedRow_RoutesByResourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType string
		wantTable    string
	}{
		{name: "workspace", resourceType: domain.ResourceTypeWorkspace, wantTable: "workspace_event_feed"},
		{name: "conversation", resourceType: domain.ResourceTypeConversation, wantTable: "conversation_event_feed"},
		{name: "file", resourceType: domain.ResourceTypeFile, wantTable: "file_event_feed"},
		{name: "user", resourceType: domain.ResourceTypeUser, wantTable: "user_event_feed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &execCaptureDB{}
			repo := NewExternalEventRepo(db)

			err := repo.insertFeedRow(context.Background(), domain.ExternalEvent{
				ID:           42,
				ResourceType: tt.resourceType,
				ResourceID:   "R123",
			})
			if err != nil {
				t.Fatalf("insertFeedRow: %v", err)
			}
			if !strings.Contains(db.sql, tt.wantTable) {
				t.Fatalf("sql = %q, want table %q", db.sql, tt.wantTable)
			}
			if len(db.args) != 2 || db.args[0] != "R123" || db.args[1] != int64(42) {
				t.Fatalf("args = %#v, want [R123 42]", db.args)
			}
		})
	}
}

func TestExternalEventRepoInsertFeedRow_RejectsUnknownResourceType(t *testing.T) {
	t.Parallel()

	repo := NewExternalEventRepo(&execCaptureDB{})
	err := repo.insertFeedRow(context.Background(), domain.ExternalEvent{
		ID:           1,
		ResourceType: "unknown",
		ResourceID:   "R1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPrincipalCanReadExternalResourceType(t *testing.T) {
	t.Parallel()

	restricted := repository.ExternalEventPrincipal{
		WorkspaceID: "T123",
		UserID:      "U123",
		APIKeyID:    "AK123",
		Permissions: []string{domain.PermissionConversationsCreate},
	}
	if principalCanReadExternalResourceType(restricted, domain.ResourceTypeConversation) {
		t.Fatal("conversation events should require messages.read for restricted API keys")
	}
	if !principalCanReadExternalResourceType(restricted, domain.ResourceTypeWorkspace) {
		t.Fatal("workspace events should remain visible within the workspace")
	}

	reader := repository.ExternalEventPrincipal{
		WorkspaceID: "T123",
		UserID:      "U123",
		APIKeyID:    "AK123",
		Permissions: []string{domain.PermissionMessagesRead},
	}
	if !principalCanReadExternalResourceType(reader, domain.ResourceTypeConversation) {
		t.Fatal("messages.read should allow conversation events")
	}

	sessionPrincipal := repository.ExternalEventPrincipal{WorkspaceID: "T123", UserID: "U123"}
	if !principalCanReadExternalResourceType(sessionPrincipal, domain.ResourceTypeConversation) {
		t.Fatal("human sessions should remain unrestricted")
	}

	empty := repository.ExternalEventPrincipal{
		WorkspaceID: "T123",
		UserID:      "U123",
		APIKeyID:    "AK123",
		Permissions: []string{},
	}
	if principalCanReadExternalResourceType(empty, domain.ResourceTypeConversation) {
		t.Fatal("empty API key permissions should not allow conversation events")
	}
}

func TestAllowedResourceTypes_ExcludesDisallowedResourceFamilies(t *testing.T) {
	t.Parallel()

	principal := repository.ExternalEventPrincipal{
		WorkspaceID: "T123",
		UserID:      "U123",
		APIKeyID:    "AK123",
		Permissions: []string{domain.PermissionConversationsCreate},
	}

	resourceTypes := allowedResourceTypes(principal)
	if slicesContain(resourceTypes, domain.ResourceTypeConversation) {
		t.Fatalf("resource types should not include conversation: %#v", resourceTypes)
	}
	if !slicesContain(resourceTypes, domain.ResourceTypeWorkspace) {
		t.Fatalf("resource types should still include workspace: %#v", resourceTypes)
	}
}

func TestExternalMemberResourceTypes_OnlyIncludesSharedResourceFamilies(t *testing.T) {
	t.Parallel()

	principal := repository.ExternalEventPrincipal{
		WorkspaceID: "T123",
		AccountID:   "A123",
		APIKeyID:    "AK123",
		Permissions: []string{domain.PermissionMessagesRead},
	}

	resourceTypes := externalMemberResourceTypes(principal)
	if !slicesContain(resourceTypes, domain.ResourceTypeConversation) {
		t.Fatalf("resource types should include conversation: %#v", resourceTypes)
	}
	if slicesContain(resourceTypes, domain.ResourceTypeWorkspace) {
		t.Fatalf("resource types should not include workspace: %#v", resourceTypes)
	}
	if slicesContain(resourceTypes, domain.ResourceTypeUser) {
		t.Fatalf("resource types should not include user: %#v", resourceTypes)
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
