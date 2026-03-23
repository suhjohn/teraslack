package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ProjectorCheckpointRepo struct {
	db DBTX
}

func NewProjectorCheckpointRepo(db DBTX) *ProjectorCheckpointRepo {
	return &ProjectorCheckpointRepo{db: db}
}

func (r *ProjectorCheckpointRepo) WithTx(tx pgx.Tx) repository.ProjectorCheckpointRepository {
	return &ProjectorCheckpointRepo{db: tx}
}

func (r *ProjectorCheckpointRepo) Get(ctx context.Context, name string) (int64, error) {
	var lastEventID int64
	err := r.db.QueryRow(ctx, `
		SELECT last_event_id
		FROM projector_checkpoints
		WHERE name = $1
	`, name).Scan(&lastEventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get projector checkpoint: %w", err)
	}
	return lastEventID, nil
}

func (r *ProjectorCheckpointRepo) Set(ctx context.Context, name string, lastEventID int64) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO projector_checkpoints (name, last_event_id)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET
			last_event_id = EXCLUDED.last_event_id,
			updated_at = NOW()
	`, name, lastEventID)
	if err != nil {
		return fmt.Errorf("set projector checkpoint: %w", err)
	}
	return nil
}
