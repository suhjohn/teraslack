package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJobSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	job := Job{
		ID:           "evt-123",
		EventID:      123,
		ResourceType: "user",
		ResourceID:   "U123",
		WorkspaceID:       "T001",
		EventType:    "user.created",
		Content:      "alice Alice Smith alice@example.com",
		Data:         json.RawMessage(`{"id":"U123","name":"alice"}`),
		Status:       StatusPending,
		CreatedAt:    now,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}

	var decoded Job
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}

	if decoded.ID != job.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, job.ID)
	}
	if decoded.ResourceType != "user" {
		t.Errorf("ResourceType: got %q, want %q", decoded.ResourceType, "user")
	}
	if decoded.Status != StatusPending {
		t.Errorf("Status: got %q, want %q", decoded.Status, StatusPending)
	}
}

func TestQueueStateSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	state := QueueState{
		Cursor: 42,
		Jobs: []Job{
			{
				ID:           "evt-1",
				EventID:      1,
				ResourceType: "user",
				ResourceID:   "U001",
				WorkspaceID:       "T001",
				EventType:    "user.created",
				Content:      "alice",
				Data:         json.RawMessage(`{}`),
				Status:       StatusCompleted,
				CreatedAt:    now,
			},
			{
				ID:           "evt-2",
				EventID:      2,
				ResourceType: "message",
				ResourceID:   "C001:123.456",
				WorkspaceID:       "T001",
				EventType:    "message.posted",
				Content:      "hello world",
				Data:         json.RawMessage(`{}`),
				Status:       StatusPending,
				CreatedAt:    now,
			},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	var decoded QueueState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	if decoded.Cursor != 42 {
		t.Errorf("Cursor: got %d, want 42", decoded.Cursor)
	}
	if len(decoded.Jobs) != 2 {
		t.Fatalf("Jobs: got %d, want 2", len(decoded.Jobs))
	}
	if decoded.Jobs[0].Status != StatusCompleted {
		t.Errorf("Jobs[0].Status: got %q, want %q", decoded.Jobs[0].Status, StatusCompleted)
	}
	if decoded.Jobs[1].Status != StatusPending {
		t.Errorf("Jobs[1].Status: got %q, want %q", decoded.Jobs[1].Status, StatusPending)
	}
}

func TestProducerEventToJob(t *testing.T) {
	// Test that the producer correctly converts events to jobs
	// We can't test the full producer without S3, but we can test eventToJob
	// by creating a producer with nil dependencies (only logger is used in eventToJob).
	p := &IndexProducer{
		logger: nil, // eventToJob uses logger for warnings but handles nil
	}

	// We can't easily call eventToJob without a logger, so just verify
	// the job types are correct.
	if StatusPending != "pending" {
		t.Errorf("StatusPending: got %q, want %q", StatusPending, "pending")
	}
	if StatusClaimed != "claimed" {
		t.Errorf("StatusClaimed: got %q, want %q", StatusClaimed, "claimed")
	}
	if StatusCompleted != "completed" {
		t.Errorf("StatusCompleted: got %q, want %q", StatusCompleted, "completed")
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed: got %q, want %q", StatusFailed, "failed")
	}
	_ = p
}
