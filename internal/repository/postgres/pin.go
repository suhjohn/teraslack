package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
)

// PinRepo implements repository.PinRepository using Postgres.
type PinRepo struct {
	pool *pgxpool.Pool
}

// NewPinRepo creates a new PinRepo.
func NewPinRepo(pool *pgxpool.Pool) *PinRepo {
	return &PinRepo{pool: pool}
}

func (r *PinRepo) Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error) {
	var p domain.Pin
	err := r.pool.QueryRow(ctx, `
		INSERT INTO pins (channel_id, message_ts, pinned_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id, message_ts) DO NOTHING
		RETURNING channel_id, message_ts, pinned_by, pinned_at`,
		params.ChannelID, params.MessageTS, params.UserID,
	).Scan(&p.ChannelID, &p.MessageTS, &p.PinnedBy, &p.PinnedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("add pin: %w", err)
	}
	return &p, nil
}

func (r *PinRepo) Remove(ctx context.Context, params domain.PinParams) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM pins WHERE channel_id = $1 AND message_ts = $2`,
		params.ChannelID, params.MessageTS)
	if err != nil {
		return fmt.Errorf("remove pin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PinRepo) List(ctx context.Context, params domain.ListPinsParams) ([]domain.Pin, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT channel_id, message_ts, pinned_by, pinned_at
		FROM pins WHERE channel_id = $1
		ORDER BY pinned_at DESC`, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("list pins: %w", err)
	}
	defer rows.Close()

	var pins []domain.Pin
	for rows.Next() {
		var p domain.Pin
		if err := rows.Scan(&p.ChannelID, &p.MessageTS, &p.PinnedBy, &p.PinnedAt); err != nil {
			return nil, fmt.Errorf("scan pin: %w", err)
		}
		pins = append(pins, p)
	}
	if pins == nil {
		pins = []domain.Pin{}
	}
	return pins, nil
}
