package openapicli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnsuh/teraslack/server/internal/api"
)

const (
	agentSessionStoreVersion = 1
)

type sessionStartHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source,omitempty"`
	Model          string `json:"model,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	AgentType      string `json:"agent_type,omitempty"`
}

type sessionStartHookOutput struct {
	AdditionalContext string
	SystemMessage     string
}

type agentSessionRecord struct {
	Version                  int    `json:"version"`
	Client                   string `json:"client"`
	SessionID                string `json:"session_id"`
	HookSource               string `json:"hook_source,omitempty"`
	Cwd                      string `json:"cwd"`
	LinkedPath               string `json:"linked_path"`
	BaseURL                  string `json:"base_url"`
	ConversationID           string `json:"conversation_id"`
	ConversationWorkspaceID  string `json:"conversation_workspace_id,omitempty"`
	ConversationAccessPolicy string `json:"conversation_access_policy,omitempty"`
	CreatedByUserID          string `json:"created_by_user_id,omitempty"`
	OwnerType                string `json:"owner_type"`
	OwnerWorkspaceID         string `json:"owner_workspace_id,omitempty"`
	AgentID                  string `json:"agent_id"`
	AgentToken               string `json:"agent_token"`
	AgentMode                string `json:"agent_mode,omitempty"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

type pidSessionRecord struct {
	Version   int    `json:"version"`
	Client    string `json:"client"`
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	UpdatedAt string `json:"updated_at"`
}

func (c *CLI) runHook(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" {
		c.printHookHelp(stdout)
		return 0
	}

	switch strings.TrimSpace(args[0]) {
	case "session-start":
		return c.runHookSessionStart(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown hook command %q\n\n", args[0])
		c.printHookHelp(stderr)
		return 2
	}
}

func (c *CLI) printHookHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:\n  teraslack hook session-start --client <codex|claude>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Internal hook entrypoints used by the Teraslack installer for Codex and Claude Code.")
}

