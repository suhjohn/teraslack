package openapicli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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
	assertCommandExists(t, cli, "workspaces", "billing")
	assertCommandExists(t, cli, "auth", "oauth-start")
	assertCommandExists(t, cli, "api-keys", "rotate")
	assertCommandExists(t, cli, "messages", "react")
	assertCommandExists(t, cli, "auth", "me")
	assertCommandExists(t, cli, "health", "get")
	assertCommandExists(t, cli, "search", "run")
	assertCommandExists(t, cli, "files", "start-upload")
	assertCommandExists(t, cli, "conversations", "members")
	assertCommandExists(t, cli, "conversations", "mark-read")
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

func TestRunVersionCommand(t *testing.T) {
	oldVersion := Version
	Version = "v9.9.9-test"
	t.Cleanup(func() {
		Version = oldVersion
	})

	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"version"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run(version) exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "teraslack v9.9.9-test" {
		t.Fatalf("stdout = %q, want %q", got, "teraslack v9.9.9-test")
	}
}

func TestRootHelpIncludesLifecycleCommands(t *testing.T) {
	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	cli.printRootHelp(&output)
	text := output.String()
	for _, token := range []string{"signin", "signout", "whoami", "health", "search", "version", "update", "uninstall"} {
		if !strings.Contains(text, token) {
			t.Fatalf("root help missing %q: %s", token, text)
		}
	}
}

func TestHelpSupportsTopLevelAliases(t *testing.T) {
	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"help", "whoami"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("Run(help whoami) exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "auth me") {
		t.Fatalf("help whoami output = %q, want auth me help", text)
	}
}

func TestSigninHelpIncludesProviders(t *testing.T) {
	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	cli.printSigninHelp(nil, &output)
	text := output.String()
	for _, token := range []string{"email", "google", "github"} {
		if !strings.Contains(text, token) {
			t.Fatalf("signin help missing %q: %s", token, text)
		}
	}
}

func TestStripInstallerPathBlock(t *testing.T) {
	content := "\n# Added by Teraslack installer\nexport PATH=\"/Users/test/.teraslack/bin:$PATH\"\n"
	got := stripInstallerPathBlock(content, "/Users/test/.teraslack/bin")
	if strings.Contains(got, ".teraslack/bin") {
		t.Fatalf("stripInstallerPathBlock() = %q, want path entry removed", got)
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
