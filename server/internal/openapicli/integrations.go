package openapicli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	managedCodexHookCommand  = "hook session-start --client codex"
	managedClaudeHookCommand = "hook session-start --client claude"
)

func (c *CLI) runIntegrations(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" {
		c.printIntegrationsHelp(stdout)
		return 0
	}

	switch strings.TrimSpace(args[0]) {
	case "install":
		return c.runIntegrationsInstall(ctx, args[1:], stdout, stderr)
	case "uninstall":
		return c.runIntegrationsUninstall(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown integrations command %q\n\n", args[0])
		c.printIntegrationsHelp(stderr)
		return 2
	}
}

func (c *CLI) printIntegrationsHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  teraslack integrations install [--cli-binary-path <path>] [--mcp-binary-path <path>]")
	fmt.Fprintln(w, "  teraslack integrations uninstall")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Install or remove the local Teraslack stdio MCP server and SessionStart hooks for Codex and Claude Code.")
}

func (c *CLI) runIntegrationsInstall(_ context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "automatic Codex and Claude integration setup is only supported on macOS and Linux")
		return 1
	}

	var cliBinaryPath string
	var mcpBinaryPath string
	fs := flag.NewFlagSet("integrations install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cliBinaryPath, "cli-binary-path", "", "Override the teraslack CLI binary path used for SessionStart hooks.")
	fs.StringVar(&mcpBinaryPath, "mcp-binary-path", "", "Override the teraslack-mcp binary path used for MCP server launch.")
	fs.Usage = func() {
		c.printIntegrationsHelp(stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	if strings.TrimSpace(cliBinaryPath) == "" {
		currentPath, err := currentExecutablePath()
		if err != nil {
			fmt.Fprintf(stderr, "resolve teraslack binary path: %v\n", err)
			return 1
		}
		cliBinaryPath = currentPath
	}
	if strings.TrimSpace(mcpBinaryPath) == "" {
		resolvedPath, err := installedMCPBinaryPath()
		if err != nil {
			fmt.Fprintf(stderr, "resolve teraslack-mcp binary path: %v\n", err)
			return 1
		}
		mcpBinaryPath = resolvedPath
	}
	cliBinaryPath = filepath.Clean(cliBinaryPath)
	mcpBinaryPath = filepath.Clean(mcpBinaryPath)

	if err := installCodexIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		fmt.Fprintf(stderr, "configure Codex integration: %v\n", err)
		return 1
	}
	if err := installClaudeIntegration(cliBinaryPath, mcpBinaryPath); err != nil {
		fmt.Fprintf(stderr, "configure Claude integration: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "configured Codex and Claude integrations")
	return 0
}

func (c *CLI) runIntegrationsUninstall(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("integrations uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		c.printIntegrationsHelp(stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	if err := uninstallCodexIntegration(); err != nil {
		fmt.Fprintf(stderr, "remove Codex integration: %v\n", err)
		return 1
	}
	if err := uninstallClaudeIntegration(); err != nil {
		fmt.Fprintf(stderr, "remove Claude integration: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "removed Codex and Claude integrations")
	return 0
}

func installCodexIntegration(cliBinaryPath string, mcpBinaryPath string) error {
	rootDir, err := codexConfigRootPath()
	if err != nil {
		return err
	}
	configPath := filepath.Join(rootDir, "config.toml")
	hooksPath := filepath.Join(rootDir, "hooks.json")

	configDoc, err := loadTOMLDocument(configPath)
	if err != nil {
		return err
	}
	setNestedMapValue(configDoc, []string{"features", "codex_hooks"}, true)
	setNestedMapValue(configDoc, []string{"mcp_servers", "teraslack_local", "command"}, mcpBinaryPath)
	removeNestedMapValue(configDoc, []string{"mcp_servers", "teraslack_local", "args"})
	if err := saveTOMLDocument(configPath, configDoc); err != nil {
		return err
	}

	hooksDoc, err := loadJSONDocument(hooksPath)
	if err != nil {
		return err
	}
	upsertSessionStartHook(hooksDoc, managedCodexHook(cliBinaryPath), managedCodexHookCommand)
	return saveJSONDocument(hooksPath, hooksDoc)
}

func uninstallCodexIntegration() error {
	rootDir, err := codexConfigRootPath()
	if err != nil {
		return err
	}
	configPath := filepath.Join(rootDir, "config.toml")
	hooksPath := filepath.Join(rootDir, "hooks.json")

	hooksRemain := false
	if fileExists(hooksPath) {
		hooksDoc, err := loadJSONDocument(hooksPath)
		if err != nil {
			return err
		}
		if removeManagedSessionStartHook(hooksDoc, managedCodexHookCommand) {
			if err := saveOrRemoveJSONDocument(hooksPath, hooksDoc); err != nil {
				return err
			}
		}
		hooksRemain = hasConfiguredHooks(hooksDoc)
	}

	if fileExists(configPath) {
		configDoc, err := loadTOMLDocument(configPath)
		if err != nil {
			return err
		}
		changed := removeNestedMapValueAndPrune(configDoc, []string{"mcp_servers", "teraslack_local"})
		if !hooksRemain {
			changed = removeNestedMapValueAndPrune(configDoc, []string{"features", "codex_hooks"}) || changed
		}
		if changed {
			if err := saveOrRemoveTOMLDocument(configPath, configDoc); err != nil {
				return err
			}
		}
	}

	return nil
}

func installClaudeIntegration(cliBinaryPath string, mcpBinaryPath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	rootConfigPath := filepath.Join(homeDir, ".claude.json")

	settingsDoc, err := loadJSONDocument(settingsPath)
	if err != nil {
		return err
	}
	upsertSessionStartHook(settingsDoc, managedClaudeHook(cliBinaryPath), managedClaudeHookCommand)
	if err := saveJSONDocument(settingsPath, settingsDoc); err != nil {
		return err
	}

	rootDoc, err := loadJSONDocument(rootConfigPath)
	if err != nil {
		return err
	}
	mcpServers := ensureMap(rootDoc, "mcpServers")
	mcpServers["teraslack_local"] = map[string]any{
		"command": mcpBinaryPath,
	}
	return saveJSONDocument(rootConfigPath, rootDoc)
}

func uninstallClaudeIntegration() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	rootConfigPath := filepath.Join(homeDir, ".claude.json")

	if fileExists(settingsPath) {
		settingsDoc, err := loadJSONDocument(settingsPath)
		if err != nil {
			return err
		}
		if removeManagedSessionStartHook(settingsDoc, managedClaudeHookCommand) {
			if err := saveOrRemoveJSONDocument(settingsPath, settingsDoc); err != nil {
				return err
			}
		}
	}

	if fileExists(rootConfigPath) {
		rootDoc, err := loadJSONDocument(rootConfigPath)
		if err != nil {
			return err
		}
		if removeNestedMapValueAndPrune(rootDoc, []string{"mcpServers", "teraslack_local"}) {
			if err := saveOrRemoveJSONDocument(rootConfigPath, rootDoc); err != nil {
				return err
			}
		}
	}

	return nil
}

func codexConfigRootPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".codex"), nil
}

func managedCodexHook(binaryPath string) map[string]any {
	return map[string]any{
		"matcher": "startup|resume",
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       shellCommand(binaryPath, "hook", "session-start", "--client", "codex"),
				"statusMessage": "Setting up Teraslack session agent",
			},
		},
	}
}

