package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the common interface between *pgxpool.Pool and pgx.Tx.
// Both satisfy this interface. When a pgx.Tx is used, Begin() creates
// a savepoint (nested transaction), which is the correct behavior for
// repos that manage internal transactions within an outer service-level tx.
type DBTX interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// beginOwnedTx returns the current transaction when db already is a pgx.Tx.
// The bool reports whether the caller owns the returned tx and therefore must
// commit or roll it back.
func beginOwnedTx(ctx context.Context, db DBTX) (pgx.Tx, bool, error) {
	if tx, ok := db.(pgx.Tx); ok {
		return tx, false, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	return tx, true, nil
}
