package teraslackmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/suhjohn/teraslack/internal/domain"
)

func TestServer_RegisterAndCollaborate(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:        serverURL,
		BootstrapToken: backend.bootstrapToken,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	registerResp := callToolJSON(t, srv, "register", map[string]any{
		"name": "deploy-agent",
	})
	user := nestedMap(t, registerResp, "user")
	if got := user["name"]; got != "deploy-agent" {
		t.Fatalf("register user name = %v, want deploy-agent", got)
	}

	searchResp := callToolJSON(t, srv, "search_users", map[string]any{
		"query": "test-agent",
		"exact": true,
	})
	users := nestedSlice(t, searchResp, "users")
	if len(users) != 1 {
		t.Fatalf("search_users count = %d, want 1", len(users))
	}
	foundUser := users[0].(map[string]any)
	if got := foundUser["id"]; got != backend.testAgentID {
		t.Fatalf("search_users id = %v, want %s", got, backend.testAgentID)
	}

	dmResp := callToolJSON(t, srv, "create_dm", map[string]any{
		"user_id": backend.testAgentID,
	})
	conversationID, _ := dmResp["conversation_id"].(string)
	if conversationID == "" {
		t.Fatalf("create_dm conversation_id missing: %+v", dmResp)
	}

	sendResp := callToolJSON(t, srv, "send_message", map[string]any{
		"text": "Deploy to staging is done.",
	})
	if got := sendResp["conversation_id"]; got != conversationID {
		t.Fatalf("send_message conversation_id = %v, want %s", got, conversationID)
	}

	listResp := callToolJSON(t, srv, "list_messages", map[string]any{
		"conversation_id": conversationID,
	})
	messages := nestedSlice(t, listResp, "messages")
	if len(messages) != 1 {
		t.Fatalf("list_messages count = %d, want 1", len(messages))
	}
	message := messages[0].(map[string]any)
	if got := message["text"]; got != "Deploy to staging is done." {
		t.Fatalf("message text = %v", got)
	}
}

func TestServer_WaitForEventAndAPIRequest(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:        serverURL,
		BootstrapToken: backend.bootstrapToken,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	callToolJSON(t, srv, "register", map[string]any{
		"name": "deploy-agent",
	})

	conversationID := backend.createConversationForTest("U_TESTER", "U_TEST")
	go func() {
		time.Sleep(50 * time.Millisecond)
		backend.appendEvent(domain.ExternalEvent{
			Type:         domain.EventTypeConversationMemberAdded,
			ResourceType: domain.ResourceTypeConversation,
			ResourceID:   conversationID,
			Payload: mustJSON(t, map[string]any{
				"conversation_id": conversationID,
				"user_id":         "U_TEST",
				"actor_id":        "U_TESTER",
			}),
		})
	}()

	waitResp := callToolJSON(t, srv, "wait_for_event", map[string]any{
		"type":             domain.EventTypeConversationMemberAdded,
		"resource_type":    domain.ResourceTypeConversation,
		"resource_id":      conversationID,
		"timeout_seconds":  2,
		"poll_interval_ms": 10,
	})
	event := nestedMap(t, waitResp, "event")
	if got := event["resource_id"]; got != conversationID {
		t.Fatalf("wait_for_event resource_id = %v, want %s", got, conversationID)
	}

	apiResp := callToolJSON(t, srv, "api_request", map[string]any{
		"method":     "GET",
		"path":       "/users",
		"auth_scope": "bootstrap",
		"query": map[string]any{
			"limit": 10,
		},
	})
	result := nestedMap(t, apiResp, "result")
	items := nestedSlice(t, result, "items")
	if len(items) < 2 {
		t.Fatalf("api_request users count = %d, want at least 2", len(items))
	}
}

