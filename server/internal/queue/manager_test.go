package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	s3store "github.com/johnsuh/teraslack/server/internal/s3"
)

type memoryStore struct {
	mu      sync.Mutex
	bodies  map[string][]byte
	etags   map[string]string
	nextTag int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		bodies: make(map[string][]byte),
		etags:  make(map[string]string),
	}
}

func (s *memoryStore) Read(ctx context.Context, key string) (s3store.ReadResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.bodies[key]
	if !ok {
		return s3store.ReadResult{}, s3store.ErrNotFound
	}
	return s3store.ReadResult{
		Body:   append([]byte(nil), body...),
		ETag:   s.etags[key],
		Exists: true,
	}, nil
}

func (s *memoryStore) WriteCAS(ctx context.Context, key string, body []byte, expectedETag string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	currentETag, exists := s.etags[key]
	switch {
	case !exists && expectedETag != "":
		return "", s3store.ErrCASMismatch
	case exists && currentETag != expectedETag:
		return "", s3store.ErrCASMismatch
	}
	s.nextTag++
	newETag := fmt.Sprintf("etag-%d", s.nextTag)
	s.bodies[key] = append([]byte(nil), body...)
	s.etags[key] = newETag
	return newETag, nil
}

func TestManagerConcurrentEnqueuePreservesAllJobs(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	first := NewManager(store, "queues/test.json")
	second := NewManager(store, "queues/test.json")
	firstProducer := first.Producer()
	secondProducer := second.Producer()
	consumer := first.Consumer("worker-a")

	var wg sync.WaitGroup
	for idx := 0; idx < 25; idx++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			err := firstProducer.Enqueue(context.Background(), Item{Kind: "job", Payload: []byte(fmt.Sprintf(`{"source":"first","n":%d}`, i))})
			if err != nil {
				t.Errorf("first enqueue %d: %v", i, err)
			}
		}(idx)
		go func(i int) {
			defer wg.Done()
			err := secondProducer.Enqueue(context.Background(), Item{Kind: "job", Payload: []byte(fmt.Sprintf(`{"source":"second","n":%d}`, i))})
			if err != nil {
				t.Errorf("second enqueue %d: %v", i, err)
			}
		}(idx)
	}
	wg.Wait()

	response, err := consumer.Claim(context.Background(), 100)
	if err != nil {
		t.Fatalf("claim queued jobs: %v", err)
	}
	if got, want := len(response), 50; got != want {
		t.Fatalf("claimed %d jobs, want %d", got, want)
	}
}

func TestManagerReclaimsExpiredLease(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	manager := NewManager(store, "queues/test.json")
	ctx := context.Background()
	producer := manager.Producer()
	firstConsumer := manager.Consumer("worker-a").WithLeaseDuration(time.Second)
	secondConsumer := manager.Consumer("worker-b").WithLeaseDuration(time.Second)

	if err := producer.Enqueue(ctx, Item{Kind: "job", Payload: []byte(`{"n":1}`)}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	firstClaim, err := firstConsumer.Claim(ctx, 1)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if len(firstClaim) != 1 {
		t.Fatalf("first claim returned %d jobs, want 1", len(firstClaim))
	}

	time.Sleep(1100 * time.Millisecond)

	secondClaim, err := secondConsumer.Claim(ctx, 1)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(secondClaim) != 1 {
		t.Fatalf("second claim returned %d jobs, want 1", len(secondClaim))
	}
	if got := secondClaim[0].Attempt; got != 2 {
		t.Fatalf("second claim attempt = %d, want 2", got)
	}
}

func TestManagerSeesJobsEnqueuedByAnotherManagerAfterEmptyClaim(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	producerManager := NewManager(store, "queues/test.json")
	consumerManager := NewManager(store, "queues/test.json")
	producer := producerManager.Producer()
	consumer := consumerManager.Consumer("worker-a")
	ctx := context.Background()

	jobs, err := consumer.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("initial empty claim: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("initial claim returned %d jobs, want 0", len(jobs))
	}

	if err := producer.Enqueue(ctx, Item{Kind: "job", Payload: []byte(`{"n":1}`)}); err != nil {
		t.Fatalf("enqueue after empty claim: %v", err)
	}

	jobs, err = consumer.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("claim after enqueue: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("claim after enqueue returned %d jobs, want 1", len(jobs))
	}
}

func TestConsumeOnceAcknowledgesSuccessfulJobs(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	manager := NewManager(store, "queues/test.json")
	producer := manager.Producer()
	consumer := manager.Consumer("worker-a")

	if err := producer.Enqueue(context.Background(), Item{Kind: "job", Payload: []byte(`{"n":1}`)}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	calls := 0
	err := ConsumeOnce(context.Background(), consumer, 10, time.Second, time.Second, func(ctx context.Context, job ClaimedJob) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("consume once: %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}

	jobs, err := consumer.Claim(context.Background(), 10)
	if err != nil {
		t.Fatalf("claim after ack: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected queue to be empty, found %d jobs", len(jobs))
	}
}
