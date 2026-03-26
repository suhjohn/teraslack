package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type internalEventStoreStub struct {
	eventsByShard map[int][]domain.InternalEvent
	requested     []int
}

func (s *internalEventStoreStub) WithTx(tx pgx.Tx) repository.InternalEventStoreRepository { return s }
func (s *internalEventStoreStub) Append(ctx context.Context, event domain.InternalEvent) (*domain.InternalEvent, error) {
	return &event, nil
}
func (s *internalEventStoreStub) GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]domain.InternalEvent, error) {
	return nil, nil
}
func (s *internalEventStoreStub) GetAllSince(ctx context.Context, sinceID int64, limit int) ([]domain.InternalEvent, error) {
	return nil, nil
}
func (s *internalEventStoreStub) GetAllSinceByShard(ctx context.Context, shardID int, sinceID int64, limit int) ([]domain.InternalEvent, error) {
	s.requested = append(s.requested, shardID)
	events := s.eventsByShard[shardID]
	items := make([]domain.InternalEvent, 0, len(events))
	for _, event := range events {
		if event.ID > sinceID {
			items = append(items, event)
		}
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

type checkpointRepoStub struct {
	values map[string]int64
}

func (r *checkpointRepoStub) WithTx(tx pgx.Tx) repository.ProjectorCheckpointRepository { return r }
func (r *checkpointRepoStub) Get(ctx context.Context, name string) (int64, error) {
	return r.values[name], nil
}
func (r *checkpointRepoStub) Set(ctx context.Context, name string, lastEventID int64) error {
	if r.values == nil {
		r.values = map[string]int64{}
	}
	r.values[name] = lastEventID
	return nil
}

type projectorExternalRepoStub struct {
	inserted []domain.ExternalEvent
}

func (r *projectorExternalRepoStub) WithTx(tx pgx.Tx) repository.ExternalEventRepository { return r }
func (r *projectorExternalRepoStub) Insert(ctx context.Context, event domain.ExternalEvent) (*domain.ExternalEvent, error) {
	r.inserted = append(r.inserted, event)
	return &event, nil
}
func (r *projectorExternalRepoStub) RecordProjectionFailure(ctx context.Context, internalEventID int64, message string) error {
	return nil
}
func (r *projectorExternalRepoStub) ListVisible(ctx context.Context, principal repository.ExternalEventPrincipal, params domain.ListExternalEventsParams) (*domain.CursorPage[domain.ExternalEvent], error) {
	return &domain.CursorPage[domain.ExternalEvent]{Items: []domain.ExternalEvent{}}, nil
}
func (r *projectorExternalRepoStub) GetSince(ctx context.Context, afterID int64, limit int) ([]domain.ExternalEvent, error) {
	return nil, nil
}
func (r *projectorExternalRepoStub) Rebuild(ctx context.Context, events []domain.ExternalEvent) error {
	r.inserted = nil
	return nil
}
func (r *projectorExternalRepoStub) RebuildFeeds(ctx context.Context) error { return nil }

func TestExternalEventProjector_ProcessPending_UsesOwnedShardsAndShardCheckpoints(t *testing.T) {
	convPayload, _ := json.Marshal(domain.Conversation{
		ID:        "C123",
		WorkspaceID:    "T123",
		Name:      "general",
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: "U123",
	})

	internal := &internalEventStoreStub{
		eventsByShard: map[int][]domain.InternalEvent{
			1: {{
				ID:            41,
				EventType:     domain.EventConversationCreated,
				AggregateType: domain.AggregateConversation,
				AggregateID:   "C123",
				WorkspaceID:        "T123",
				ShardKey:      "C123",
				ShardID:       1,
				Payload:       convPayload,
				CreatedAt:     time.Now().UTC(),
			}},
			2: {{
				ID:            99,
				EventType:     domain.EventConversationCreated,
				AggregateType: domain.AggregateConversation,
				AggregateID:   "C999",
				WorkspaceID:        "T123",
				ShardKey:      "C999",
				ShardID:       2,
				Payload:       convPayload,
				CreatedAt:     time.Now().UTC(),
			}},
		},
	}
	external := &projectorExternalRepoStub{}
	checkpoints := &checkpointRepoStub{values: map[string]int64{}}
	projector := NewExternalEventProjector(
		mockTxBeginner{},
		internal,
		external,
		checkpoints,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	projector.SetOwnedShards([]int{1})

	if err := projector.ProcessPending(context.Background()); err != nil {
		t.Fatalf("ProcessPending() error = %v", err)
	}
	if len(internal.requested) == 0 || internal.requested[0] != 1 {
		t.Fatalf("requested shards = %v, want shard 1 only", internal.requested)
	}
	if got := checkpoints.values[externalEventProjectorCheckpointName(1)]; got != 41 {
		t.Fatalf("checkpoint shard 1 = %d, want 41", got)
	}
	if got := checkpoints.values[externalEventProjectorCheckpointName(2)]; got != 0 {
		t.Fatalf("checkpoint shard 2 = %d, want 0", got)
	}
	if len(external.inserted) != 1 || external.inserted[0].ResourceID != "C123" {
		t.Fatalf("inserted external events = %+v, want one event for C123", external.inserted)
	}
}
