package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
)

type messageCaptureDB struct {
	sql       string
	args      []any
	rowValues []any
}

func (db *messageCaptureDB) Begin(ctx context.Context) (pgx.Tx, error) { return db, nil }
func (db *messageCaptureDB) Commit(ctx context.Context) error          { return nil }
func (db *messageCaptureDB) Rollback(ctx context.Context) error        { return nil }
func (db *messageCaptureDB) CopyFrom(ctx context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (db *messageCaptureDB) SendBatch(ctx context.Context, _ *pgx.Batch) pgx.BatchResults {
	return nil
}
func (db *messageCaptureDB) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (db *messageCaptureDB) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (db *messageCaptureDB) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (db *messageCaptureDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (db *messageCaptureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.sql = sql
	db.args = append([]any(nil), args...)
	return messageCaptureRow{values: db.rowValues}
}
func (db *messageCaptureDB) Conn() *pgx.Conn { return nil }

type messageCaptureRow struct {
	values []any
}

func (r messageCaptureRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan dest count mismatch: got %d want %d", len(dest), len(r.values))
	}
	for i, d := range dest {
		switch ptr := d.(type) {
		case *string:
			*ptr = r.values[i].(string)
		case *pgtype.Text:
			switch v := r.values[i].(type) {
			case nil:
				*ptr = pgtype.Text{}
			case string:
				*ptr = pgtype.Text{String: v, Valid: true}
			case pgtype.Text:
				*ptr = v
			default:
				return fmt.Errorf("unsupported source %T for %T", r.values[i], d)
			}
		case *[]byte:
			switch v := r.values[i].(type) {
			case nil:
				*ptr = nil
			case []byte:
				*ptr = v
			case string:
				*ptr = []byte(v)
			default:
				return fmt.Errorf("unsupported source %T for %T", r.values[i], d)
			}
		case *int32:
			switch v := r.values[i].(type) {
			case int32:
				*ptr = v
			case int:
				*ptr = int32(v)
			default:
				return fmt.Errorf("unsupported source %T for %T", r.values[i], d)
			}
		case *bool:
			*ptr = r.values[i].(bool)
		case *time.Time:
			switch v := r.values[i].(type) {
			case time.Time:
				*ptr = v
			case pgtype.Timestamptz:
				*ptr = v.Time
			default:
				return fmt.Errorf("unsupported source %T for %T", r.values[i], d)
			}
		default:
			return fmt.Errorf("unsupported scan destination %T", d)
		}
	}
	return nil
}

func TestMessageRepo_Create_UsesWorkspaceMembershipAwareAuthoring(t *testing.T) {
	db := &messageCaptureDB{
		rowValues: []any{
			"1712345678.123456",
			"C123",
			"U123",
			pgtype.Text{String: "A123", Valid: true},
			pgtype.Text{String: "WM123", Valid: true},
			"hello",
			pgtype.Text{},
			"message",
			pgtype.Text{},
			[]byte(`{"blocks":true}`),
			[]byte(`{"metadata":true}`),
			pgtype.Text{},
			pgtype.Text{},
			int32(0),
			int32(0),
			pgtype.Text{},
			false,
			time.Unix(1712345678, 0).UTC(),
			time.Unix(1712345679, 0).UTC(),
		},
	}
	repo := NewMessageRepo(db)

	msg, err := repo.Create(context.Background(), domain.PostMessageParams{
		ChannelID:       "C123",
		UserID:          "U123",
		AuthorAccountID: "A123",
		Text:            "hello",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !strings.Contains(db.sql, "JOIN workspace_memberships") {
		t.Fatalf("Create() should use the workspace-membership-aware path, got SQL %q", db.sql)
	}
	if msg.AuthorAccountID != "A123" {
		t.Fatalf("AuthorAccountID = %q, want %q", msg.AuthorAccountID, "A123")
	}
	if msg.AuthorWorkspaceMembershipID != "WM123" {
		t.Fatalf("AuthorWorkspaceMembershipID = %q, want %q", msg.AuthorWorkspaceMembershipID, "WM123")
	}
	if msg.UserID != "U123" {
		t.Fatalf("UserID = %q, want %q", msg.UserID, "U123")
	}
}