func managedClaudeHook(binaryPath string) map[string]any {
	return map[string]any{
		"matcher": "startup|resume",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": shellCommand(binaryPath, "hook", "session-start", "--client", "claude"),
			},
		},
	}
}

func upsertSessionStartHook(doc map[string]any, hookGroup map[string]any, managedCommandFragment string) {
	hooks := ensureMap(doc, "hooks")
	existing, _ := hooks["SessionStart"].([]any)
	filtered := make([]any, 0, len(existing)+1)
	for _, entry := range existing {
		if sessionStartHookOwnsEntry(entry, managedCommandFragment) {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, hookGroup)
	hooks["SessionStart"] = filtered
}

func removeManagedSessionStartHook(doc map[string]any, managedCommandFragment string) bool {
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return false
	}
	existing, _ := hooks["SessionStart"].([]any)
	filtered := make([]any, 0, len(existing))
	changed := false
	for _, entry := range existing {
		if sessionStartHookOwnsEntry(entry, managedCommandFragment) {
			changed = true
			continue
		}
		filtered = append(filtered, entry)
	}
	if !changed {
		return false
	}
	if len(filtered) == 0 {
		delete(hooks, "SessionStart")
	} else {
		hooks["SessionStart"] = filtered
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
	return true
}

func hasConfiguredHooks(doc map[string]any) bool {
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return false
	}
	for _, value := range hooks {
		switch typed := value.(type) {
		case []any:
			if len(typed) > 0 {
				return true
			}
		case nil:
		default:
			return true
		}
	}
	return false
}

func sessionStartHookOwnsEntry(entry any, managedCommandFragment string) bool {
	group, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooksValue, ok := group["hooks"].([]any)
	if !ok {
		return false
	}
	for _, rawHook := range hooksValue {
		hook, ok := rawHook.(map[string]any)
		if !ok {
			continue
		}
		command, _ := hook["command"].(string)
		if strings.Contains(command, managedCommandFragment) {
			return true
		}
	}
	return false
}

func loadJSONDocument(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return map[string]any{}, nil
	}
	return doc, nil
}

func saveJSONDocument(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func saveOrRemoveJSONDocument(path string, doc map[string]any) error {
	if len(doc) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return saveJSONDocument(path, doc)
}

func loadTOMLDocument(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return map[string]any{}, nil
	}
	return doc, nil
}

func saveTOMLDocument(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func ensureMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	if existing, ok := root[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	root[key] = created
	return created
}

func setNestedMapValue(root map[string]any, path []string, value any) {
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

func removeNestedMapValue(root map[string]any, path []string) {
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			return
		}
		current = next
	}
	delete(current, path[len(path)-1])
}

func removeNestedMapValueAndPrune(root map[string]any, path []string) bool {
	if root == nil || len(path) == 0 {
		return false
	}
	if len(path) == 1 {
		if _, ok := root[path[0]]; !ok {
			return false
		}
		delete(root, path[0])
		return true
	}

	child, ok := root[path[0]].(map[string]any)
	if !ok {
		return false
	}
	if !removeNestedMapValueAndPrune(child, path[1:]) {
		return false
	}
	if len(child) == 0 {
		delete(root, path[0])
	}
	return true
}

func saveOrRemoveTOMLDocument(path string, doc map[string]any) error {
	if len(doc) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return saveTOMLDocument(path, doc)
}

func shellCommand(binaryPath string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(binaryPath))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`()[]{}*?;&|<>") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
