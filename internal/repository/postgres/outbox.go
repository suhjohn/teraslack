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

// ClaimBatch atomically claims up to `limit` pending outbox entries by setting
// their status to 'processing' and incrementing attempts. The UPDATE...RETURNING
// with FOR UPDATE SKIP LOCKED ensures no other worker can re-claim the same rows.
func (r *OutboxRepo) ClaimBatch(ctx context.Context, limit int) ([]domain.OutboxEntry, error) {
	rows, err := r.q.ClaimOutboxBatch(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("claim outbox batch: %w", err)
	}

	entries := make([]domain.OutboxEntry, len(rows))
	for i, row := range rows {
		entries[i] = outboxRowToDomain(row)
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
