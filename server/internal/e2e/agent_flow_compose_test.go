package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suhjohn/teraslack/internal/domain"
	pgRepo "github.com/suhjohn/teraslack/internal/repository/postgres"
	"github.com/suhjohn/teraslack/internal/service"
)

func TestComposeE2E_AgentSessionFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compose e2e test in short mode")
	}
	if os.Getenv("TERASLACK_E2E") != "1" {
		t.Skip("set TERASLACK_E2E=1 to run compose-backed e2e tests")
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
	ownerToken := createSessionToken(t, ctx, pool, owner.WorkspaceID, owner.ID)

	agentA := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("agent-a"),
		Email:         uniqueEmail("agent-a"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})
	agentB := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("agent-b"),
		Email:         uniqueEmail("agent-b"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})

	_, agentAKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Agent A Key",
		Scope:       domain.APIKeyScopeWorkspaceSystem,
		WorkspaceID: owner.WorkspaceID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsMembersWrite,
		},
	})
	_, agentBKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Agent B Key",
		Scope:       domain.APIKeyScopeWorkspaceSystem,
		WorkspaceID: owner.WorkspaceID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsMembersWrite,
		},
	})

	channel := createConversationViaHTTP(t, httpClient, baseURL, agentAKey, domain.CreateConversationParams{
		Name:      uniqueName("session"),
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agentA.ID,
	})

	inviteUsersViaHTTP(t, httpClient, baseURL, agentAKey, channel.ID, []string{agentB.ID})

	rootMessage := postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channel.ID,
		UserID:    agentA.ID,
		Text:      "Investigate deployment failure for api-service and report back.",
		Metadata: mustRawJSON(t, map[string]any{
			"session_type": "agent_coordination",
			"task_id":      uniqueName("task"),
		}),
	})

	replyMessage := postMessageViaHTTP(t, httpClient, baseURL, agentBKey, domain.PostMessageParams{
		ChannelID: channel.ID,
		UserID:    agentB.ID,
		ThreadTS:  rootMessage.TS,
		Text:      "Acknowledged. Gathering logs and deployment metadata now.",
	})
	if replyMessage.ThreadTS == nil || *replyMessage.ThreadTS != rootMessage.TS {
		t.Fatalf("reply thread_ts = %v, want %q", replyMessage.ThreadTS, rootMessage.TS)
	}

	addReactionViaHTTP(t, httpClient, baseURL, agentBKey, channel.ID, rootMessage.TS, "eyes", agentB.ID)

	memberIDs := listMembersViaHTTP(t, httpClient, baseURL, agentAKey, channel.ID)
	if !contains(memberIDs, agentA.ID) || !contains(memberIDs, agentB.ID) {
		t.Fatalf("members = %v, want both %s and %s", memberIDs, agentA.ID, agentB.ID)
	}

	threadMessages := listThreadMessagesViaHTTP(t, httpClient, baseURL, agentAKey, channel.ID, rootMessage.TS)
	if len(threadMessages) != 2 {
		t.Fatalf("thread message count = %d, want 2", len(threadMessages))
	}

	eventTypes := queryWorkspaceEventTypes(t, ctx, pool, owner.WorkspaceID)
	wantEvents := []string{
		domain.EventUserCreated,
		domain.EventUserCreated,
		domain.EventUserCreated,
		domain.EventAPIKeyCreated,
		domain.EventAPIKeyCreated,
		domain.EventConversationCreated,
		domain.EventMemberJoined,
		domain.EventMessagePosted,
		domain.EventMessagePosted,
		domain.EventReactionAdded,
	}
	if len(eventTypes) != len(wantEvents) {
		t.Fatalf("event count = %d, want %d: %v", len(eventTypes), len(wantEvents), eventTypes)
	}
	for i, want := range wantEvents {
		if eventTypes[i] != want {
			t.Fatalf("event[%d] = %q, want %q (full sequence: %v)", i, eventTypes[i], want, eventTypes)
		}
	}
}

func e2eBaseURL() string {
	if v := os.Getenv("TERASLACK_E2E_BASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:38080"
}

func bootstrapOwnerUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	userRepo := pgRepo.NewUserRepo(pool)
	eventStoreRepo := pgRepo.NewEventStoreRepo(pool)
	recorder := service.NewEventRecorder(eventStoreRepo)
	userSvc := service.NewUserService(userRepo, recorder, pool, logger)

	workspaceID := uniqueName("T-agent-e2e")
	owner, err := userSvc.Create(ctx, domain.CreateUserParams{
		WorkspaceID:   workspaceID,
		Name:          uniqueName("owner"),
		Email:         uniqueEmail("owner"),
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeAdmin,
	})
	if err != nil {
		t.Fatalf("bootstrap owner user: %v", err)
	}
	return owner
}

func createSessionToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, userID string) string {
	t.Helper()
	session, err := pgRepo.NewAuthRepo(pool).CreateSession(ctx, domain.CreateAuthSessionParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Provider:    domain.AuthProviderGitHub,
		ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create session token: %v", err)
	}
	return session.Token
}

func createUserViaHTTP(t *testing.T, httpClient *http.Client, baseURL, token string, params domain.CreateUserParams) domain.User {
	t.Helper()
	var resp domain.User
	doJSON(t, httpClient, http.MethodPost, baseURL+"/workspaces/"+params.WorkspaceID+"/users", token, params, &resp)
	if resp.ID == "" {
		t.Fatalf("create user response = %+v", resp)
	}
	return resp
}

func createAPIKeyViaHTTP(t *testing.T, httpClient *http.Client, baseURL, token string, params domain.CreateAPIKeyParams) (domain.APIKey, string) {
	t.Helper()
	var resp struct {
		APIKey domain.APIKey `json:"api_key"`
		Secret string        `json:"secret"`
	}
	doJSON(t, httpClient, http.MethodPost, baseURL+"/api-keys", token, params, &resp)
	if resp.APIKey.ID == "" || resp.Secret == "" {
		t.Fatalf("create api key response = %+v", resp)
	}
	return resp.APIKey, resp.Secret
}

func createConversationViaHTTP(t *testing.T, httpClient *http.Client, baseURL, apiKey string, params domain.CreateConversationParams) domain.Conversation {
	t.Helper()
	var resp domain.Conversation
	doJSON(t, httpClient, http.MethodPost, baseURL+"/conversations", apiKey, params, &resp)
	if resp.ID == "" {
		t.Fatalf("create conversation response = %+v", resp)
	}
	return resp
}

func inviteUsersViaHTTP(t *testing.T, httpClient *http.Client, baseURL, apiKey, channelID string, userIDs []string) {
	t.Helper()
	var resp domain.Conversation
	doJSON(t, httpClient, http.MethodPost, baseURL+"/conversations/"+channelID+"/members", apiKey, map[string]any{
		"user_ids": userIDs,
	}, &resp)
	if resp.NumMembers < len(userIDs)+1 {
		t.Fatalf("invite response = %+v", resp)
	}
}

func postMessageViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth string, params domain.PostMessageParams) domain.Message {
	t.Helper()
	var resp domain.Message
	doJSON(t, httpClient, http.MethodPost, baseURL+"/messages", auth, params, &resp)
	if resp.TS == "" {
		t.Fatalf("post message response = %+v", resp)
	}
	return resp
}

func addReactionViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth, channelID, ts, emoji, userID string) {
	t.Helper()
	doJSON(t, httpClient, http.MethodPost, baseURL+"/messages/"+channelID+"/"+ts+"/reactions", auth, map[string]any{
		"name": emoji,
	}, nil)
}

func listMembersViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth, channelID string) []string {
	t.Helper()
	var resp struct {
		Items []string `json:"items"`
	}
	doJSON(t, httpClient, http.MethodGet, baseURL+"/conversations/"+channelID+"/members", auth, nil, &resp)
	return resp.Items
}

func listThreadMessagesViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth, channelID, threadTS string) []domain.Message {
	t.Helper()
	var resp struct {
		Items []domain.Message `json:"items"`
	}
	url := fmt.Sprintf("%s/messages?conversation_id=%s&thread_ts=%s", baseURL, channelID, threadTS)
	doJSON(t, httpClient, http.MethodGet, url, auth, nil, &resp)
	return resp.Items
}

func queryWorkspaceEventTypes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID string) []string {
	t.Helper()

	rows, err := pool.Query(ctx, "SELECT event_type FROM internal_events WHERE workspace_id = $1 ORDER BY id ASC", workspaceID)
	if err != nil {
		t.Fatalf("query internal_events: %v", err)
	}
	defer rows.Close()

	var eventTypes []string
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			t.Fatalf("scan event_type: %v", err)
		}
		eventTypes = append(eventTypes, et)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate event_types: %v", err)
	}
	return eventTypes
}

func doJSON(t *testing.T, httpClient *http.Client, method, url, auth string, body any, out any) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s %s: status=%d body=%s", method, url, resp.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal response %s: %v\nbody=%s", url, err, data)
	}
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return data
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}
