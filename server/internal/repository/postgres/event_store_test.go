package postgres

import (
	"encoding/json"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestDeriveInternalEventShard_UsesConversationIDForConversationScopedEvents(t *testing.T) {
	messagePayload, _ := json.Marshal(domain.Message{
		TS:        "1712345678.000001",
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "hello",
	})
	reactionPayload, _ := json.Marshal(map[string]any{
		"reaction": map[string]any{
			"channel_id": "C123",
			"message_ts": "1712345678.000001",
			"user_id":    "U123",
			"emoji":      "wave",
		},
	})

	tests := []struct {
		name         string
		event        domain.InternalEvent
		wantShardKey string
	}{
		{
			name: "conversation aggregate",
			event: domain.InternalEvent{
				AggregateType: domain.AggregateConversation,
				AggregateID:   "C123",
				WorkspaceID:   "T123",
			},
			wantShardKey: "C123",
		},
		{
			name: "message payload channel",
			event: domain.InternalEvent{
				AggregateType: domain.AggregateMessage,
				AggregateID:   "1712345678.000001",
				WorkspaceID:   "T123",
				Payload:       messagePayload,
			},
			wantShardKey: "C123",
		},
		{
			name: "reaction envelope channel",
			event: domain.InternalEvent{
				AggregateType: domain.AggregateMessage,
				AggregateID:   "1712345678.000001",
				WorkspaceID:   "T123",
				Payload:       reactionPayload,
			},
			wantShardKey: "C123",
		},
		{
			name: "fallback to workspace",
			event: domain.InternalEvent{
				AggregateType: domain.AggregateUser,
				AggregateID:   "U123",
				WorkspaceID:   "T123",
			},
			wantShardKey: "T123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shardKey, shardID := deriveInternalEventShard(tt.event)
			if shardKey != tt.wantShardKey {
				t.Fatalf("shard key = %q, want %q", shardKey, tt.wantShardKey)
			}
			if shardID < 0 || shardID >= domain.InternalEventShardCount {
				t.Fatalf("shard id = %d, want [0,%d)", shardID, domain.InternalEventShardCount)
			}
		})
	}
}
