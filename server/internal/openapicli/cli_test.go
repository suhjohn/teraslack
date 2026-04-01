package openapicli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewBuildsCommandSurface(t *testing.T) {
	t.Parallel()

	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if cli.operationCnt != 87 {
		t.Fatalf("operationCnt = %d, want 87", cli.operationCnt)
	}

	assertCommandExists(t, cli, "workspaces", "list")
	assertCommandExists(t, cli, "workspaces", "get-billing")
	assertCommandExists(t, cli, "auth", "start-oauth")
	assertCommandExists(t, cli, "api-keys", "rotate")
	assertCommandExists(t, cli, "messages", "create-reaction")
}

func TestBuildRequestBuildsPathQueryAndBody(t *testing.T) {
	t.Parallel()

	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	conversations := mustOperation(t, cli, "conversations", "list")
	values := map[string]*string{
		"workspace-id":     stringRef("W123"),
		"types":            stringRef("public_channel,private_channel"),
		"exclude-archived": stringRef("true"),
	}

	path, query, body, err := buildRequest(conversations, values, "", "", nil)
	if err != nil {
		t.Fatalf("buildRequest(conversations) error = %v", err)
	}
	if path != "/conversations" {
		t.Fatalf("path = %q, want /conversations", path)
	}
	if body != nil {
		t.Fatalf("body = %#v, want nil", body)
	}
	if got, want := query["workspace_id"], "W123"; got != want {
		t.Fatalf("workspace_id = %#v, want %q", got, want)
	}
	if got, want := query["types"], "public_channel,private_channel"; got != want {
		t.Fatalf("types = %#v, want %q", got, want)
	}
	if got, want := query["exclude_archived"], true; got != want {
		t.Fatalf("exclude_archived = %#v, want %v", got, want)
	}

	workspaces := mustOperation(t, cli, "workspaces", "create")
	bodyPath, _, bodyValue, err := buildRequest(workspaces, map[string]*string{}, "", "", []string{
		"name=Acme",
		"billing.plan=pro",
		"default_channels=[\"C1\",\"C2\"]",
	})
	if err != nil {
		t.Fatalf("buildRequest(workspaces) error = %v", err)
	}
	if bodyPath != "/workspaces" {
		t.Fatalf("bodyPath = %q, want /workspaces", bodyPath)
	}

	objectBody, ok := bodyValue.(map[string]any)
	if !ok {
		t.Fatalf("bodyValue type = %T, want map[string]any", bodyValue)
	}
	if got, want := objectBody["name"], "Acme"; got != want {
		t.Fatalf("body name = %#v, want %q", got, want)
	}

	billing, ok := objectBody["billing"].(map[string]any)
	if !ok {
		t.Fatalf("billing type = %T, want map[string]any", objectBody["billing"])
	}
	if got, want := billing["plan"], "pro"; got != want {
		t.Fatalf("billing.plan = %#v, want %q", got, want)
	}
}

func TestMergeCollectionPage(t *testing.T) {
	t.Parallel()

	combined := map[string]any{
		"items":       []any{map[string]any{"id": "U1"}},
		"next_cursor": "page-2",
		"has_more":    true,
	}
	page := map[string]any{
		"items": []any{map[string]any{"id": "U2"}},
	}

	mergeCollectionPage(combined, page)

	items, ok := combined["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T, want []any", combined["items"])
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if _, exists := combined["next_cursor"]; exists {
		t.Fatalf("next_cursor should be removed when the new page has no cursor")
	}
	if got, want := combined["has_more"], true; got != want {
		t.Fatalf("has_more = %#v, want %v", got, want)
	}
}

func TestLoadFileConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	err := os.WriteFile(path, []byte("{\"base_url\":\"https://api.example.com\",\"api_key\":\"sk_test\"}\n"), 0o600)
	if err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	t.Setenv("TERASLACK_CONFIG_FILE", path)

	cfg, err := loadFileConfig()
	if err != nil {
		t.Fatalf("loadFileConfig() error = %v", err)
	}
	if got, want := cfg.BaseURL, "https://api.example.com"; got != want {
		t.Fatalf("BaseURL = %q, want %q", got, want)
	}
	if got, want := cfg.APIKey, "sk_test"; got != want {
		t.Fatalf("APIKey = %q, want %q", got, want)
	}
}

func assertCommandExists(t *testing.T, cli *CLI, groupName, commandName string) {
	t.Helper()

	group := cli.groupByName[groupName]
	if group == nil {
		t.Fatalf("missing group %q", groupName)
	}
	if group.byName[commandName] == nil {
		t.Fatalf("missing command %q in group %q", commandName, groupName)
	}
}

func mustOperation(t *testing.T, cli *CLI, groupName, commandName string) *Operation {
	t.Helper()

	group := cli.groupByName[groupName]
	if group == nil {
		t.Fatalf("missing group %q", groupName)
	}
	op := group.byName[commandName]
	if op == nil {
		t.Fatalf("missing command %q in group %q", commandName, groupName)
	}
	return op
}

func stringRef(value string) *string {
	return &value
}