func TestServer_SubscribeConversationAndReadNextEvent(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:        serverURL,
		BootstrapToken: backend.bootstrapToken,
		PeerUserID:     backend.testAgentID,
		PeerUserName:   "test-agent",
		PeerUserEmail:  "test-agent@example.com",
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	callToolJSON(t, srv, "register", map[string]any{
		"name": "deploy-agent",
	})
	dmResp := callToolJSON(t, srv, "create_dm", map[string]any{
		"user_id": backend.testAgentID,
	})
	channelID, _ := dmResp["conversation_id"].(string)
	if channelID == "" {
		t.Fatalf("create_dm conversation_id missing: %+v", dmResp)
	}

	backend.appendMessageEvent(channelID, backend.testAgentID, "old status")

	subscribeResp := callToolJSON(t, srv, "subscribe_conversation", map[string]any{
		"conversation_id": channelID,
	})
	subscriptionID, _ := subscribeResp["subscription_id"].(string)
	if subscriptionID == "" {
		t.Fatalf("subscribe_conversation subscription_id missing: %+v", subscribeResp)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		backend.appendMessageEvent(channelID, backend.testAgentID, "new status")
	}()

	nextResp := callToolJSON(t, srv, "next_event", map[string]any{
		"subscription_id":  subscriptionID,
		"event_type":       domain.EventTypeConversationMessageCreated,
		"from_user_id":     backend.testAgentID,
		"contains_text":    "new",
		"timeout_seconds":  2,
		"poll_interval_ms": 10,
	})
	event := nestedMap(t, nextResp, "event")
	if got := event["type"]; got != domain.EventTypeConversationMessageCreated {
		t.Fatalf("next_event type = %v, want %s", got, domain.EventTypeConversationMessageCreated)
	}
	message := nestedMap(t, nextResp, "message")
	if got := message["text"]; got != "new status" {
		t.Fatalf("next_event message text = %v, want new status", got)
	}
	if got := message["sender_email"]; got != "test-agent@example.com" {
		t.Fatalf("next_event sender_email = %v, want test-agent@example.com", got)
	}
}

func TestServer_WaitForMessageIgnoresExistingHistoryByDefault(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:        serverURL,
		BootstrapToken: backend.bootstrapToken,
		PeerUserID:     backend.testAgentID,
		PeerUserName:   "test-agent",
		PeerUserEmail:  "test-agent@example.com",
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	callToolJSON(t, srv, "register", map[string]any{
		"name": "deploy-agent",
	})
	dmResp := callToolJSON(t, srv, "create_dm", map[string]any{
		"user_id": backend.testAgentID,
	})
	channelID, _ := dmResp["conversation_id"].(string)
	if channelID == "" {
		t.Fatalf("create_dm conversation_id missing: %+v", dmResp)
	}

	backend.appendMessage(channelID, backend.testAgentID, "status: old")

	go func() {
		time.Sleep(50 * time.Millisecond)
		backend.appendMessage(channelID, backend.testAgentID, "status: new")
	}()

	waitResp := callToolJSON(t, srv, "wait_for_message", map[string]any{
		"conversation_id":  channelID,
		"contains_text":    "status:",
		"from_user_id":     backend.testAgentID,
		"timeout_seconds":  2,
		"poll_interval_ms": 10,
	})
	message := nestedMap(t, waitResp, "message")
	if got := message["text"]; got != "status: new" {
		t.Fatalf("wait_for_message text = %v, want status: new", got)
	}
}

