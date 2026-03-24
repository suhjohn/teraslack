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
	if got := sendResp["channel_id"]; got != conversationID {
		t.Fatalf("send_message channel_id = %v, want %s", got, conversationID)
	}

	listResp := callToolJSON(t, srv, "list_messages", map[string]any{
		"channel_id": conversationID,
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

type mockMCPBackend struct {
	mu sync.Mutex

	bootstrapToken string
	teamID         string
	testAgentID    string

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
		bootstrapToken: "bootstrap-token",
		teamID:         "T_TEST",
		userSeq:        3,
		keySeq:         1,
		convSeq:        1,
		msgSeq:         1,
		eventSeq:       1,
		usersByID:      map[string]domain.User{},
		tokenUsers:     map[string]string{},
		conversations:  map[string]domain.Conversation{},
		messages:       map[string][]domain.Message{},
	}

	bootstrap := domain.User{
		ID:            "U_BOOT",
		TeamID:        backend.teamID,
		Name:          "bootstrap",
		Email:         "bootstrap@example.com",
		PrincipalType: domain.PrincipalTypeSystem,
		IsBot:         true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	testAgent := domain.User{
		ID:            "U_TEST",
		TeamID:        backend.teamID,
		Name:          "test-agent",
		Email:         "test-agent@example.com",
		PrincipalType: domain.PrincipalTypeAgent,
		IsBot:         true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	backend.testAgentID = testAgent.ID
	backend.usersByID[bootstrap.ID] = bootstrap
	backend.usersByID[testAgent.ID] = testAgent
	backend.userOrder = []string{bootstrap.ID, testAgent.ID}
	backend.tokenUsers[backend.bootstrapToken] = bootstrap.ID
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
		TeamID:        user.TeamID,
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
		TeamID:        b.teamID,
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
	secret := fmt.Sprintf("sk_live_test_%03d", b.keySeq)
	b.keySeq++
	b.tokenUsers[secret] = params.PrincipalID

	key := domain.APIKey{
		ID:          id,
		TeamID:      b.teamID,
		PrincipalID: params.PrincipalID,
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
		ID:         id,
		TeamID:     b.teamID,
		Type:       params.Type,
		CreatorID:  user.ID,
		NumMembers: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
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
	msg := domain.Message{
		TS:        fmt.Sprintf("1000.%03d", b.msgSeq),
		ChannelID: params.ChannelID,
		UserID:    params.UserID,
		Text:      params.Text,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	b.msgSeq++
	b.messages[params.ChannelID] = append([]domain.Message{msg}, b.messages[params.ChannelID]...)
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
	event.TeamID = b.teamID
	event.OccurredAt = time.Now().UTC()
	event.CreatedAt = event.OccurredAt
	b.eventSeq++
	b.events = append(b.events, event)
}

func (b *mockMCPBackend) createConversationForTest(actorID, targetID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := fmt.Sprintf("D_%03d", b.convSeq)
	b.convSeq++
	b.conversations[id] = domain.Conversation{
		ID:         id,
		TeamID:     b.teamID,
		Type:       domain.ConversationTypeIM,
		CreatorID:  actorID,
		NumMembers: 2,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	return id
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
