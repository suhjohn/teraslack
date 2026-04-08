package openapicli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallCodexIntegrationIsIdempotent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".codex", "config.toml")
	hooksPath := filepath.Join(homeDir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("model = \"gpt-5.4\"\n[features]\nmulti_agent = true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo keep"}]}]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(hooks) error = %v", err)
	}

	const cliBinaryPath = "/tmp/tera slack/bin/teraslack"
	const mcpBinaryPath = "/tmp/tera slack/mcp/bin/teraslack-mcp"
	if err := installCodexIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		t.Fatalf("installCodexIntegration() error = %v", err)
	}
	if err := installCodexIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		t.Fatalf("installCodexIntegration() second call error = %v", err)
	}

	configDoc, err := loadTOMLDocument(configPath)
	if err != nil {
		t.Fatalf("loadTOMLDocument() error = %v", err)
	}
	if model, _ := configDoc["model"].(string); model != "gpt-5.4" {
		t.Fatalf("model = %#v", configDoc["model"])
	}
	features := ensureMap(configDoc, "features")
	if features["codex_hooks"] != true {
		t.Fatalf("features.codex_hooks = %#v", features["codex_hooks"])
	}
	mcpServers := ensureMap(configDoc, "mcp_servers")
	teraslackLocal := ensureMap(mcpServers, "teraslack_local")
	if teraslackLocal["command"] != mcpBinaryPath {
		t.Fatalf("mcp_servers.teraslack_local.command = %#v", teraslackLocal["command"])
	}
	if _, ok := teraslackLocal["args"]; ok {
		t.Fatalf("expected mcp_servers.teraslack_local.args to be removed, got %#v", teraslackLocal["args"])
	}

	hooksDoc, err := loadJSONDocument(hooksPath)
	if err != nil {
		t.Fatalf("loadJSONDocument() error = %v", err)
	}
	sessionHooks, _ := ensureMap(hooksDoc, "hooks")["SessionStart"].([]any)
	managedCount := 0
	for _, entry := range sessionHooks {
		if sessionStartHookOwnsEntry(entry, managedCodexHookCommand) {
			managedCount++
		}
	}
	if managedCount != 1 {
		t.Fatalf("expected exactly one managed Codex SessionStart hook, got %d", managedCount)
	}
}

func TestInstallCodexIntegrationUsesCODEXHOMEWhenSet(t *testing.T) {
	homeDir := t.TempDir()
	codexHome := filepath.Join(homeDir, "custom-codex-home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)

	const cliBinaryPath = "/tmp/tera slack/bin/teraslack"
	const mcpBinaryPath = "/tmp/tera slack/mcp/bin/teraslack-mcp"
	if err := installCodexIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		t.Fatalf("installCodexIntegration() error = %v", err)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	hooksPath := filepath.Join(codexHome, "hooks.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config at CODEX_HOME: %v", err)
	}
	if _, err := os.Stat(hooksPath); err != nil {
		t.Fatalf("expected hooks at CODEX_HOME: %v", err)
	}
}