func (c *CLI) runHookSessionStart(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	var client string

	fs := flag.NewFlagSet("hook session-start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&client, "client", "", "Hook client name: codex or claude.")
	fs.Usage = func() {
		c.printHookHelp(stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	client = strings.TrimSpace(strings.ToLower(client))
	switch client {
	case "codex", "claude":
	default:
		fmt.Fprintln(stderr, "missing or invalid --client; expected codex or claude")
		return 2
	}

	var payload sessionStartHookInput
	if err := json.NewDecoder(os.Stdin).Decode(&payload); err != nil {
		c.writeSessionStartHookOutput(stdout, sessionStartHookOutput{
			SystemMessage: fmt.Sprintf("Teraslack hook setup skipped: could not decode %s SessionStart payload.", client),
		})
		return 0
	}

	output, err := initializeAgentSessionFromHook(ctx, client, payload)
	if err != nil {
		c.writeSessionStartHookOutput(stdout, sessionStartHookOutput{
			SystemMessage: fmt.Sprintf("Teraslack session agent setup failed: %v", err),
		})
		return 0
	}
	c.writeSessionStartHookOutput(stdout, output)
	return 0
}

func (c *CLI) writeSessionStartHookOutput(w io.Writer, output sessionStartHookOutput) {
	if strings.TrimSpace(output.AdditionalContext) == "" && strings.TrimSpace(output.SystemMessage) == "" {
		return
	}

	payload := map[string]any{}
	if strings.TrimSpace(output.SystemMessage) != "" {
		payload["systemMessage"] = strings.TrimSpace(output.SystemMessage)
	}
	if strings.TrimSpace(output.AdditionalContext) != "" {
		payload["hookSpecificOutput"] = map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": strings.TrimSpace(output.AdditionalContext),
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(w, string(data))
}

func initializeAgentSessionFromHook(ctx context.Context, client string, payload sessionStartHookInput) (sessionStartHookOutput, error) {
	if strings.TrimSpace(payload.AgentID) != "" {
		return sessionStartHookOutput{}, nil
	}

	parentPID := os.Getppid()
	if parentPID > 0 {
		_ = removePIDSessionRecord(parentPID)
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		return sessionStartHookOutput{}, fmt.Errorf("missing session_id in hook payload")
	}

	cwd := strings.TrimSpace(payload.Cwd)
	if cwd == "" {
		cwd = "."
	}
	canonicalCwd, err := canonicalLinkPath(cwd)
	if err != nil {
		return sessionStartHookOutput{}, fmt.Errorf("resolve session cwd: %w", err)
	}

	link, ok, err := resolveDirectoryLink(canonicalCwd)
	if err != nil {
		return sessionStartHookOutput{}, fmt.Errorf("resolve linked conversation: %w", err)
	}
	if !ok {
		return sessionStartHookOutput{}, nil
	}

	cfg, err := loadFileConfig()
	if err != nil {
		return sessionStartHookOutput{}, fmt.Errorf("load CLI config: %w", err)
	}
	baseURL := canonicalBaseURL(firstNonEmpty(cfg.BaseURL, defaultAuthBaseURL))
	humanToken := bearerToken(cfg.SessionToken, cfg.APIKey)
	if strings.TrimSpace(humanToken) == "" {
		return sessionStartHookOutput{}, fmt.Errorf("linked directory %s is not signed in; run `teraslack signin email --email <email>` first", link.Path)
	}

	conversation, err := getConversationForSessionStart(ctx, baseURL, humanToken, link.ConversationID)
	if err != nil {
		return sessionStartHookOutput{}, fmt.Errorf("load linked conversation %s: %w", link.ConversationID, err)
	}

	record, err := ensureAgentSessionRecord(ctx, client, payload, cfg, baseURL, humanToken, canonicalCwd, link, conversation)
	if err != nil {
		return sessionStartHookOutput{}, err
	}

	if parentPID > 0 {
		if err := savePIDSessionRecord(pidSessionRecord{
			Version:   agentSessionStoreVersion,
			Client:    client,
			SessionID: sessionID,
			PID:       parentPID,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return sessionStartHookOutput{}, fmt.Errorf("save PID session bridge: %w", err)
		}
	}

	if err := saveAgentSessionRecord(record); err != nil {
		return sessionStartHookOutput{}, fmt.Errorf("save Teraslack session agent: %w", err)
	}

	workspace := loadSessionStartWorkspace(ctx, baseURL, humanToken, record.ConversationWorkspaceID)
	agent := loadSessionStartAgent(ctx, baseURL, humanToken, record.AgentID)

	return sessionStartHookOutput{
		AdditionalContext: buildSessionStartAdditionalContext(record, conversation, workspace, agent),
	}, nil
}

func ensureAgentSessionRecord(ctx context.Context, client string, payload sessionStartHookInput, cfg fileConfig, baseURL, humanToken, canonicalCwd string, link directoryLink, conversation api.Conversation) (agentSessionRecord, error) {
	ownerType := "user"
	ownerWorkspaceID := ""
	if conversation.WorkspaceID != nil && strings.TrimSpace(*conversation.WorkspaceID) != "" {
		ownerType = "workspace"
		ownerWorkspaceID = strings.TrimSpace(*conversation.WorkspaceID)
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	record, ok, err := loadAgentSessionRecord(client, sessionID)
	if err != nil {
		return agentSessionRecord{}, fmt.Errorf("load existing Teraslack session agent: %w", err)
	}

	now := time.Now().UTC()
	if !ok || !canReuseAgentSessionRecord(record, client, sessionID, cfg.UserID, baseURL, link, conversation, ownerType, ownerWorkspaceID) {
		record, err = createAgentSessionRecord(ctx, client, sessionID, cfg.UserID, baseURL, humanToken, ownerType, ownerWorkspaceID)
		if err != nil {
			return agentSessionRecord{}, err
		}
		record.CreatedAt = now.Format(time.RFC3339)
	}

	if conversation.AccessPolicy == "members" {
		if err := addConversationParticipantsForSessionStart(ctx, baseURL, humanToken, conversation.ID, []string{record.AgentID}); err != nil {
			return agentSessionRecord{}, fmt.Errorf("add Teraslack agent %s to linked conversation %s: %w", record.AgentID, conversation.ID, err)
		}
	}

	record.Version = agentSessionStoreVersion
	record.Client = client
	record.SessionID = sessionID
	record.HookSource = strings.TrimSpace(payload.Source)
	record.Cwd = canonicalCwd
	record.LinkedPath = link.Path
	record.BaseURL = baseURL
	record.ConversationID = conversation.ID
	record.ConversationAccessPolicy = strings.TrimSpace(conversation.AccessPolicy)
	record.ConversationWorkspaceID = ownerWorkspaceID
	record.CreatedByUserID = firstNonEmpty(strings.TrimSpace(cfg.UserID), strings.TrimSpace(record.CreatedByUserID))
	record.OwnerType = ownerType
	record.OwnerWorkspaceID = ownerWorkspaceID
	record.UpdatedAt = now.Format(time.RFC3339)
	if strings.TrimSpace(record.CreatedAt) == "" {
		record.CreatedAt = record.UpdatedAt
	}

	return record, nil
}

func canReuseAgentSessionRecord(record agentSessionRecord, client, sessionID, userID, baseURL string, link directoryLink, conversation api.Conversation, ownerType, ownerWorkspaceID string) bool {
	if record.Version == 0 {
		return false
	}
	if strings.TrimSpace(record.Client) != client || strings.TrimSpace(record.SessionID) != sessionID {
		return false
	}
	if strings.TrimSpace(record.AgentID) == "" || strings.TrimSpace(record.AgentToken) == "" {
		return false
	}
	if strings.TrimSpace(record.BaseURL) != strings.TrimSpace(baseURL) {
		return false
	}
	if strings.TrimSpace(record.LinkedPath) != strings.TrimSpace(link.Path) {
		return false
	}
	if strings.TrimSpace(record.ConversationID) != strings.TrimSpace(conversation.ID) {
		return false
	}
	if strings.TrimSpace(record.OwnerType) != ownerType {
		return false
	}
	if strings.TrimSpace(record.OwnerWorkspaceID) != ownerWorkspaceID {
		return false
	}
	if strings.TrimSpace(userID) != "" && strings.TrimSpace(record.CreatedByUserID) != "" && strings.TrimSpace(record.CreatedByUserID) != strings.TrimSpace(userID) {
		return false
	}
	return true
}

func createAgentSessionRecord(ctx context.Context, client, sessionID, userID, baseURL, humanToken, ownerType, ownerWorkspaceID string) (agentSessionRecord, error) {
	request := api.CreateAgentRequest{
		OwnerType: ownerType,
		Mode:      "safe_write",
	}
	if strings.TrimSpace(ownerWorkspaceID) != "" {
		request.OwnerWorkspaceID = stringPtr(ownerWorkspaceID)
	}

	var response api.CreateAgentResponse
	if err := doJSONRequest(ctx, "POST", baseURL+"/agents", request, humanToken, &response); err != nil {
		return agentSessionRecord{}, fmt.Errorf("create Teraslack session agent for %s session %s: %w", client, sessionID, err)
	}

	return agentSessionRecord{
		Version:          agentSessionStoreVersion,
		Client:           client,
		SessionID:        sessionID,
		BaseURL:          baseURL,
		CreatedByUserID:  strings.TrimSpace(userID),
		OwnerType:        ownerType,
		OwnerWorkspaceID: strings.TrimSpace(ownerWorkspaceID),
		AgentID:          response.Agent.User.ID,
		AgentToken:       strings.TrimSpace(response.APIKey.Token),
		AgentMode:        strings.TrimSpace(response.Agent.Mode),
	}, nil
}

func getConversationForSessionStart(ctx context.Context, baseURL, authToken, conversationID string) (api.Conversation, error) {
	var conversation api.Conversation
	if err := doJSONRequest(ctx, "GET", fmt.Sprintf("%s/conversations/%s", baseURL, strings.TrimSpace(conversationID)), nil, authToken, &conversation); err != nil {
		return api.Conversation{}, err
	}
	return conversation, nil
}

func getWorkspaceForSessionStart(ctx context.Context, baseURL, authToken, workspaceID string) (api.Workspace, error) {
	var workspace api.Workspace
	if err := doJSONRequest(ctx, "GET", fmt.Sprintf("%s/workspaces/%s", baseURL, strings.TrimSpace(workspaceID)), nil, authToken, &workspace); err != nil {
		return api.Workspace{}, err
	}
	return workspace, nil
}

func getAgentForSessionStart(ctx context.Context, baseURL, authToken, agentID string) (api.Agent, error) {
	var agent api.Agent
	if err := doJSONRequest(ctx, "GET", fmt.Sprintf("%s/agents/%s", baseURL, strings.TrimSpace(agentID)), nil, authToken, &agent); err != nil {
		return api.Agent{}, err
	}
	return agent, nil
}

func addConversationParticipantsForSessionStart(ctx context.Context, baseURL, authToken, conversationID string, userIDs []string) error {
	return doJSONRequest(ctx, "POST", fmt.Sprintf("%s/conversations/%s/participants", baseURL, strings.TrimSpace(conversationID)), api.AddParticipantsRequest{
		UserIDs: userIDs,
	}, authToken, &api.CollectionResponse[api.User]{})
}

func loadSessionStartWorkspace(ctx context.Context, baseURL, authToken, workspaceID string) *api.Workspace {
	if strings.TrimSpace(workspaceID) == "" {
		return nil
	}

	workspace, err := getWorkspaceForSessionStart(ctx, baseURL, authToken, workspaceID)
	if err != nil {
		return nil
	}
	return &workspace
}

func loadSessionStartAgent(ctx context.Context, baseURL, authToken, agentID string) *api.Agent {
	if strings.TrimSpace(agentID) == "" {
		return nil
	}

	agent, err := getAgentForSessionStart(ctx, baseURL, authToken, agentID)
	if err != nil {
		return nil
	}
	return &agent
}

func buildSessionStartAdditionalContext(record agentSessionRecord, conversation api.Conversation, workspace *api.Workspace, agent *api.Agent) string {
	parts := []string{
		fmt.Sprintf("Conversation ID: `%s`.", strings.TrimSpace(record.ConversationID)),
	}

	if title := strings.TrimSpace(optionalStringValue(conversation.Title)); title != "" {
		parts = append(parts, fmt.Sprintf("Conversation title: %q.", title))
	}

	if workspaceID := strings.TrimSpace(record.ConversationWorkspaceID); workspaceID != "" {
		parts = append(parts, fmt.Sprintf("Workspace ID: `%s`.", workspaceID))
		if workspace != nil {
			if name := strings.TrimSpace(workspace.Name); name != "" {
				parts = append(parts, fmt.Sprintf("Workspace name: %q.", name))
			}
		}
	}

	profileParts := []string{
		fmt.Sprintf("ID `%s`", strings.TrimSpace(record.AgentID)),
	}
	if agent != nil {
		if principalType := strings.TrimSpace(agent.User.PrincipalType); principalType != "" {
			profileParts = append(profileParts, fmt.Sprintf("type `%s`", principalType))
		}
		if displayName := strings.TrimSpace(agent.User.Profile.DisplayName); displayName != "" {
			profileParts = append(profileParts, fmt.Sprintf("display name %q", displayName))
		}
		if handle := strings.TrimSpace(agent.User.Profile.Handle); handle != "" {
			profileParts = append(profileParts, fmt.Sprintf("handle `%s`", handle))
		}
		if status := strings.TrimSpace(agent.User.Status); status != "" {
			profileParts = append(profileParts, fmt.Sprintf("status `%s`", status))
		}
	}
	parts = append(parts, "Agent profile: "+strings.Join(profileParts, ", ")+".")

	return strings.Join(parts, " ")
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func agentSessionsRootPath() (string, error) {
	root, err := configDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "agent-sessions"), nil
}

func agentSessionsDirPath() (string, error) {
	root, err := agentSessionsRootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "sessions"), nil
}

func pidSessionsDirPath() (string, error) {
	root, err := agentSessionsRootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "by-client-pid"), nil
}

func agentSessionRecordPath(client, sessionID string) (string, error) {
	root, err := agentSessionsDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, safeFileComponent(client), safeFileComponent(sessionID)+".json"), nil
}

func pidSessionRecordPath(pid int) (string, error) {
	root, err := pidSessionsDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, fmt.Sprintf("%d.json", pid)), nil
}

func loadAgentSessionRecord(client, sessionID string) (agentSessionRecord, bool, error) {
	path, err := agentSessionRecordPath(client, sessionID)
	if err != nil {
		return agentSessionRecord{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return agentSessionRecord{}, false, nil
		}
		return agentSessionRecord{}, false, err
	}
	var record agentSessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return agentSessionRecord{}, false, err
	}
	return record, true, nil
}

func saveAgentSessionRecord(record agentSessionRecord) error {
	path, err := agentSessionRecordPath(record.Client, record.SessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func savePIDSessionRecord(record pidSessionRecord) error {
	path, err := pidSessionRecordPath(record.PID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func removePIDSessionRecord(pid int) error {
	path, err := pidSessionRecordPath(pid)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func loadCurrentAgentSessionRecord() (agentSessionRecord, error) {
	if client, sessionID, ok := currentSessionReferenceFromEnv(); ok {
		record, found, err := loadAgentSessionRecord(client, sessionID)
		if err != nil {
			return agentSessionRecord{}, err
		}
		if !found {
			return agentSessionRecord{}, fmt.Errorf("no Teraslack session agent record exists for active %s session", client)
		}
		return record, nil
	}

	pid := os.Getppid()
	if pid <= 0 {
		return agentSessionRecord{}, fmt.Errorf("could not determine parent process")
	}

	pidPath, err := pidSessionRecordPath(pid)
	if err != nil {
		return agentSessionRecord{}, err
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return agentSessionRecord{}, fmt.Errorf("no Teraslack session agent is active for this MCP session")
		}
		return agentSessionRecord{}, err
	}

	var bridge pidSessionRecord
	if err := json.Unmarshal(data, &bridge); err != nil {
		return agentSessionRecord{}, fmt.Errorf("decode PID session bridge: %w", err)
	}
	record, ok, err := loadAgentSessionRecord(strings.TrimSpace(bridge.Client), strings.TrimSpace(bridge.SessionID))
	if err != nil {
		return agentSessionRecord{}, err
	}
	if !ok {
		return agentSessionRecord{}, fmt.Errorf("no Teraslack session agent record exists for active MCP session")
	}
	return record, nil
}

func currentSessionReferenceFromEnv() (client string, sessionID string, ok bool) {
	if sessionID = strings.TrimSpace(os.Getenv("TERASLACK_SESSION_ID")); sessionID != "" {
		client = strings.TrimSpace(strings.ToLower(os.Getenv("TERASLACK_SESSION_CLIENT")))
		if client != "" {
			return client, sessionID, true
		}
	}

	if sessionID = strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")); sessionID != "" {
		return "codex", sessionID, true
	}

	return "", "", false
}

func safeFileComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return replacer.Replace(value)
}
