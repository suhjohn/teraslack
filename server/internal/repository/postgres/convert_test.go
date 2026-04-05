package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

func TestConvToDomain_ListVisibleConversationCarriesUnreadState(t *testing.T) {
	now := time.Now().UTC()
	row := sqlcgen.ListVisibleConversationsRow{
		ID:             "C123",
		WorkspaceID:    pgtype.Text{String: "T123", Valid: true},
		Name:           "general",
		Type:           "public_channel",
		CreatorID:      pgtype.Text{String: "U123", Valid: true},
		IsArchived:     false,
		TopicValue:     "topic",
		TopicCreator:   "U123",
		PurposeValue:   "purpose",
		PurposeCreator: "U123",
		NumMembers:     2,
		LastMessageTs:  pgtype.Text{String: "1712345678.000001", Valid: true},
		LastActivityTs: pgtype.Text{String: "1712345679.000001", Valid: true},
		LastReadTs:     "1712345678.000001",
		HasUnread:      pgtype.Bool{Bool: false, Valid: true},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	conv := convToDomain(row)
	if conv.LastReadTS == nil || *conv.LastReadTS != "1712345678.000001" {
		t.Fatalf("LastReadTS = %v, want %q", conv.LastReadTS, "1712345678.000001")
	}
	if conv.HasUnread == nil || *conv.HasUnread != false {
		t.Fatalf("HasUnread = %v, want false", conv.HasUnread)
	}
}