func TestUninstallCodexIntegrationRemovesOnlyManagedEntries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".codex", "config.toml")
	hooksPath := filepath.Join(homeDir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("model = \"gpt-5.4\"\n[features]\ncodex_hooks = true\nmulti_agent = true\n[mcp_servers.teraslack_local]\ncommand = \"/tmp/teraslack-mcp\"\n[mcp_servers.other]\ncommand = \"echo\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"/tmp/teraslack hook session-start --client codex","statusMessage":"Setting up Teraslack session agent"}]},{"matcher":"startup","hooks":[{"type":"command","command":"echo keep"}]}]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(hooks) error = %v", err)
	}

	if err := uninstallCodexIntegration(); err != nil {
		t.Fatalf("uninstallCodexIntegration() error = %v", err)
	}

	configDoc, err := loadTOMLDocument(configPath)
	if err != nil {
		t.Fatalf("loadTOMLDocument() error = %v", err)
	}
	if model, _ := configDoc["model"].(string); model != "gpt-5.4" {
		t.Fatalf("model = %#v", configDoc["model"])
	}
	features := ensureMap(configDoc, "features")
	if features["codex_hooks"] != true {
		t.Fatalf("features.codex_hooks = %#v", features["codex_hooks"])
	}
	if features["multi_agent"] != true {
		t.Fatalf("features.multi_agent = %#v", features["multi_agent"])
	}
	mcpServers := ensureMap(configDoc, "mcp_servers")
	if _, ok := mcpServers["teraslack_local"]; ok {
		t.Fatalf("expected teraslack_local MCP server to be removed")
	}
	otherServer := ensureMap(mcpServers, "other")
	if otherServer["command"] != "echo" {
		t.Fatalf("mcp_servers.other.command = %#v", otherServer["command"])
	}

	hooksDoc, err := loadJSONDocument(hooksPath)
	if err != nil {
		t.Fatalf("loadJSONDocument() error = %v", err)
	}
	sessionHooks, _ := ensureMap(hooksDoc, "hooks")["SessionStart"].([]any)
	if len(sessionHooks) != 1 {
		t.Fatalf("expected one remaining SessionStart hook, got %#v", sessionHooks)
	}
	if sessionStartHookOwnsEntry(sessionHooks[0], managedCodexHookCommand) {
		t.Fatalf("managed Codex hook was not removed")
	}
}

func TestUninstallCodexIntegrationRemovesCodexHooksFlagWhenNoHooksRemain(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".codex", "config.toml")
	hooksPath := filepath.Join(homeDir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("[features]\ncodex_hooks = true\n[mcp_servers.teraslack_local]\ncommand = \"/tmp/teraslack-mcp\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"/tmp/teraslack hook session-start --client codex"}]}]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(hooks) error = %v", err)
	}

	if err := uninstallCodexIntegration(); err != nil {
		t.Fatalf("uninstallCodexIntegration() error = %v", err)
	}

	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Fatalf("expected hooks.json to be removed, err=%v", err)
	}

	configDoc, err := loadTOMLDocument(configPath)
	if err != nil {
		t.Fatalf("loadTOMLDocument() error = %v", err)
	}
	if _, ok := configDoc["features"]; ok {
		t.Fatalf("expected empty features map to be pruned: %#v", configDoc)
	}
	if _, ok := configDoc["mcp_servers"]; ok {
		t.Fatalf("expected empty mcp_servers map to be pruned: %#v", configDoc)
	}
}

