package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityMigrationsDropMembershipBridgeAndEnforceUserUniqueness(t *testing.T) {
	t.Helper()

	root := filepath.Join("migrations")

	backfill := readMigration(t, filepath.Join(root, "000010_users_account_id_backfill.up.sql"))
	if !strings.Contains(backfill, "ADD COLUMN IF NOT EXISTS account_id") {
		t.Fatal("users account_id backfill migration must add account_id")
	}
	if !strings.Contains(backfill, "SET account_id = wm.account_id") {
		t.Fatal("users account_id backfill migration must copy account ownership from the legacy join table")
	}
	if !strings.Contains(backfill, "idx_users_account_workspace_unique") {
		t.Fatal("users account_id backfill migration must add the account/workspace uniqueness index")
	}

	authDrop := readMigration(t, filepath.Join(root, "000011_auth_drop_membership_id.up.sql"))
	if !strings.Contains(authDrop, "DROP COLUMN IF EXISTS membership_id") {
		t.Fatal("auth migrations must drop membership_id bridge columns")
	}

	dropMemberships := readMigration(t, filepath.Join(root, "000013_drop_workspace_memberships.up.sql"))
	if !strings.Contains(dropMemberships, "UNIQUE (account_id, workspace_id)") {
		t.Fatal("final membership-drop migration must enforce one user per account per workspace")
	}
	if !strings.Contains(dropMemberships, "DROP TABLE IF EXISTS public.workspace_memberships") {
		t.Fatal("final membership-drop migration must remove the legacy workspace_memberships table")
	}

	dropAccountPersona := readMigration(t, filepath.Join(root, "000015_accounts_drop_persona_fields.up.sql"))
	for _, snippet := range []string{
		"DROP COLUMN IF EXISTS name",
		"DROP COLUMN IF EXISTS real_name",
		"DROP COLUMN IF EXISTS display_name",
		"DROP COLUMN IF EXISTS profile",
	} {
		if !strings.Contains(dropAccountPersona, snippet) {
			t.Fatalf("accounts persona-drop migration missing %q", snippet)
		}
	}
}

func readMigration(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(body)
}
