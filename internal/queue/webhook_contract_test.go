package queue

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	internalcrypto "github.com/suhjohn/teraslack/internal/crypto"
	"github.com/suhjohn/teraslack/internal/domain"
)

func TestMarshalWebhookEnvelope_UsesFullExternalEvent(t *testing.T) {
	createdAt := time.Date(2026, 3, 21, 19, 20, 30, 0, time.UTC)
	internalID := int64(77)
	evt := domain.ExternalEvent{
		ID:                     123,
		TeamID:                 "T123",
		Type:                   domain.EventTypeConversationMessageCreated,
		ResourceType:           domain.ResourceTypeConversation,
		ResourceID:             "C123",
		OccurredAt:             createdAt,
		Payload:                mustRawJSON(t, map[string]any{"ts": "1742578123.001200", "text": "hello"}),
		SourceInternalEventID:  &internalID,
		SourceInternalEventIDs: []int64{77},
		DedupeKey:              "internal:77:0",
		CreatedAt:              createdAt,
	}

	body, err := marshalWebhookEnvelope(evt)
	if err != nil {
		t.Fatalf("marshalWebhookEnvelope: %v", err)
	}

	var got domain.ExternalEvent
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if got.ID != evt.ID || got.Type != evt.Type || got.ResourceType != evt.ResourceType ||
		got.ResourceID != evt.ResourceID || got.TeamID != evt.TeamID {
		t.Fatalf("unexpected envelope: %+v", got)
	}
	if !got.OccurredAt.Equal(createdAt) {
		t.Fatalf("OccurredAt = %s, want %s", got.OccurredAt, createdAt)
	}
	assertJSONEqual(t, got.Payload, evt.Payload)
}

func TestWebhookWorkerDeliverWebhook_SendsEnvelopeAndHeaders(t *testing.T) {
	timestampNow := time.Date(2026, 3, 21, 19, 20, 30, 0, time.UTC)
	evt := domain.ExternalEvent{
		ID:           456,
		TeamID:       "T999",
		Type:         domain.EventTypeConversationMessageUpdated,
		ResourceType: domain.ResourceTypeConversation,
		ResourceID:   "C999",
		Payload:      mustRawJSON(t, map[string]any{"ts": "1742578123.001200", "text": "updated"}),
		OccurredAt:   timestampNow,
		CreatedAt:    timestampNow,
	}
	body, err := marshalWebhookEnvelope(evt)
	if err != nil {
		t.Fatalf("marshalWebhookEnvelope: %v", err)
	}

	var gotBody []byte
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var readErr error
		gotBody, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatalf("read body: %v", readErr)
		}
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	worker := &WebhookWorker{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		encryptor:  testEncryptor(t),
	}

	secret, err := worker.encryptor.Encrypt("topsecret")
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}

	job := Job{
		ID:             "wh-456-ES123",
		EventID:        evt.ID,
		TeamID:         evt.TeamID,
		EventType:      evt.Type,
		SubscriptionID: "ES123",
		URL:            srv.URL,
		Secret:         secret,
		Payload:        body,
	}

	if errMsg := worker.deliverWebhook(context.Background(), job); errMsg != "" {
		t.Fatalf("deliverWebhook error = %q", errMsg)
	}

	assertJSONEqual(t, gotBody, body)
	if gotHeaders.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", gotHeaders.Get("Content-Type"))
	}
	if gotHeaders.Get("X-Teraslack-Delivery-Id") != job.ID {
		t.Fatalf("X-Teraslack-Delivery-Id = %q, want %q", gotHeaders.Get("X-Teraslack-Delivery-Id"), job.ID)
	}
	if gotHeaders.Get("X-Teraslack-Event-Id") != "456" {
		t.Fatalf("X-Teraslack-Event-Id = %q", gotHeaders.Get("X-Teraslack-Event-Id"))
	}
	if gotHeaders.Get("X-Teraslack-Event-Type") != job.EventType {
		t.Fatalf("X-Teraslack-Event-Type = %q, want %q", gotHeaders.Get("X-Teraslack-Event-Type"), job.EventType)
	}
	if gotHeaders.Get("X-Teraslack-Team-Id") != job.TeamID {
		t.Fatalf("X-Teraslack-Team-Id = %q, want %q", gotHeaders.Get("X-Teraslack-Team-Id"), job.TeamID)
	}

	ts := gotHeaders.Get("X-Teraslack-Request-Timestamp")
	if ts == "" {
		t.Fatal("missing X-Teraslack-Request-Timestamp")
	}
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	wantSig := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if gotHeaders.Get("X-Teraslack-Signature") != wantSig {
		t.Fatalf("X-Teraslack-Signature = %q, want %q", gotHeaders.Get("X-Teraslack-Signature"), wantSig)
	}
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()

	var gotV any
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("unmarshal got json: %v", err)
	}

	var wantV any
	if err := json.Unmarshal(want, &wantV); err != nil {
		t.Fatalf("unmarshal want json: %v", err)
	}

	gotNorm, err := json.Marshal(gotV)
	if err != nil {
		t.Fatalf("marshal got normalized json: %v", err)
	}
	wantNorm, err := json.Marshal(wantV)
	if err != nil {
		t.Fatalf("marshal want normalized json: %v", err)
	}

	if string(gotNorm) != string(wantNorm) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", gotNorm, wantNorm)
	}
}

func testEncryptor(t *testing.T) *internalcrypto.Encryptor {
	t.Helper()

	provider, err := internalcrypto.NewEnvKeyProvider("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	if err != nil {
		t.Fatalf("new key provider: %v", err)
	}
	return internalcrypto.NewEncryptor(provider)
}
