package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type PinRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewPinRepo(db DBTX) *PinRepo {
	return &PinRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new PinRepo that operates within the given transaction.
func (r *PinRepo) WithTx(tx pgx.Tx) repository.PinRepository {
	return &PinRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *PinRepo) Add(ctx context.Context, params domain.PinParams) (*domain.Pin, error) {
	row, err := r.q.AddPin(ctx, sqlcgen.AddPinParams{
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

	return pinToDomain(row), nil
}

func (r *PinRepo) Remove(ctx context.Context, params domain.PinParams) error {
	return r.q.RemovePin(ctx, sqlcgen.RemovePinParams{
		ChannelID: params.ChannelID,
		MessageTs: params.MessageTS,
	})
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
