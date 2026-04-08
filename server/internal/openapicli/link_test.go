package openapicli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathsUseConfigDir(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "teraslack-config")
	t.Setenv("TERASLACK_CONFIG_DIR", configDir)
	t.Setenv("TERASLACK_CONFIG_FILE", "")

	gotConfigDir, err := configDirPath()
	if err != nil {
		t.Fatalf("configDirPath() error = %v", err)
	}
	if gotConfigDir != configDir {
		t.Fatalf("configDirPath() = %q, want %q", gotConfigDir, configDir)
	}

	gotConfigPath, err := configFilePath()
	if err != nil {
		t.Fatalf("configFilePath() error = %v", err)
	}
	if gotConfigPath != filepath.Join(configDir, "config.json") {
		t.Fatalf("configFilePath() = %q", gotConfigPath)
	}

	gotLinksPath, err := linksFilePath()
	if err != nil {
		t.Fatalf("linksFilePath() error = %v", err)
	}
	if gotLinksPath != filepath.Join(configDir, "links.json") {
		t.Fatalf("linksFilePath() = %q", gotLinksPath)
	}
}

func TestRunLinkStoresAndReadsCurrentDirectoryLink(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "teraslack-config")
	t.Setenv("TERASLACK_CONFIG_DIR", configDir)
	t.Setenv("TERASLACK_CONFIG_FILE", "")

	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	canonicalProjectDir, err := canonicalLinkPath(projectDir)
	if err != nil {
		t.Fatalf("canonicalLinkPath(projectDir) error = %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	cli := &CLI{}
	conversationID := "11111111-1111-1111-1111-111111111111"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := cli.runLink([]string{"--conversation", conversationID}, "json", &stdout, &stderr); code != 0 {
		t.Fatalf("runLink(set) code = %d, stderr = %s", code, stderr.String())
	}

	var saved linkCommandResponse
	if err := json.Unmarshal(stdout.Bytes(), &saved); err != nil {
		t.Fatalf("Unmarshal(set output) error = %v", err)
	}
	if saved.LinkedPath != canonicalProjectDir {
		t.Fatalf("saved.LinkedPath = %q, want %q", saved.LinkedPath, canonicalProjectDir)
	}
	if saved.ConversationID != conversationID {
		t.Fatalf("saved.ConversationID = %q, want %q", saved.ConversationID, conversationID)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.runLink(nil, "json", &stdout, &stderr); code != 0 {
		t.Fatalf("runLink(show) code = %d, stderr = %s", code, stderr.String())
	}

	var resolved linkCommandResponse
	if err := json.Unmarshal(stdout.Bytes(), &resolved); err != nil {
		t.Fatalf("Unmarshal(show output) error = %v", err)
	}
	if resolved.LinkedPath != canonicalProjectDir {
		t.Fatalf("resolved.LinkedPath = %q, want %q", resolved.LinkedPath, canonicalProjectDir)
	}
	if resolved.ConversationID != conversationID {
		t.Fatalf("resolved.ConversationID = %q, want %q", resolved.ConversationID, conversationID)
	}
}

func TestRunLinkResolvesParentDirectoryLink(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "teraslack-config")
	t.Setenv("TERASLACK_CONFIG_DIR", configDir)
	t.Setenv("TERASLACK_CONFIG_FILE", "")

	repoDir := filepath.Join(t.TempDir(), "repo")
	subDir := filepath.Join(repoDir, "nested", "dir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	canonicalRepoDir, err := canonicalLinkPath(repoDir)
	if err != nil {
		t.Fatalf("canonicalLinkPath(repoDir) error = %v", err)
	}
	canonicalSubDir, err := canonicalLinkPath(subDir)
	if err != nil {
		t.Fatalf("canonicalLinkPath(subDir) error = %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	cli := &CLI{}
	conversationID := "22222222-2222-2222-2222-222222222222"

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Chdir(repoDir) error = %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := cli.runLink([]string{"--conversation", conversationID}, "json", &stdout, &stderr); code != 0 {
		t.Fatalf("runLink(set) code = %d, stderr = %s", code, stderr.String())
	}

	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("Chdir(subDir) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.runLink(nil, "json", &stdout, &stderr); code != 0 {
		t.Fatalf("runLink(show) code = %d, stderr = %s", code, stderr.String())
	}

	var resolved linkCommandResponse
	if err := json.Unmarshal(stdout.Bytes(), &resolved); err != nil {
		t.Fatalf("Unmarshal(show output) error = %v", err)
	}
	if resolved.CurrentPath != canonicalSubDir {
		t.Fatalf("resolved.CurrentPath = %q, want %q", resolved.CurrentPath, canonicalSubDir)
	}
	if resolved.LinkedPath != canonicalRepoDir {
		t.Fatalf("resolved.LinkedPath = %q, want %q", resolved.LinkedPath, canonicalRepoDir)
	}
	if resolved.ConversationID != conversationID {
		t.Fatalf("resolved.ConversationID = %q, want %q", resolved.ConversationID, conversationID)
	}
}
