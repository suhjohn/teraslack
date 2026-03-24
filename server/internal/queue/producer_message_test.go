package queue

import (
	"encoding/json"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
)

func TestIndexProducerEventToJob_MessageUpdatedUsesCanonicalResourceID(t *testing.T) {
	p := &IndexProducer{}
	payload := mustJSON(t, domain.Message{
		TS:        "123.456",
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "updated",
	})

	job := p.eventToJob(domain.InternalEvent{
		ID:            1,
		EventType:     domain.EventMessageUpdated,
		AggregateType: domain.AggregateMessage,
		AggregateID:   "123.456",
		TeamID:        "T123",
		Payload:       payload,
	})
	if job == nil {
		t.Fatal("expected job")
	}
	if job.TeamID != "T123" {
		t.Fatalf("TeamID = %q, want %q", job.TeamID, "T123")
	}
	if job.ResourceID != service.MessageSearchID("C123", "123.456") {
		t.Fatalf("ResourceID = %q, want %q", job.ResourceID, service.MessageSearchID("C123", "123.456"))
	}
}

func TestIndexProducerEventToJob_MessageDeletedUsesCanonicalResourceID(t *testing.T) {
	p := &IndexProducer{}
	payload := mustJSON(t, domain.Message{
		TS:        "123.456",
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "delete me",
	})

	job := p.eventToJob(domain.InternalEvent{
		ID:            2,
		EventType:     domain.EventMessageDeleted,
		AggregateType: domain.AggregateMessage,
		AggregateID:   "123.456",
		TeamID:        "T123",
		Payload:       payload,
	})
	if job == nil {
		t.Fatal("expected job")
	}
	if job.TeamID != "T123" {
		t.Fatalf("TeamID = %q, want %q", job.TeamID, "T123")
	}
	if job.Content != "" {
		t.Fatalf("Content = %q, want empty", job.Content)
	}
	if job.ResourceID != service.MessageSearchID("C123", "123.456") {
		t.Fatalf("ResourceID = %q, want %q", job.ResourceID, service.MessageSearchID("C123", "123.456"))
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
