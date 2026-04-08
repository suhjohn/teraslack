package openapicli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnsuh/teraslack/server/internal/api"
)

func TestInitializeAgentSessionFromHookCreatesRecordAndPIDBridge(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")

	repoDir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if _, err := upsertDirectoryLink(repoDir, "conversation-1", testNow()); err != nil {
		t.Fatalf("upsertDirectoryLink() error = %v", err)
	}

	var createdAgentRequest map[string]any
	var addedParticipantsRequest map[string]any
	var updatedAgentRequest api.UpdateAgentRequest
	var currentAgentMetadata map[string]any
	patchAgentCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer human-session-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/conversations/conversation-1":
			writeJSONResponse(t, w, map[string]any{
				"id":                 "conversation-1",
				"workspace_id":       "workspace-1",
				"access_policy":      "members",
				"participant_count":  1,
				"title":              "Launchpad",
				"created_by_user_id": "human-1",
				"archived":           false,
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/workspace-1":
			writeJSONResponse(t, w, map[string]any{
				"id":                 "workspace-1",
				"slug":               "launchpad",
				"name":               "Launchpad",
				"created_by_user_id": "human-1",
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/agents":
			if err := json.NewDecoder(r.Body).Decode(&createdAgentRequest); err != nil {
				t.Fatalf("decode create agent body: %v", err)
			}
			writeJSONResponse(t, w, map[string]any{
				"agent": map[string]any{
					"user": map[string]any{
						"id":             "agent-user-1",
						"principal_type": "agent",
						"status":         "active",
						"profile": map[string]any{
							"handle":       "agent-user-1",
							"display_name": "Agent User",
						},
					},
					"owner_type":         "workspace",
					"owner_workspace_id": "workspace-1",
					"mode":               "safe_write",
					"created_by_user_id": "human-1",
					"created_at":         "2026-04-07T00:00:00Z",
					"updated_at":         "2026-04-07T00:00:00Z",
				},
				"api_key": map[string]any{
					"id":         "agent-key-1",
					"token":      "agent-token-1",
					"scope_type": "global",
					"created_at": "2026-04-07T00:00:00Z",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/agents/agent-user-1":
			payload := map[string]any{
				"user": map[string]any{
					"id":             "agent-user-1",
					"principal_type": "agent",
					"status":         "active",
					"profile": map[string]any{
						"handle":       "agent-user-1",
						"display_name": "Agent User",
					},
				},
				"owner_type":         "workspace",
				"owner_workspace_id": "workspace-1",
				"mode":               "safe_write",
				"created_by_user_id": "human-1",
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			}
			if currentAgentMetadata != nil {
				payload["metadata"] = currentAgentMetadata
			}
			writeJSONResponse(t, w, payload)
		case r.Method == http.MethodPatch && r.URL.Path == "/agents/agent-user-1":
			patchAgentCalls++
			if err := json.NewDecoder(r.Body).Decode(&updatedAgentRequest); err != nil {
				t.Fatalf("decode patch agent body: %v", err)
			}
			if updatedAgentRequest.Metadata == nil {
				t.Fatal("expected patch request metadata")
			}
			currentAgentMetadata = *updatedAgentRequest.Metadata
			writeJSONResponse(t, w, map[string]any{
				"user": map[string]any{
					"id":             "agent-user-1",
					"principal_type": "agent",
					"status":         "active",
					"profile": map[string]any{
						"handle":       "agent-user-1",
						"display_name": "Agent User",
					},
				},
				"owner_type":         "workspace",
				"owner_workspace_id": "workspace-1",
				"mode":               "safe_write",
				"metadata":           currentAgentMetadata,
				"created_by_user_id": "human-1",
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/conversations/conversation-1/participants":
			if err := json.NewDecoder(r.Body).Decode(&addedParticipantsRequest); err != nil {
				t.Fatalf("decode add participants body: %v", err)
			}
			writeJSONResponse(t, w, map[string]any{"items": []any{}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := saveFileConfig(fileConfig{
		BaseURL:      server.URL,
		SessionToken: "human-session-token",
		UserID:       "human-1",
	}); err != nil {
		t.Fatalf("saveFileConfig() error = %v", err)
	}
	canonicalRepoDir, err := canonicalLinkPath(repoDir)
	if err != nil {
		t.Fatalf("canonicalLinkPath() error = %v", err)
	}

	output, err := initializeAgentSessionFromHook(context.Background(), "codex", sessionStartHookInput{
		SessionID: "session-1",
		Cwd:       repoDir,
		Source:    "startup",
	})
	if err != nil {
		t.Fatalf("initializeAgentSessionFromHook() error = %v", err)
	}
	if strings.TrimSpace(output.AdditionalContext) == "" {
		t.Fatalf("expected additional context, got %+v", output)
	}
	if !strings.Contains(output.AdditionalContext, "Conversation ID: `conversation-1`.") {
		t.Fatalf("expected conversation id in additional context, got %q", output.AdditionalContext)
	}
	if !strings.Contains(output.AdditionalContext, "Conversation title: \"Launchpad\".") {
		t.Fatalf("expected conversation title in additional context, got %q", output.AdditionalContext)
	}
	if !strings.Contains(output.AdditionalContext, "Workspace ID: `workspace-1`.") {
		t.Fatalf("expected workspace id in additional context, got %q", output.AdditionalContext)
	}
	if !strings.Contains(output.AdditionalContext, "Workspace name: \"Launchpad\".") {
		t.Fatalf("expected workspace name in additional context, got %q", output.AdditionalContext)
	}
	if !strings.Contains(output.AdditionalContext, "Agent profile: ID `agent-user-1`, type `agent`, display name \"Agent User\", handle `agent-user-1`, status `active`.") {
		t.Fatalf("expected agent profile in additional context, got %q", output.AdditionalContext)
	}
	if !strings.Contains(output.AdditionalContext, "Agent metadata:") || !strings.Contains(output.AdditionalContext, "session ID `session-1`") || !strings.Contains(output.AdditionalContext, "client `codex`") || !strings.Contains(output.AdditionalContext, "cwd `"+canonicalRepoDir+"`") {
		t.Fatalf("expected session metadata in additional context, got %q", output.AdditionalContext)
	}
	if strings.Contains(output.AdditionalContext, "Access policy:") {
		t.Fatalf("did not expect access policy in additional context, got %q", output.AdditionalContext)
	}
	if strings.Contains(output.AdditionalContext, "Use the Teraslack MCP tools") {
		t.Fatalf("did not expect generic MCP guidance in additional context, got %q", output.AdditionalContext)
	}
	if patchAgentCalls != 1 {
		t.Fatalf("patchAgentCalls = %d, want 1", patchAgentCalls)
	}
	createdMetadata, _ := createdAgentRequest["metadata"].(map[string]any)
	sessionMetadata, _ := createdMetadata["teraslack_session"].(map[string]any)
	if sessionMetadata["session_id"] != "session-1" {
		t.Fatalf("create agent metadata session_id = %#v", sessionMetadata["session_id"])
	}
	if sessionMetadata["client"] != "codex" {
		t.Fatalf("create agent metadata client = %#v", sessionMetadata["client"])
	}
	if sessionMetadata["cwd"] != canonicalRepoDir {
		t.Fatalf("create agent metadata cwd = %#v", sessionMetadata["cwd"])
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		if sessionMetadata["hostname"] != strings.TrimSpace(hostname) {
			t.Fatalf("create agent metadata hostname = %#v", sessionMetadata["hostname"])
		}
		if !strings.Contains(output.AdditionalContext, "host `"+strings.TrimSpace(hostname)+"`") {
			t.Fatalf("expected hostname in additional context, got %q", output.AdditionalContext)
		}
	}

	record, ok, err := loadAgentSessionRecord("codex", "session-1")
	if err != nil {
		t.Fatalf("loadAgentSessionRecord() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected session record to be written")
	}
	if record.AgentID != "agent-user-1" {
		t.Fatalf("record.AgentID = %q", record.AgentID)
	}
	if record.AgentToken != "agent-token-1" {
		t.Fatalf("record.AgentToken = %q", record.AgentToken)
	}
	if record.OwnerType != "workspace" || record.OwnerWorkspaceID != "workspace-1" {
		t.Fatalf("unexpected owner info: %+v", record)
	}

	bridgePath, err := pidSessionRecordPath(os.Getppid())
	if err != nil {
		t.Fatalf("pidSessionRecordPath() error = %v", err)
	}
	if _, err := os.Stat(bridgePath); err != nil {
		t.Fatalf("expected PID bridge to exist: %v", err)
	}

	if got := createdAgentRequest["owner_type"]; got != "workspace" {
		t.Fatalf("create agent owner_type = %#v", got)
	}
	if got := createdAgentRequest["owner_workspace_id"]; got != "workspace-1" {
		t.Fatalf("create agent owner_workspace_id = %#v", got)
	}
	userIDs, _ := addedParticipantsRequest["user_ids"].([]any)
	if len(userIDs) != 1 || userIDs[0] != "agent-user-1" {
		t.Fatalf("add participants user_ids = %#v", addedParticipantsRequest["user_ids"])
	}
}

func TestInitializeAgentSessionFromHookReusesAgentOnResume(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")

	repoDir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if _, err := upsertDirectoryLink(repoDir, "conversation-1", testNow()); err != nil {
		t.Fatalf("upsertDirectoryLink() error = %v", err)
	}

	createAgentCalls := 0
	patchAgentCalls := 0
	var currentAgentMetadata map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer human-session-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/conversations/conversation-1":
			writeJSONResponse(t, w, map[string]any{
				"id":                 "conversation-1",
				"workspace_id":       "workspace-1",
				"access_policy":      "workspace",
				"participant_count":  1,
				"created_by_user_id": "human-1",
				"archived":           false,
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/workspace-1":
			writeJSONResponse(t, w, map[string]any{
				"id":                 "workspace-1",
				"slug":               "workspace-1",
				"name":               "Workspace One",
				"created_by_user_id": "human-1",
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/agents":
			createAgentCalls++
			writeJSONResponse(t, w, map[string]any{
				"agent": map[string]any{
					"user": map[string]any{
						"id":             "agent-user-1",
						"principal_type": "agent",
						"status":         "active",
						"profile": map[string]any{
							"handle":       "agent-user-1",
							"display_name": "Agent User",
						},
					},
					"owner_type":         "workspace",
					"owner_workspace_id": "workspace-1",
					"mode":               "safe_write",
					"created_by_user_id": "human-1",
					"created_at":         "2026-04-07T00:00:00Z",
					"updated_at":         "2026-04-07T00:00:00Z",
				},
				"api_key": map[string]any{
					"id":         "agent-key-1",
					"token":      "agent-token-1",
					"scope_type": "global",
					"created_at": "2026-04-07T00:00:00Z",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/agents/agent-user-1":
			payload := map[string]any{
				"user": map[string]any{
					"id":             "agent-user-1",
					"principal_type": "agent",
					"status":         "active",
					"profile": map[string]any{
						"handle":       "agent-user-1",
						"display_name": "Agent User",
					},
				},
				"owner_type":         "workspace",
				"owner_workspace_id": "workspace-1",
				"mode":               "safe_write",
				"created_by_user_id": "human-1",
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			}
			if currentAgentMetadata != nil {
				payload["metadata"] = currentAgentMetadata
			}
			writeJSONResponse(t, w, payload)
		case r.Method == http.MethodPatch && r.URL.Path == "/agents/agent-user-1":
			patchAgentCalls++
			var request api.UpdateAgentRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode patch agent body: %v", err)
			}
			if request.Metadata == nil {
				t.Fatal("expected patch request metadata")
			}
			currentAgentMetadata = *request.Metadata
			writeJSONResponse(t, w, map[string]any{
				"user": map[string]any{
					"id":             "agent-user-1",
					"principal_type": "agent",
					"status":         "active",
					"profile": map[string]any{
						"handle":       "agent-user-1",
						"display_name": "Agent User",
					},
				},
				"owner_type":         "workspace",
				"owner_workspace_id": "workspace-1",
				"mode":               "safe_write",
				"metadata":           currentAgentMetadata,
				"created_by_user_id": "human-1",
				"created_at":         "2026-04-07T00:00:00Z",
				"updated_at":         "2026-04-07T00:00:00Z",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := saveFileConfig(fileConfig{
		BaseURL:      server.URL,
		SessionToken: "human-session-token",
		UserID:       "human-1",
	}); err != nil {
		t.Fatalf("saveFileConfig() error = %v", err)
	}
	canonicalRepoDir, err := canonicalLinkPath(repoDir)
	if err != nil {
		t.Fatalf("canonicalLinkPath() error = %v", err)
	}

	firstOutput, err := initializeAgentSessionFromHook(context.Background(), "codex", sessionStartHookInput{
		SessionID: "session-1",
		Cwd:       repoDir,
		Source:    "startup",
	})
	if err != nil {
		t.Fatalf("first initializeAgentSessionFromHook() error = %v", err)
	}
	if strings.TrimSpace(firstOutput.AdditionalContext) == "" {
		t.Fatalf("expected additional context on first hook run")
	}
	if !strings.Contains(firstOutput.AdditionalContext, "Conversation ID: `conversation-1`.") {
		t.Fatalf("expected conversation id in first additional context, got %q", firstOutput.AdditionalContext)
	}
	if !strings.Contains(firstOutput.AdditionalContext, "Workspace ID: `workspace-1`.") {
		t.Fatalf("expected workspace id in first additional context, got %q", firstOutput.AdditionalContext)
	}
	if !strings.Contains(firstOutput.AdditionalContext, "Workspace name: \"Workspace One\".") {
		t.Fatalf("expected workspace name in first additional context, got %q", firstOutput.AdditionalContext)
	}
	if !strings.Contains(firstOutput.AdditionalContext, "Agent profile: ID `agent-user-1`, type `agent`, display name \"Agent User\", handle `agent-user-1`, status `active`.") {
		t.Fatalf("expected agent profile in first additional context, got %q", firstOutput.AdditionalContext)
	}
	if !strings.Contains(firstOutput.AdditionalContext, "session ID `session-1`") || !strings.Contains(firstOutput.AdditionalContext, "cwd `"+canonicalRepoDir+"`") {
		t.Fatalf("expected session metadata in first additional context, got %q", firstOutput.AdditionalContext)
	}

	recordBeforeResume, ok, err := loadAgentSessionRecord("codex", "session-1")
	if err != nil {
		t.Fatalf("loadAgentSessionRecord() before resume error = %v", err)
	}
	if !ok {
		t.Fatalf("expected session record before resume")
	}

	secondOutput, err := initializeAgentSessionFromHook(context.Background(), "codex", sessionStartHookInput{
		SessionID: "session-1",
		Cwd:       repoDir,
		Source:    "resume",
	})
	if err != nil {
		t.Fatalf("second initializeAgentSessionFromHook() error = %v", err)
	}
	if strings.TrimSpace(secondOutput.AdditionalContext) == "" {
		t.Fatalf("expected additional context on resume hook run")
	}
	if !strings.Contains(secondOutput.AdditionalContext, "Conversation ID: `conversation-1`.") {
		t.Fatalf("expected conversation id in resume additional context, got %q", secondOutput.AdditionalContext)
	}
	if !strings.Contains(secondOutput.AdditionalContext, "Workspace ID: `workspace-1`.") {
		t.Fatalf("expected workspace id in resume additional context, got %q", secondOutput.AdditionalContext)
	}
	if !strings.Contains(secondOutput.AdditionalContext, "Workspace name: \"Workspace One\".") {
		t.Fatalf("expected workspace name in resume additional context, got %q", secondOutput.AdditionalContext)
	}
	if !strings.Contains(secondOutput.AdditionalContext, "Agent profile: ID `agent-user-1`, type `agent`, display name \"Agent User\", handle `agent-user-1`, status `active`.") {
		t.Fatalf("expected agent profile in resume additional context, got %q", secondOutput.AdditionalContext)
	}
	if !strings.Contains(secondOutput.AdditionalContext, "session ID `session-1`") || !strings.Contains(secondOutput.AdditionalContext, "cwd `"+canonicalRepoDir+"`") {
		t.Fatalf("expected session metadata in resume additional context, got %q", secondOutput.AdditionalContext)
	}

	recordAfterResume, ok, err := loadAgentSessionRecord("codex", "session-1")
	if err != nil {
		t.Fatalf("loadAgentSessionRecord() after resume error = %v", err)
	}
	if !ok {
		t.Fatalf("expected session record after resume")
	}
	if createAgentCalls != 1 {
		t.Fatalf("createAgentCalls = %d, want 1", createAgentCalls)
	}
	if patchAgentCalls != 1 {
		t.Fatalf("patchAgentCalls = %d, want 1", patchAgentCalls)
	}
	if recordAfterResume.AgentID != recordBeforeResume.AgentID {
		t.Fatalf("AgentID changed across resume: before=%q after=%q", recordBeforeResume.AgentID, recordAfterResume.AgentID)
	}
	if recordAfterResume.AgentToken != recordBeforeResume.AgentToken {
		t.Fatalf("AgentToken changed across resume: before=%q after=%q", recordBeforeResume.AgentToken, recordAfterResume.AgentToken)
	}
	if recordAfterResume.HookSource != "resume" {
		t.Fatalf("HookSource = %q, want resume", recordAfterResume.HookSource)
	}
}

func TestLoadCurrentAgentSessionRecordUsesPIDBridge(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_THREAD_ID", "")

	if err := saveAgentSessionRecord(agentSessionRecord{
		Version:    agentSessionStoreVersion,
		Client:     "claude",
		SessionID:  "session-2",
		BaseURL:    "https://api.example.com",
		AgentID:    "agent-user-2",
		AgentToken: "agent-token-2",
		CreatedAt:  "2026-04-07T00:00:00Z",
		UpdatedAt:  "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("saveAgentSessionRecord() error = %v", err)
	}
	if err := savePIDSessionRecord(pidSessionRecord{
		Version:   agentSessionStoreVersion,
		Client:    "claude",
		SessionID: "session-2",
		PID:       os.Getppid(),
		UpdatedAt: "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("savePIDSessionRecord() error = %v", err)
	}

	record, err := loadCurrentAgentSessionRecord()
	if err != nil {
		t.Fatalf("loadCurrentAgentSessionRecord() error = %v", err)
	}
	if record.AgentToken != "agent-token-2" {
		t.Fatalf("record.AgentToken = %q", record.AgentToken)
	}
}

func TestLoadCurrentAgentSessionRecordUsesCodexThreadIDEnv(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_THREAD_ID", "session-env")

	if err := saveAgentSessionRecord(agentSessionRecord{
		Version:    agentSessionStoreVersion,
		Client:     "codex",
		SessionID:  "session-env",
		BaseURL:    "https://api.example.com",
		AgentID:    "agent-user-env",
		AgentToken: "agent-token-env",
		CreatedAt:  "2026-04-07T00:00:00Z",
		UpdatedAt:  "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("saveAgentSessionRecord() error = %v", err)
	}

	record, err := loadCurrentAgentSessionRecord()
	if err != nil {
		t.Fatalf("loadCurrentAgentSessionRecord() error = %v", err)
	}
	if record.AgentToken != "agent-token-env" {
		t.Fatalf("record.AgentToken = %q", record.AgentToken)
	}
}

func TestLoadCurrentAgentSessionRecordPrefersEnvOverPIDBridge(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_THREAD_ID", "session-env")

	if err := saveAgentSessionRecord(agentSessionRecord{
		Version:    agentSessionStoreVersion,
		Client:     "codex",
		SessionID:  "session-env",
		BaseURL:    "https://api.example.com",
		AgentID:    "agent-user-env",
		AgentToken: "agent-token-env",
		CreatedAt:  "2026-04-07T00:00:00Z",
		UpdatedAt:  "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("saveAgentSessionRecord(env) error = %v", err)
	}
	if err := saveAgentSessionRecord(agentSessionRecord{
		Version:    agentSessionStoreVersion,
		Client:     "codex",
		SessionID:  "session-pid",
		BaseURL:    "https://api.example.com",
		AgentID:    "agent-user-pid",
		AgentToken: "agent-token-pid",
		CreatedAt:  "2026-04-07T00:00:00Z",
		UpdatedAt:  "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("saveAgentSessionRecord(pid) error = %v", err)
	}
	if err := savePIDSessionRecord(pidSessionRecord{
		Version:   agentSessionStoreVersion,
		Client:    "codex",
		SessionID: "session-pid",
		PID:       os.Getppid(),
		UpdatedAt: "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("savePIDSessionRecord() error = %v", err)
	}

	record, err := loadCurrentAgentSessionRecord()
	if err != nil {
		t.Fatalf("loadCurrentAgentSessionRecord() error = %v", err)
	}
	if record.SessionID != "session-env" {
		t.Fatalf("record.SessionID = %q", record.SessionID)
	}
	if record.AgentToken != "agent-token-env" {
		t.Fatalf("record.AgentToken = %q", record.AgentToken)
	}
}

func TestInitializeAgentSessionFromHookIgnoresSubagentPayloads(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())

	if err := savePIDSessionRecord(pidSessionRecord{
		Version:   agentSessionStoreVersion,
		Client:    "codex",
		SessionID: "session-existing",
		PID:       os.Getppid(),
		UpdatedAt: "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("savePIDSessionRecord() error = %v", err)
	}

	if _, err := initializeAgentSessionFromHook(context.Background(), "codex", sessionStartHookInput{
		SessionID: "session-subagent",
		Cwd:       ".",
		AgentID:   "subagent-1",
	}); err != nil {
		t.Fatalf("initializeAgentSessionFromHook() error = %v", err)
	}

	path, err := pidSessionRecordPath(os.Getppid())
	if err != nil {
		t.Fatalf("pidSessionRecordPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var bridge pidSessionRecord
	if err := json.Unmarshal(data, &bridge); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if bridge.SessionID != "session-existing" {
		t.Fatalf("bridge.SessionID = %q", bridge.SessionID)
	}
}

func TestMCPToolCallUsesSessionAgentToken(t *testing.T) {
	t.Setenv("TERASLACK_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_THREAD_ID", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/me" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer agent-token-3" {
			t.Fatalf("Authorization header = %q", got)
		}
		writeJSONResponse(t, w, map[string]any{
			"user": map[string]any{
				"id":             "agent-user-3",
				"principal_type": "agent",
				"status":         "active",
				"profile": map[string]any{
					"handle":       "agent-user-3",
					"display_name": "Agent User",
				},
			},
			"workspaces": []any{},
		})
	}))
	defer server.Close()

	if err := saveAgentSessionRecord(agentSessionRecord{
		Version:    agentSessionStoreVersion,
		Client:     "codex",
		SessionID:  "session-3",
		BaseURL:    server.URL,
		AgentID:    "agent-user-3",
		AgentToken: "agent-token-3",
		CreatedAt:  "2026-04-07T00:00:00Z",
		UpdatedAt:  "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("saveAgentSessionRecord() error = %v", err)
	}
	if err := savePIDSessionRecord(pidSessionRecord{
		Version:   agentSessionStoreVersion,
		Client:    "codex",
		SessionID: "session-3",
		PID:       os.Getppid(),
		UpdatedAt: "2026-04-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("savePIDSessionRecord() error = %v", err)
	}

	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	mcpServer, err := newMCPServer(cli)
	if err != nil {
		t.Fatalf("newMCPServer() error = %v", err)
	}

	result, err := mcpServer.handleToolCall(context.Background(), json.RawMessage(`{"name":"profile_get","arguments":{}}`))
	if err != nil {
		t.Fatalf("handleToolCall() error = %v", err)
	}
	if _, ok := result["structuredContent"]; !ok {
		t.Fatalf("expected structuredContent in %+v", result)
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("json encode: %v", err)
	}
}

func testNow() time.Time {
	return time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
}