func TestInstallClaudeIntegrationIsIdempotent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	rootConfigPath := filepath.Join(homeDir, ".claude.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"permissions":{"allow":["mcp__other__call"]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}
	if err := os.WriteFile(rootConfigPath, []byte(`{"installMethod":"native","mcpServers":{"other":{"command":"echo","args":["other"]}}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(root config) error = %v", err)
	}

	const cliBinaryPath = "/tmp/tera slack/bin/teraslack"
	const mcpBinaryPath = "/tmp/tera slack/mcp/bin/teraslack-mcp"
	if err := installClaudeIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		t.Fatalf("installClaudeIntegration() error = %v", err)
	}
	if err := installClaudeIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		t.Fatalf("installClaudeIntegration() second call error = %v", err)
	}

	settingsDoc, err := loadJSONDocument(settingsPath)
	if err != nil {
		t.Fatalf("loadJSONDocument(settings) error = %v", err)
	}
	permissions := ensureMap(settingsDoc, "permissions")
	allow, _ := permissions["allow"].([]any)
	if len(allow) != 1 || allow[0] != "mcp__other__call" {
		t.Fatalf("permissions.allow = %#v", permissions["allow"])
	}

	rawSessionHooks, _ := ensureMap(settingsDoc, "hooks")["SessionStart"].([]any)
	managedCount := 0
	for _, entry := range rawSessionHooks {
		if sessionStartHookOwnsEntry(entry, managedClaudeHookCommand) {
			managedCount++
		}
	}
	if managedCount != 1 {
		t.Fatalf("expected exactly one managed Claude SessionStart hook, got %d", managedCount)
	}

	rootDoc, err := loadJSONDocument(rootConfigPath)
	if err != nil {
		t.Fatalf("loadJSONDocument(root) error = %v", err)
	}
	mcpServers := ensureMap(rootDoc, "mcpServers")
	if _, ok := mcpServers["other"]; !ok {
		t.Fatalf("expected existing MCP server to be preserved: %#v", mcpServers)
	}
	teraslackLocal := ensureMap(mcpServers, "teraslack_local")
	if teraslackLocal["command"] != mcpBinaryPath {
		t.Fatalf("mcpServers.teraslack_local.command = %#v", teraslackLocal["command"])
	}
	if _, ok := teraslackLocal["args"]; ok {
		t.Fatalf("expected mcpServers.teraslack_local.args to be omitted, got %#v", teraslackLocal["args"])
	}
}

func TestUninstallClaudeIntegrationRemovesOnlyManagedEntries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	rootConfigPath := filepath.Join(homeDir, ".claude.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"permissions":{"allow":["mcp__other__call"]},"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"/tmp/teraslack hook session-start --client claude"}]},{"matcher":"startup","hooks":[{"type":"command","command":"echo keep"}]}]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}
	if err := os.WriteFile(rootConfigPath, []byte(`{"installMethod":"native","mcpServers":{"teraslack_local":{"command":"/tmp/teraslack-mcp"},"other":{"command":"echo","args":["other"]}}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(root config) error = %v", err)
	}

	if err := uninstallClaudeIntegration(); err != nil {
		t.Fatalf("uninstallClaudeIntegration() error = %v", err)
	}

	settingsDoc, err := loadJSONDocument(settingsPath)
	if err != nil {
		t.Fatalf("loadJSONDocument(settings) error = %v", err)
	}
	permissions := ensureMap(settingsDoc, "permissions")
	allow, _ := permissions["allow"].([]any)
	if len(allow) != 1 || allow[0] != "mcp__other__call" {
		t.Fatalf("permissions.allow = %#v", permissions["allow"])
	}
	rawSessionHooks, _ := ensureMap(settingsDoc, "hooks")["SessionStart"].([]any)
	if len(rawSessionHooks) != 1 {
		t.Fatalf("expected one remaining Claude SessionStart hook, got %#v", rawSessionHooks)
	}
	if sessionStartHookOwnsEntry(rawSessionHooks[0], managedClaudeHookCommand) {
		t.Fatalf("managed Claude hook was not removed")
	}

	rootDoc, err := loadJSONDocument(rootConfigPath)
	if err != nil {
		t.Fatalf("loadJSONDocument(root) error = %v", err)
	}
	mcpServers := ensureMap(rootDoc, "mcpServers")
	if _, ok := mcpServers["teraslack_local"]; ok {
		t.Fatalf("expected teraslack_local MCP server to be removed")
	}
	if _, ok := mcpServers["other"]; !ok {
		t.Fatalf("expected existing MCP server to be preserved: %#v", mcpServers)
	}
}

func TestUninstallClaudeIntegrationRemovesEmptyFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	rootConfigPath := filepath.Join(homeDir, ".claude.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"/tmp/teraslack hook session-start --client claude"}]}]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}
	if err := os.WriteFile(rootConfigPath, []byte(`{"mcpServers":{"teraslack_local":{"command":"/tmp/teraslack-mcp"}}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(root config) error = %v", err)
	}

	if err := uninstallClaudeIntegration(); err != nil {
		t.Fatalf("uninstallClaudeIntegration() error = %v", err)
	}

	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("expected settings.json to be removed, err=%v", err)
	}
	if _, err := os.Stat(rootConfigPath); !os.IsNotExist(err) {
		t.Fatalf("expected .claude.json to be removed, err=%v", err)
	}
}
