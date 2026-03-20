package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

// OutboxRepo implements repository.OutboxRepository using Postgres.
type OutboxRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

// NewOutboxRepo creates a new OutboxRepo.
func NewOutboxRepo(pool *pgxpool.Pool) *OutboxRepo {
	return &OutboxRepo{pool: pool, q: sqlcgen.New(pool)}
}

// ClaimBatch claims up to `limit` pending outbox entries using FOR UPDATE SKIP LOCKED.
// This must run inside a transaction so the locks are held until delivery completes.
func (r *OutboxRepo) ClaimBatch(ctx context.Context, limit int) ([]domain.OutboxEntry, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	rows, err := qtx.ClaimOutboxBatch(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("claim outbox batch: %w", err)
	}

	entries := make([]domain.OutboxEntry, len(rows))
	for i, row := range rows {
		entries[i] = outboxRowToDomain(row)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return entries, nil
}

// MarkDelivered marks an outbox entry as successfully delivered.
func (r *OutboxRepo) MarkDelivered(ctx context.Context, id int64) error {
	return r.q.MarkOutboxDelivered(ctx, id)
}

// MarkFailed marks an outbox entry as permanently failed.
func (r *OutboxRepo) MarkFailed(ctx context.Context, id int64, lastError string) error {
	return r.q.MarkOutboxFailed(ctx, sqlcgen.MarkOutboxFailedParams{
		ID:        id,
		LastError: lastError,
	})
}

// ScheduleRetry schedules an outbox entry for retry at a future time.
func (r *OutboxRepo) ScheduleRetry(ctx context.Context, id int64, nextAttemptAt time.Time, lastError string) error {
	return r.q.ScheduleOutboxRetry(ctx, sqlcgen.ScheduleOutboxRetryParams{
		ID:            id,
		NextAttemptAt: pgtype.Timestamptz{Time: nextAttemptAt, Valid: true},
		LastError:     lastError,
	})
}

// CleanupDelivered removes delivered outbox entries older than the given duration.
func (r *OutboxRepo) CleanupDelivered(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := r.q.CleanupDeliveredOutbox(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("cleanup delivered outbox: %w", err)
	}
	return result.RowsAffected(), nil
}

func outboxRowToDomain(o sqlcgen.Outbox) domain.OutboxEntry {
	entry := domain.OutboxEntry{
		ID:             o.ID,
		EventID:        o.EventID,
		SubscriptionID: o.SubscriptionID,
		URL:            o.Url,
		Payload:        o.Payload,
		Secret:         o.Secret,
		Status:         o.Status,
		Attempts:       int(o.Attempts),
		MaxAttempts:    int(o.MaxAttempts),
		NextAttemptAt:  tsToTime(o.NextAttemptAt),
		LastError:      o.LastError,
		DeliveredAt:    tsToTimePtr(o.DeliveredAt),
		CreatedAt:      tsToTime(o.CreatedAt),
	}
	return entry
}
