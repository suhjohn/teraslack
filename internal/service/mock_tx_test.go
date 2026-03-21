package service

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/suhjohn/workspace/internal/repository"
)

// mockTx implements pgx.Tx for testing — all operations are no-ops.
type mockTx struct{}

func (mockTx) Begin(ctx context.Context) (pgx.Tx, error)                          { return mockTx{}, nil }
func (mockTx) Commit(ctx context.Context) error                                    { return nil }
func (mockTx) Rollback(ctx context.Context) error                                  { return nil }
func (mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (mockTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (mockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}
func (mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (mockTx) Conn() *pgx.Conn                                               { return nil }

// mockTxBeginner implements repository.TxBeginner for testing.
type mockTxBeginner struct{}

func (mockTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	return mockTx{}, nil
}

// Compile-time check
var _ repository.TxBeginner = mockTxBeginner{}
