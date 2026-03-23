package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/teraslackmcp"
)

func TestComposeE2E_CodexPeerChat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compose e2e test in short mode")
	}
	if os.Getenv("TERASLACK_E2E") != "1" {
		t.Skip("set TERASLACK_E2E=1 to run compose-backed e2e tests")
	}

	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI is required: %v", err)
	}

	currentHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home directory: %v", err)
	}
	authPath := filepath.Join(currentHome, ".codex", "auth.json")
	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("missing Codex auth file at %s: %v", authPath, err)
	}

	ctx := context.Background()
	baseURL := e2eBaseURL()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Fatal("DATABASE_URL is required for compose-backed e2e tests")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	owner := bootstrapOwnerUser(t, ctx, pool)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	ownerToken := createSessionToken(t, ctx, pool, owner.TeamID, owner.ID)

	agentAEmail := uniqueEmail("codex-a")
	agentBEmail := uniqueEmail("codex-b")
	agentA := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("codex-a"),
		Email:         agentAEmail,
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})
	agentB := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("codex-b"),
		Email:         agentBEmail,
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})

	_, agentAKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Codex A Key",
		TeamID:      owner.TeamID,
		PrincipalID: agentA.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsMembersWrite,
		},
	})
	_, agentBKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Codex B Key",
		TeamID:      owner.TeamID,
		PrincipalID: agentB.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsMembersWrite,
		},
	})

	dm := createConversationViaHTTP(t, httpClient, baseURL, agentAKey, domain.CreateConversationParams{
		Type:      domain.ConversationTypeIM,
		CreatorID: agentA.ID,
	})
	inviteUsersViaHTTP(t, httpClient, baseURL, agentAKey, dm.ID, []string{agentB.ID})

	senderHome := createIsolatedCodexHome(t, authPath)
	receiverHome := createIsolatedCodexHome(t, authPath)

	senderMCP := startTeraslackMCPHTTPServer(t, teraslackmcp.Config{
		BaseURL:       baseURL,
		APIKey:        agentAKey,
		TeamID:        owner.TeamID,
		UserID:        agentA.ID,
		UserName:      agentA.Name,
		UserEmail:     agentA.Email,
		PeerUserID:    agentB.ID,
		PeerUserName:  agentB.Name,
		PeerUserEmail: agentB.Email,
		ChannelID:     dm.ID,
	})
	defer senderMCP.Close()
	receiverMCP := startTeraslackMCPHTTPServer(t, teraslackmcp.Config{
		BaseURL:       baseURL,
		APIKey:        agentBKey,
		TeamID:        owner.TeamID,
		UserID:        agentB.ID,
		UserName:      agentB.Name,
		UserEmail:     agentB.Email,
		PeerUserID:    agentA.ID,
		PeerUserName:  agentA.Name,
		PeerUserEmail: agentA.Email,
		ChannelID:     dm.ID,
	})
	defer receiverMCP.Close()

	registerTeraslackMCPURL(t, senderHome, senderMCP.URL)
	registerTeraslackMCPURL(t, receiverHome, receiverMCP.URL)

	workDir := t.TempDir()
	senderSchema := filepath.Join(workDir, "sender-schema.json")
	receiverSchema := filepath.Join(workDir, "receiver-schema.json")
	writeFile(t, senderSchema, []byte(`{
  "type": "object",
  "required": ["status", "sender_email", "sent_text", "channel_id"],
  "properties": {
    "status": {"type": "string"},
    "sender_email": {"type": "string"},
    "sent_text": {"type": "string"},
    "channel_id": {"type": "string"}
  },
  "additionalProperties": false
}`))
	writeFile(t, receiverSchema, []byte(`{
  "type": "object",
  "required": ["status", "receiver_email", "sender_email", "received_text", "channel_id"],
  "properties": {
    "status": {"type": "string"},
    "receiver_email": {"type": "string"},
    "sender_email": {"type": "string"},
    "received_text": {"type": "string"},
    "channel_id": {"type": "string"}
  },
  "additionalProperties": false
}`))

	senderOut := filepath.Join(workDir, "sender-output.json")
	receiverOut := filepath.Join(workDir, "receiver-output.json")
	senderLog := filepath.Join(workDir, "sender.log")
	receiverLog := filepath.Join(workDir, "receiver.log")

	receiverPrompt := strings.TrimSpace(`
Use the teraslack MCP server for this task.
1. Call whoami.
2. Call wait_for_notification with {"kind":"direct_message","body_preview":"hi","timeout_seconds":60}.
3. Return JSON only matching the schema with:
   - status = "received"
   - receiver_email = your email from whoami
   - sender_email = the actor_email from wait_for_notification
   - received_text = the body_preview from wait_for_notification
   - channel_id = the configured conversation id from whoami
Do not send any messages and do not include markdown.
`)
	senderPrompt := strings.TrimSpace(`
Use the teraslack MCP server for this task.
1. Call whoami.
2. Call send_message exactly once with {"text":"hi"}.
3. Return JSON only matching the schema with:
   - status = "sent"
   - sender_email = your email from whoami
   - sent_text = "hi"
   - channel_id = the configured conversation id from whoami
Do not send any other messages and do not include markdown.
`)

	receiverCmd, receiverCancel := newCodexExecCommand(t, receiverHome, receiverPrompt, receiverSchema, receiverOut, receiverLog)
	defer receiverCancel()
	if err := receiverCmd.Start(); err != nil {
		t.Fatalf("start receiver codex exec: %v", err)
	}

	time.Sleep(2 * time.Second)

	senderCmd, senderCancel := newCodexExecCommand(t, senderHome, senderPrompt, senderSchema, senderOut, senderLog)
	defer senderCancel()
	if err := senderCmd.Run(); err != nil {
		t.Fatalf("sender codex exec failed: %v\nsender log:\n%s", err, mustReadFile(t, senderLog))
	}
	if err := receiverCmd.Wait(); err != nil {
		t.Fatalf("receiver codex exec failed: %v\nreceiver log:\n%s", err, mustReadFile(t, receiverLog))
	}

	var senderResult struct {
		Status      string `json:"status"`
		SenderEmail string `json:"sender_email"`
		SentText    string `json:"sent_text"`
		ChannelID   string `json:"channel_id"`
	}
	var receiverResult struct {
		Status        string `json:"status"`
		ReceiverEmail string `json:"receiver_email"`
		SenderEmail   string `json:"sender_email"`
		ReceivedText  string `json:"received_text"`
		ChannelID     string `json:"channel_id"`
	}
	readJSONFile(t, senderOut, &senderResult)
	readJSONFile(t, receiverOut, &receiverResult)

	if senderResult.Status != "sent" || senderResult.SentText != "hi" || senderResult.SenderEmail != agentA.Email || senderResult.ChannelID != dm.ID {
		t.Fatalf("unexpected sender result: %+v", senderResult)
	}
	if receiverResult.Status != "received" || receiverResult.ReceivedText != "hi" || receiverResult.SenderEmail != agentA.Email || receiverResult.ReceiverEmail != agentB.Email || receiverResult.ChannelID != dm.ID {
		t.Fatalf("unexpected receiver result: %+v", receiverResult)
	}

	events := listEventsViaHTTP(t, httpClient, baseURL, agentBKey)
	found := false
	for _, event := range events {
		if event.Type != domain.EventTypeConversationMessageCreated || event.ResourceType != domain.ResourceTypeConversation || event.ResourceID != dm.ID {
			continue
		}
		var payload struct {
			TS        string `json:"ts"`
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Text      string `json:"text"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode event payload: %v", err)
		}
		if payload.Text == "hi" && payload.UserID == agentA.ID && payload.ChannelID == dm.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected message.created event for receiver, got %+v", events)
	}

	history := listChannelMessagesViaHTTP(t, httpClient, baseURL, agentAKey, dm.ID)
	var topLevel []domain.Message
	for _, msg := range history {
		if msg.IsDeleted || msg.ThreadTS != nil {
			continue
		}
		topLevel = append(topLevel, msg)
	}
	if len(topLevel) != 1 {
		t.Fatalf("top-level message count = %d, want 1 (history=%+v)", len(topLevel), topLevel)
	}
	if topLevel[0].Text != "hi" || topLevel[0].UserID != agentA.ID {
		t.Fatalf("unexpected message in teraslack history: %+v", topLevel[0])
	}
}

func startTeraslackMCPHTTPServer(t *testing.T, cfg teraslackmcp.Config) *httptest.Server {
	t.Helper()
	server, err := teraslackmcp.NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("create teraslack MCP server: %v", err)
	}
	return httptest.NewServer(server.HTTPHandler())
}

func registerTeraslackMCPURL(t *testing.T, homeDir, serverURL string) {
	t.Helper()

	cmd := exec.Command("codex", "mcp", "add", "teraslack", "--url", serverURL)
	cmd.Env = withHomeEnv(homeDir)
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("register teraslack MCP URL: %v\n%s", err, output)
	}
}

func createIsolatedCodexHome(t *testing.T, sourceAuth string) string {
	t.Helper()

	homeDir := filepath.Join(t.TempDir(), "home")
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir isolated codex dir: %v", err)
	}

	copyFile(t, sourceAuth, filepath.Join(codexDir, "auth.json"), 0o600)
	config := fmt.Sprintf(`model = "gpt-5.4"
model_reasoning_effort = "medium"
personality = "pragmatic"

[projects.%q]
trust_level = "trusted"
`, repoRoot(t))
	writeFile(t, filepath.Join(codexDir, "config.toml"), []byte(config))

	return homeDir
}

func newCodexExecCommand(t *testing.T, homeDir, prompt, schemaPath, outputPath, logPath string) (*exec.Cmd, context.CancelFunc) {
	t.Helper()

	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create codex log file %s: %v", logPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	cmd := exec.CommandContext(ctx, "codex", "exec",
		"-C", repoRoot(t),
		"--color", "never",
		"--output-schema", schemaPath,
		"--output-last-message", outputPath,
		prompt,
	)
	cmd.Env = withHomeEnv(homeDir)
	cmd.Dir = repoRoot(t)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return cmd, func() {
		cancel()
		_ = logFile.Close()
	}
}

func listChannelMessagesViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth, channelID string) []domain.Message {
	t.Helper()
	var resp struct {
		Items []domain.Message `json:"items"`
	}
	url := fmt.Sprintf("%s/messages?conversation_id=%s&limit=100", baseURL, channelID)
	doJSON(t, httpClient, http.MethodGet, url, auth, nil, &resp)
	return resp.Items
}

func listEventsViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth string) []domain.ExternalEvent {
	t.Helper()
	var resp struct {
		Items []domain.ExternalEvent `json:"items"`
	}
	doJSON(t, httpClient, http.MethodGet, baseURL+"/events?limit=100", auth, nil, &resp)
	return resp.Items
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func copyFile(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	writeFileMode(t, dst, data, mode)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	writeFileMode(t, path, data, 0o600)
}

func writeFileMode(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()
	data := mustReadFile(t, path)
	if err := json.Unmarshal([]byte(data), out); err != nil {
		t.Fatalf("unmarshal %s: %v\nbody=%s", path, err, data)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func withHomeEnv(homeDir string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	prefixes := []string{"HOME=", "XDG_CONFIG_HOME=", "XDG_DATA_HOME=", "XDG_STATE_HOME=", "XDG_CACHE_HOME="}
	for _, entry := range env {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, entry)
		}
	}
	out = append(out,
		"HOME="+homeDir,
		"XDG_CONFIG_HOME="+filepath.Join(homeDir, ".config"),
		"XDG_DATA_HOME="+filepath.Join(homeDir, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(homeDir, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(homeDir, ".cache"),
	)
	return out
}
