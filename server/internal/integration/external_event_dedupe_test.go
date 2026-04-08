//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/johnsuh/teraslack/server/internal/api"
)

func TestExternalEventDedupeKeyMigrationRewritesLegacyRows(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	workspace := mustJSON[api.Workspace](
		t,
		h,
		http.MethodPost,
		"/workspaces",
		alpha.Token,
		api.CreateWorkspaceRequest{Name: "Acme", Slug: h.uniqueSlug("acme")},
		http.StatusCreated,
	)

	event := h.waitForExternalEvent(
		t,
		alpha.Token,
		"/events?type=workspace.created&resource_type=workspace&resource_id="+url.QueryEscape(workspace.ID),
	)

	eventID, err := uuid.Parse(event.ID)
	if err != nil {
		t.Fatalf("parse external event id %q: %v", event.ID, err)
	}

	var storedDedupeKey string
	var sourceInternalEventID uuid.UUID
	var eventType string
	if err := h.pool.QueryRow(
		context.Background(),
		`select dedupe_key, source_internal_event_id, type
		from external_events
		where id = $1`,
		eventID,
	).Scan(&storedDedupeKey, &sourceInternalEventID, &eventType); err != nil {
		t.Fatalf("load external event row: %v", err)
	}

	wantCanonical := "internal:" + sourceInternalEventID.String() + ":" + eventType
	if storedDedupeKey != wantCanonical {
		t.Fatalf("stored dedupe_key = %q, want %q", storedDedupeKey, wantCanonical)
	}

	legacyDedupeKey := fmt.Sprintf("internal:%d:%s", sourceInternalEventID, eventType)
	if _, err := h.pool.Exec(
		context.Background(),
		`update external_events
		set dedupe_key = $1
		where id = $2`,
		legacyDedupeKey,
		eventID,
	); err != nil {
		t.Fatalf("set legacy dedupe_key: %v", err)
	}

	migrationPath := filepath.Join(testStack.rootDir, "server", "internal", "db", "migrations", "000009_external_event_dedupe_keys.sql")
	statement, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := h.pool.Exec(context.Background(), string(statement)); err != nil {
			t.Fatalf("run migration: %v", err)
		}
	}

	var migratedDedupeKey string
	if err := h.pool.QueryRow(
		context.Background(),
		`select dedupe_key
		from external_events
		where id = $1`,
		eventID,
	).Scan(&migratedDedupeKey); err != nil {
		t.Fatalf("reload migrated dedupe_key: %v", err)
	}
	if migratedDedupeKey != wantCanonical {
		t.Fatalf("migrated dedupe_key = %q, want %q", migratedDedupeKey, wantCanonical)
	}
}