func TestServer_PollChannelEventsOnceCoversAllVisibleConversations(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	client, err := NewClient(serverURL, backend.oauthHumanToken)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	srv, err := NewServer(Config{
		BaseURL: serverURL,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	conversationA := backend.createConversationForTest(backend.humanUserID, backend.testAgentID)
	conversationB := backend.createConversationForTest(backend.humanUserID, "U_OTHER")
	backend.appendMessageEvent(conversationA, backend.testAgentID, "from A")
	backend.appendMessageEvent(conversationB, "U_OTHER", "from B")
	backend.appendMessageEvent(conversationA, backend.humanUserID, "from self")

	var pushed []channelEvent
	next, err := srv.pollChannelEventsOnce(context.Background(), client, backend.humanUserID, "", func(event channelEvent) {
		pushed = append(pushed, event)
	})
	if err != nil {
		t.Fatalf("pollChannelEventsOnce() error = %v", err)
	}
	if next == "" {
		t.Fatalf("pollChannelEventsOnce() next cursor is empty")
	}
	if len(pushed) != 2 {
		t.Fatalf("pollChannelEventsOnce() pushed count = %d, want 2", len(pushed))
	}
	if pushed[0].Meta["conversation_id"] != conversationA {
		t.Fatalf("first pushed conversation_id = %v, want %s", pushed[0].Meta["conversation_id"], conversationA)
	}
	if pushed[1].Meta["conversation_id"] != conversationB {
		t.Fatalf("second pushed conversation_id = %v, want %s", pushed[1].Meta["conversation_id"], conversationB)
	}
}

func TestServer_StreamableHTTPToolsAndSessionState(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:        serverURL,
		BootstrapToken: backend.bootstrapToken,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	httpServer := httptest.NewServer(srv.HTTPHandler())
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "teraslack-mcp-test-client",
		Version: "v0.0.1",
	}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
	}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("ListTools() returned no tools")
	}

	foundRegister := false
	for _, tool := range tools.Tools {
		if tool.Name == "register" {
			foundRegister = true
			break
		}
	}
	if !foundRegister {
		t.Fatalf("register tool not found in %#v", tools.Tools)
	}

	registerResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "register",
		Arguments: map[string]any{
			"name": "deploy-agent",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(register) error = %v", err)
	}
	if registerResult.IsError {
		t.Fatalf("CallTool(register) returned tool error: %+v", registerResult)
	}

	whoamiResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "whoami",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(whoami) error = %v", err)
	}
	if whoamiResult.IsError {
		t.Fatalf("CallTool(whoami) returned tool error: %+v", whoamiResult)
	}

	var structured map[string]any
	switch value := whoamiResult.StructuredContent.(type) {
	case map[string]any:
		structured = value
	default:
		t.Fatalf("unexpected structured content type %T", whoamiResult.StructuredContent)
	}

	user := nestedMap(t, structured, "user")
	if got := user["name"]; got != "deploy-agent" {
		t.Fatalf("whoami user name = %v, want deploy-agent", got)
	}
}

func TestServer_StreamableHTTPAutoProvisionsDistinctSessionAgents(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:     serverURL,
		APIKey:      backend.oauthHumanToken,
		WorkspaceID: backend.workspaceID,
		UserID:      backend.humanUserID,
		OAuthScopes: []string{domain.MCPOAuthScopeTools},
		Permissions: []string{domain.PermissionMessagesWrite},
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	httpServer := httptest.NewServer(srv.HTTPHandler())
	defer httpServer.Close()

	clientA := mcp.NewClient(&mcp.Implementation{Name: "client-a", Version: "v0.0.1"}, nil)
	sessionA, err := clientA.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("clientA connect: %v", err)
	}
	defer sessionA.Close()

	clientB := mcp.NewClient(&mcp.Implementation{Name: "client-b", Version: "v0.0.1"}, nil)
	sessionB, err := clientB.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("clientB connect: %v", err)
	}
	defer sessionB.Close()

	whoamiA := callSessionToolJSON(t, sessionA, "whoami", map[string]any{"session_id": "claude-a"})
	whoamiB := callSessionToolJSON(t, sessionB, "whoami", map[string]any{"session_id": "claude-b"})

	userA := nestedMap(t, whoamiA, "user")
	userB := nestedMap(t, whoamiB, "user")
	if userA["id"] == userB["id"] {
		t.Fatalf("session agents should differ: A=%v B=%v", userA, userB)
	}
	if got := whoamiA["identity_mode"]; got != "session" {
		t.Fatalf("whoamiA identity_mode = %v, want session", got)
	}
	if got := whoamiB["identity_mode"]; got != "session" {
		t.Fatalf("whoamiB identity_mode = %v, want session", got)
	}
	if got := whoamiA["bootstrap_available"]; got != false {
		t.Fatalf("whoamiA bootstrap_available = %v, want false", got)
	}
	ownerA := nestedMap(t, whoamiA, "owner")
	if got := ownerA["id"]; got != backend.humanUserID {
		t.Fatalf("ownerA id = %v, want %s", got, backend.humanUserID)
	}
}

