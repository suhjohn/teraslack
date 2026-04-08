package eventsourcing

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func TestExternalEventDedupeKeyUsesCanonicalUUIDString(t *testing.T) {
	internalEventID := uuid.MustParse("4bd07e1d-c57f-4a0a-aa08-f88111525e36")
	eventType := "conversation.participant.added"

	got := externalEventDedupeKey(internalEventID, eventType)
	want := "internal:" + internalEventID.String() + ":" + eventType
	legacy := fmt.Sprintf("internal:%d:%s", internalEventID, eventType)

	if got != want {
		t.Fatalf("externalEventDedupeKey() = %q, want %q", got, want)
	}
	if got == legacy {
		t.Fatalf("externalEventDedupeKey() = %q, unexpectedly matched legacy byte-array format", got)
	}
}
