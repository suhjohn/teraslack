package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestDecodeStrictJSON_RejectsLegacyEventTypesField(t *testing.T) {
	req := httptest.NewRequest("POST", "/event-subscriptions", strings.NewReader(`{
		"url":"https://example.com",
		"event_types":["message.posted"],
		"secret":"secret"
	}`))

	var params domain.CreateEventSubscriptionParams
	if err := decodeStrictJSON(req, &params); err == nil {
		t.Fatal("expected invalid JSON for legacy event_types field")
	}
}

func TestEventSubscriptionResponseFromDomain_RedactsSecrets(t *testing.T) {
	payload, err := json.Marshal(eventSubscriptionResponseFromDomain(&domain.EventSubscription{
		ID:              "ES123",
		TeamID:          "T123",
		URL:             "https://example.com/webhook",
		Type:            domain.EventTypeConversationMessageCreated,
		ResourceType:    domain.ResourceTypeConversation,
		ResourceID:      "C123",
		Secret:          "plaintext-secret",
		EncryptedSecret: "ciphertext-secret",
		Enabled:         true,
	}))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := decoded["secret"]; ok {
		t.Fatal("response leaked plaintext secret")
	}
	if _, ok := decoded["encrypted_secret"]; ok {
		t.Fatal("response leaked encrypted secret")
	}
	if decoded["id"] != "ES123" || decoded["url"] != "https://example.com/webhook" {
		t.Fatalf("unexpected response payload: %+v", decoded)
	}
}
