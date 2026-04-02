package teraslackstdio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestWhoAmIAndMessageFlow(t *testing.T) {
	backend := newMockBackend()
	httpServer := httptest.NewServer(backend)
	defer httpServer.Close()

	srv, err := NewServer(Config{
		BaseURL: httpServer.URL,
		APIKey:  "test-token",
	}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	whoamiText, err := srv.callTool(context.Background(), rawToolCall("whoami", nil))
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	var whoami map[string]any
	if err := json.Unmarshal([]byte(whoamiText), &whoami); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	user := whoami["user"].(map[string]any)
	if user["id"] != "U_ACTOR" {
		t.Fatalf("unexpected user id: %#v", user["id"])
	}

	createDMText, err := srv.callTool(context.Background(), rawToolCall("create_dm", map[string]any{
		"user_id": "U_PEER",
	}))
	if err != nil {
		t.Fatalf("create_dm: %v", err)
	}
	var createDM map[string]any
	if err := json.Unmarshal([]byte(createDMText), &createDM); err != nil {
		t.Fatalf("decode create_dm: %v", err)
	}
	if createDM["conversation_id"] != "D_TEST" {
		t.Fatalf("unexpected conversation id: %#v", createDM["conversation_id"])
	}

	sendText, err := srv.callTool(context.Background(), rawToolCall("send_message", map[string]any{
		"text": "hi",
	}))
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	var send map[string]any
	if err := json.Unmarshal([]byte(sendText), &send); err != nil {
		t.Fatalf("decode send_message: %v", err)
	}
	if send["status"] != "sent" {
		t.Fatalf("unexpected status: %#v", send["status"])
	}
	if len(backend.messages) != 1 || backend.messages[0].Text != "hi" {
		t.Fatalf("message not posted: %#v", backend.messages)
	}
}

func rawToolCall(name string, args map[string]any) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	return payload
}

type mockBackend struct {
	mu           sync.Mutex
	messages     []domain.Message
	conversation domain.Conversation
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		conversation: domain.Conversation{
			ID:   "D_TEST",
			Type: domain.ConversationTypeIM,
		},
	}
}

func (b *mockBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/auth/me":
		writeJSON(w, domain.AuthContext{
			WorkspaceID: "T_TEST",
			UserID:      "U_ACTOR",
			Permissions: []string{"*"},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/workspaces/T_TEST/users/U_ACTOR":
		writeJSON(w, domain.User{ID: "U_ACTOR", Name: "actor", Email: "actor@example.com"})
	case r.Method == http.MethodGet && r.URL.Path == "/workspaces/T_TEST/users/U_PEER":
		writeJSON(w, domain.User{ID: "U_PEER", Name: "peer", Email: "peer@example.com"})
	case r.Method == http.MethodPost && r.URL.Path == "/conversations":
		writeJSON(w, b.conversation)
	case r.Method == http.MethodPost && r.URL.Path == "/conversations/D_TEST/members":
		writeJSON(w, b.conversation)
	case r.Method == http.MethodPost && r.URL.Path == "/messages":
		var body struct {
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Text      string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		msg := domain.Message{
			TS:        "1000.1",
			ChannelID: body.ChannelID,
			UserID:    body.UserID,
			Text:      body.Text,
		}
		b.mu.Lock()
		b.messages = append(b.messages, msg)
		b.mu.Unlock()
		writeJSON(w, msg)
	case r.Method == http.MethodGet && r.URL.Path == "/messages":
		b.mu.Lock()
		items := append([]domain.Message(nil), b.messages...)
		b.mu.Unlock()
		writeJSON(w, map[string]any{"items": items})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/events"):
		writeJSON(w, map[string]any{"items": []any{}, "next_cursor": "", "has_more": false})
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
