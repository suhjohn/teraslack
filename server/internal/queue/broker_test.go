package queue

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestBrokerClientAndServerRoundTrip(t *testing.T) {
	t.Parallel()

	manager := NewManager(newMemoryStore(), "queues/test.json")
	server := httptest.NewServer(NewBrokerServer(map[string]*Manager{
		QueueIndex: manager,
	}))
	defer server.Close()

	client := NewBrokerClient(server.URL)
	producer := client.Producer(QueueIndex)
	consumer := client.Consumer(QueueIndex, "worker-a")

	if err := producer.Enqueue(context.Background(), Item{Kind: "job", Payload: []byte(`{"n":1}`)}); err != nil {
		t.Fatalf("enqueue via broker: %v", err)
	}

	jobs, err := consumer.Claim(context.Background(), 10)
	if err != nil {
		t.Fatalf("claim via broker: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("claimed %d jobs, want 1", len(jobs))
	}

	if err := consumer.Ack(context.Background(), jobs[0].ID); err != nil {
		t.Fatalf("ack via broker: %v", err)
	}

	jobs, err = consumer.Claim(context.Background(), 10)
	if err != nil {
		t.Fatalf("claim after ack via broker: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected empty queue after ack, found %d jobs", len(jobs))
	}
}
