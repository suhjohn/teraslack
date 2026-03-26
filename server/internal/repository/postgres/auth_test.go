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

type captureDB struct {
	sql       string
	args      []any
	rowValues []any
}

func (db *captureDB) Begin(ctx context.Context) (pgx.Tx, error) { return nil, nil }
func (db *captureDB) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (db *captureDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (db *captureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.sql = sql
	db.args = append([]any(nil), args...)
	return captureRow{values: db.rowValues}
}

type captureRow struct {
	values []any
}

func (r captureRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan dest count mismatch: got %d want %d", len(dest), len(r.values))
	}
	for i, d := range dest {
		switch ptr := d.(type) {
		case *string:
			*ptr = r.values[i].(string)
		case *time.Time:
			switch v := r.values[i].(type) {
			case time.Time:
				*ptr = v
			case pgtype.Timestamptz:
				*ptr = v.Time
			default:
				return fmt.Errorf("unsupported source %T for %T", r.values[i], d)
			}
		case **time.Time:
			switch v := r.values[i].(type) {
			case nil:
				*ptr = nil
			case time.Time:
				tv := v
				*ptr = &tv
			case *time.Time:
				*ptr = v
			case pgtype.Timestamptz:
				tv := v.Time
				*ptr = &tv
			default:
				return fmt.Errorf("unsupported source %T for %T", r.values[i], d)
			}
		case *pgtype.Timestamptz:
			*ptr = r.values[i].(pgtype.Timestamptz)
		default:
			return fmt.Errorf("unsupported scan destination %T", d)
		}
	}
	return nil
}

func TestAuthRepo_CreateSession_DoesNotPersistRawToken(t *testing.T) {
	db := &captureDB{
		rowValues: []any{
			"AS123",
			"T123",
			"U123",
			"hash-123",
			"github",
			time.Time{},
			nil,
			time.Time{},
		},
	}
	repo := NewAuthRepo(db)

	session, err := repo.CreateSession(context.Background(), domain.CreateAuthSessionParams{
		WorkspaceID:    "T123",
		UserID:    "U123",
		Provider:  domain.AuthProviderGitHub,
		ExpiresAt: timeNow(),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.Token == "" {
		t.Fatal("expected raw session token in returned response")
	}
	if !strings.HasPrefix(session.Token, "sess_") {
		t.Fatalf("unexpected session token format %q", session.Token)
	}
	for i, arg := range db.args {
		s, ok := arg.(string)
		if ok && strings.HasPrefix(s, "sess_") {
			t.Fatalf("raw session token leaked into SQL args at position %d: %q", i, s)
		}
	}
}
