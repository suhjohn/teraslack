package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type PinRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewPinRepo(pool *pgxpool.Pool) *PinRepo {
	return &PinRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *PinRepo) Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.AddPin(ctx, sqlcgen.AddPinParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
		PinnedBy:  params.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("add pin: %w", err)
	}

	if row.ChannelID == "" {
		return nil, domain.ErrAlreadyPinned
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregatePin,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		EventType:     domain.EventPinAdded,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return pinToDomain(row), nil
}

func (r *PinRepo) Remove(ctx context.Context, params domain.PinParams) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.RemovePin(ctx, sqlcgen.RemovePinParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
	}); err != nil {
		return fmt.Errorf("remove pin: %w", err)
	}

	eventData, _ := json.Marshal(params)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregatePin,
		AggregateID:   params.ChannelID + ":" + params.MessageTS,
		EventType:     domain.EventPinRemoved,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *PinRepo) List(ctx context.Context, params domain.ListPinsParams) ([]domain.Pin, error) {
	rows, err := r.q.ListPins(ctx, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("list pins: %w", err)
	}

	pins := make([]domain.Pin, 0, len(rows))
	for _, row := range rows {
		pins = append(pins, *pinToDomain(row))
	}
	return pins, nil
}