func TestServer_StreamableHTTPOAuthRegisterCreatesOwnedAgent(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:     serverURL,
		APIKey:      backend.oauthHumanToken,
		WorkspaceID: backend.workspaceID,
		UserID:      backend.humanUserID,
		OAuthScopes: []string{domain.MCPOAuthScopeTools},
		Permissions: []string{domain.PermissionMessagesWrite},
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	httpServer := httptest.NewServer(srv.HTTPHandler())
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "client-a", Version: "v0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	registerResp := callSessionToolJSON(t, session, "register", map[string]any{
		"name": "agent-a",
	})
	user := nestedMap(t, registerResp, "user")
	if got := user["name"]; got != "agent-a" {
		t.Fatalf("register user name = %v, want agent-a", got)
	}
	if got := user["principal_type"]; got != string(domain.PrincipalTypeAgent) {
		t.Fatalf("register principal_type = %v, want %s", got, domain.PrincipalTypeAgent)
	}

	whoamiResp := callSessionToolJSON(t, session, "whoami", map[string]any{})
	current := nestedMap(t, whoamiResp, "user")
	if got := current["id"]; got != user["id"] {
		t.Fatalf("whoami user id = %v, want %v", got, user["id"])
	}
	if got := whoamiResp["identity_mode"]; got != "switched" {
		t.Fatalf("whoami identity_mode = %v, want switched", got)
	}

	identitiesResp := callSessionToolJSON(t, session, "list_owned_identities", map[string]any{})
	identities := nestedSlice(t, identitiesResp, "identities")
	found := false
	for _, raw := range identities {
		identity, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if identity["id"] == user["id"] {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("registered agent %v not found in owned identities: %+v", user["id"], identities)
	}
}

func TestServer_StreamableHTTPSwitchIdentityAndReset(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL:     serverURL,
		APIKey:      backend.oauthHumanToken,
		WorkspaceID: backend.workspaceID,
		UserID:      backend.humanUserID,
		OAuthScopes: []string{domain.MCPOAuthScopeTools},
		Permissions: []string{domain.PermissionMessagesWrite},
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	httpServer := httptest.NewServer(srv.HTTPHandler())
	defer httpServer.Close()

	clientA := mcp.NewClient(&mcp.Implementation{Name: "client-a", Version: "v0.0.1"}, nil)
	sessionA, err := clientA.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("clientA connect: %v", err)
	}
	defer sessionA.Close()

	clientB := mcp.NewClient(&mcp.Implementation{Name: "client-b", Version: "v0.0.1"}, nil)
	sessionB, err := clientB.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("clientB connect: %v", err)
	}
	defer sessionB.Close()

	whoamiA := callSessionToolJSON(t, sessionA, "whoami", map[string]any{"session_id": "claude-a"})
	whoamiB := callSessionToolJSON(t, sessionB, "whoami", map[string]any{"session_id": "claude-b"})
	userA := nestedMap(t, whoamiA, "user")
	userB := nestedMap(t, whoamiB, "user")
	if userA["id"] == userB["id"] {
		t.Fatalf("expected distinct initial session users: A=%v B=%v", userA, userB)
	}

	switchResp := callSessionToolJSON(t, sessionB, "switch_identity", map[string]any{
		"user_id": userA["id"],
	})
	if got := switchResp["status"]; got != "switched" {
		t.Fatalf("switch_identity status = %v, want switched", got)
	}
	whoamiAfterSwitch := callSessionToolJSON(t, sessionB, "whoami", map[string]any{"session_id": "claude-b"})
	switchedUser := nestedMap(t, whoamiAfterSwitch, "user")
	if switchedUser["id"] != userA["id"] {
		t.Fatalf("switched user id = %v, want %v", switchedUser["id"], userA["id"])
	}
	if got := whoamiAfterSwitch["identity_mode"]; got != "switched" {
		t.Fatalf("identity_mode after switch = %v, want switched", got)
	}

	listResp := callSessionToolJSON(t, sessionB, "list_owned_identities", map[string]any{})
	identities := nestedSlice(t, listResp, "identities")
	if len(identities) < 2 {
		t.Fatalf("owned identities count = %d, want at least 2", len(identities))
	}

	resetResp := callSessionToolJSON(t, sessionB, "reset_identity", map[string]any{})
	if got := resetResp["status"]; got != "reset" {
		t.Fatalf("reset_identity status = %v, want reset", got)
	}
	whoamiAfterReset := callSessionToolJSON(t, sessionB, "whoami", map[string]any{"session_id": "claude-b"})
	resetUser := nestedMap(t, whoamiAfterReset, "user")
	if resetUser["id"] != userB["id"] {
		t.Fatalf("reset user id = %v, want %v", resetUser["id"], userB["id"])
	}
	if got := whoamiAfterReset["identity_mode"]; got != "session" {
		t.Fatalf("identity_mode after reset = %v, want session", got)
	}
}

func TestServer_AllowsCredentiallessStartup(t *testing.T) {
	backend := newMockMCPBackend()
	serverURL := httptest.NewServer(backend).URL
	t.Cleanup(func() { backend.close() })

	srv, err := NewServer(Config{
		BaseURL: serverURL,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	whoamiResp := callToolJSON(t, srv, "whoami", map[string]any{})
	if got := whoamiResp["registered"]; got != false {
		t.Fatalf("whoami registered = %v, want false", got)
	}
	if got := whoamiResp["bootstrap_available"]; got != false {
		t.Fatalf("whoami bootstrap_available = %v, want false", got)
	}
}

type mockMCPBackend struct {
	mu sync.Mutex

	bootstrapToken  string
	oauthHumanToken string
	workspaceID     string
	testAgentID     string
	humanUserID     string

	userSeq  int
	keySeq   int
	convSeq  int
	msgSeq   int
	eventSeq int64

	usersByID  map[string]domain.User
	userOrder  []string
	tokenUsers map[string]string

	conversations map[string]domain.Conversation
	messages      map[string][]domain.Message
	events        []domain.ExternalEvent
}

func newMockMCPBackend() *mockMCPBackend {
	now := time.Now().UTC()
	backend := &mockMCPBackend{
		bootstrapToken:  "bootstrap-token",
		oauthHumanToken: "oauth-human-token",
		workspaceID:     "T_TEST",
		userSeq:         3,
		keySeq:          1,
		convSeq:         1,
		msgSeq:          1,
		eventSeq:        1,
		usersByID:       map[string]domain.User{},
		tokenUsers:      map[string]string{},
		conversations:   map[string]domain.Conversation{},
		messages:        map[string][]domain.Message{},
	}

	bootstrap := domain.User{
		ID:            "U_BOOT",
		WorkspaceID:   backend.workspaceID,
		Name:          "bootstrap",
		Email:         "bootstrap@example.com",
		PrincipalType: domain.PrincipalTypeSystem,
		IsBot:         true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	human := domain.User{
		ID:            "U_HUMAN",
		WorkspaceID:   backend.workspaceID,
		Name:          "johnsuh94",
		Email:         "johnsuh94@gmail.com",
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
		IsBot:         false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	testAgent := domain.User{
		ID:            "U_TEST",
		WorkspaceID:   backend.workspaceID,
		Name:          "test-agent",
		Email:         "test-agent@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		IsBot:         true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	backend.testAgentID = testAgent.ID
	backend.humanUserID = human.ID
	backend.usersByID[bootstrap.ID] = bootstrap
	backend.usersByID[human.ID] = human
	backend.usersByID[testAgent.ID] = testAgent
	backend.userOrder = []string{bootstrap.ID, human.ID, testAgent.ID}
	backend.tokenUsers[backend.bootstrapToken] = bootstrap.ID
	backend.tokenUsers[backend.oauthHumanToken] = human.ID
	return backend
}

func (b *mockMCPBackend) close() {}

func (b *mockMCPBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/auth/me":
		b.handleAuthMe(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/users":
		b.handleListUsers(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/users":
		b.handleCreateUser(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/"):
		b.handleGetUser(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api-keys":
		b.handleCreateAPIKey(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/conversations":
		b.handleCreateConversation(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/members"):
		b.handleInviteUsers(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/messages":
		b.handlePostMessage(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/messages":
		b.handleListMessages(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/events":
		b.handleListEvents(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *mockMCPBackend) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := b.mustAuthUser(w, r)
	if user == nil {
		return
	}
	writeJSON(w, http.StatusOK, domain.AuthContext{
		WorkspaceID:   user.WorkspaceID,
		UserID:        user.ID,
		PrincipalType: user.PrincipalType,
		IsBot:         user.IsBot,
	})
}

func (b *mockMCPBackend) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	items := make([]domain.User, 0, len(b.userOrder))
	start := 0
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if cursor != "" {
		for i, id := range b.userOrder {
			if id == cursor {
				start = i
				break
			}
		}
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	nextCursor := ""
	for i := start; i < len(b.userOrder) && len(items) < limit; i++ {
		items = append(items, b.usersByID[b.userOrder[i]])
	}
	if start+len(items) < len(b.userOrder) {
		nextCursor = b.userOrder[start+len(items)]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"next_cursor": nextCursor,
	})
}

func (b *mockMCPBackend) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	var params domain.CreateUserParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now().UTC()
	user := domain.User{
		ID:            fmt.Sprintf("U_%03d", b.userSeq),
		WorkspaceID:   b.workspaceID,
		Name:          params.Name,
		Email:         params.Email,
		OwnerID:       params.OwnerID,
		PrincipalType: params.PrincipalType,
		IsBot:         params.IsBot,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	b.userSeq++
	b.usersByID[user.ID] = user
	b.userOrder = append(b.userOrder, user.ID)
	writeJSON(w, http.StatusCreated, user)
}

func (b *mockMCPBackend) handleGetUser(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	userID := strings.TrimPrefix(r.URL.Path, "/users/")
	b.mu.Lock()
	defer b.mu.Unlock()
	user, ok := b.usersByID[userID]
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (b *mockMCPBackend) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	var params domain.CreateAPIKeyParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	id := fmt.Sprintf("AK_%03d", b.keySeq)
	secret := fmt.Sprintf("sk_%03d", b.keySeq)
	b.keySeq++
	b.tokenUsers[secret] = params.UserID

	key := domain.APIKey{
		ID:          id,
		WorkspaceID: b.workspaceID,
		UserID:      params.UserID,
		Permissions: params.Permissions,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"api_key": key,
		"secret":  secret,
	})
}

func (b *mockMCPBackend) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	user := b.mustAuthUser(w, r)
	if user == nil {
		return
	}

	var params domain.CreateConversationParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	id := fmt.Sprintf("D_%03d", b.convSeq)
	b.convSeq++
	now := time.Now().UTC()
	conv := domain.Conversation{
		ID:          id,
		WorkspaceID: b.workspaceID,
		Type:        params.Type,
		CreatorID:   user.ID,
		NumMembers:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	b.conversations[id] = conv
	writeJSON(w, http.StatusCreated, conv)
}

func (b *mockMCPBackend) handleInviteUsers(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/conversations/")
	channelID := strings.TrimSuffix(path, "/members")
	channelID = strings.TrimSuffix(channelID, "/")

	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	conv := b.conversations[channelID]
	conv.NumMembers += len(req.UserIDs)
	b.conversations[channelID] = conv
	writeJSON(w, http.StatusOK, conv)
}

func (b *mockMCPBackend) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	var params domain.PostMessageParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	msg := b.appendMessageLocked(params.ChannelID, params.UserID, params.Text)
	b.events = append(b.events, domain.ExternalEvent{
		ID:           b.eventSeq,
		WorkspaceID:  b.workspaceID,
		Type:         domain.EventTypeConversationMessageCreated,
		ResourceType: domain.ResourceTypeConversation,
		ResourceID:   params.ChannelID,
		OccurredAt:   msg.CreatedAt,
		CreatedAt:    msg.CreatedAt,
		Payload: mustMarshalJSON(map[string]any{
			"ts":         msg.TS,
			"channel_id": msg.ChannelID,
			"user_id":    msg.UserID,
			"text":       msg.Text,
		}),
	})
	b.eventSeq++
	writeJSON(w, http.StatusCreated, msg)
}

func (b *mockMCPBackend) handleListMessages(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	channelID := r.URL.Query().Get("conversation_id")
	b.mu.Lock()
	defer b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"items": b.messages[channelID],
	})
}

func (b *mockMCPBackend) handleListEvents(w http.ResponseWriter, r *http.Request) {
	if b.mustAuthUser(w, r) == nil {
		return
	}

	afterID := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			afterID = parsed
		}
	}
	eventType := r.URL.Query().Get("type")
	resourceType := r.URL.Query().Get("resource_type")
	resourceID := r.URL.Query().Get("resource_id")
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	filtered := make([]domain.ExternalEvent, 0, len(b.events))
	for _, event := range b.events {
		if event.ID <= afterID {
			continue
		}
		if eventType != "" && event.Type != eventType {
			continue
		}
		if resourceType != "" && event.ResourceType != resourceType {
			continue
		}
		if resourceID != "" && event.ResourceID != resourceID {
			continue
		}
		filtered = append(filtered, event)
	}

	hasMore := len(filtered) > limit
	if hasMore {
		filtered = filtered[:limit]
	}
	nextCursor := ""
	if len(filtered) > 0 {
		nextCursor = strconv.FormatInt(filtered[len(filtered)-1].ID, 10)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":       filtered,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
	})
}

func (b *mockMCPBackend) mustAuthUser(w http.ResponseWriter, r *http.Request) *domain.User {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	b.mu.Lock()
	defer b.mu.Unlock()
	userID, ok := b.tokenUsers[token]
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	user := b.usersByID[userID]
	return &user
}

func (b *mockMCPBackend) appendEvent(event domain.ExternalEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	event.ID = b.eventSeq
	event.WorkspaceID = b.workspaceID
	event.OccurredAt = time.Now().UTC()
	event.CreatedAt = event.OccurredAt
	b.eventSeq++
	b.events = append(b.events, event)
}

func (b *mockMCPBackend) appendMessage(channelID, userID, text string) domain.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.appendMessageLocked(channelID, userID, text)
}

func (b *mockMCPBackend) appendMessageEvent(channelID, userID, text string) domain.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	msg := b.appendMessageLocked(channelID, userID, text)
	b.events = append(b.events, domain.ExternalEvent{
		ID:           b.eventSeq,
		WorkspaceID:  b.workspaceID,
		Type:         domain.EventTypeConversationMessageCreated,
		ResourceType: domain.ResourceTypeConversation,
		ResourceID:   channelID,
		OccurredAt:   msg.CreatedAt,
		CreatedAt:    msg.CreatedAt,
		Payload: mustMarshalJSON(map[string]any{
			"ts":         msg.TS,
			"channel_id": msg.ChannelID,
			"user_id":    msg.UserID,
			"text":       msg.Text,
		}),
	})
	b.eventSeq++
	return msg
}

func (b *mockMCPBackend) createConversationForTest(actorID, targetID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := fmt.Sprintf("D_%03d", b.convSeq)
	b.convSeq++
	b.conversations[id] = domain.Conversation{
		ID:          id,
		WorkspaceID: b.workspaceID,
		Type:        domain.ConversationTypeIM,
		CreatorID:   actorID,
		NumMembers:  2,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	return id
}

func (b *mockMCPBackend) appendMessageLocked(channelID, userID, text string) domain.Message {
	msg := domain.Message{
		TS:        fmt.Sprintf("1000.%03d", b.msgSeq),
		ChannelID: channelID,
		UserID:    userID,
		Text:      text,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	b.msgSeq++
	b.messages[channelID] = append([]domain.Message{msg}, b.messages[channelID]...)
	return msg
}

func callToolJSON(t *testing.T, srv *Server, name string, args map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		t.Fatalf("marshal tool request: %v", err)
	}
	result, err := srv.callTool(context.Background(), raw)
	if err != nil {
		t.Fatalf("callTool(%s) error = %v", name, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode tool response: %v", err)
	}
	return decoded
}

func callSessionToolJSON(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%s) returned tool error: %+v", name, result)
	}

	value, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("CallTool(%s) structured content = %T, want map[string]any", name, result.StructuredContent)
	}
	return value
}

func nestedMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("key %q missing or not an object: %+v", key, parent)
	}
	return value
}

func nestedSlice(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("key %q missing or not an array: %+v", key, parent)
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}

func mustMarshalJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
