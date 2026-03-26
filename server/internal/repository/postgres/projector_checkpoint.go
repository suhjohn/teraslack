package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type ProjectorCheckpointRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewProjectorCheckpointRepo(db DBTX) *ProjectorCheckpointRepo {
	return &ProjectorCheckpointRepo{q: sqlcgen.New(db), db: db}
}

func (r *ProjectorCheckpointRepo) WithTx(tx pgx.Tx) repository.ProjectorCheckpointRepository {
	return &ProjectorCheckpointRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ProjectorCheckpointRepo) Get(ctx context.Context, name string) (int64, error) {
	lastEventID, err := r.q.GetProjectorCheckpoint(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get projector checkpoint: %w", err)
	}
	return lastEventID, nil
}

func (r *ProjectorCheckpointRepo) Set(ctx context.Context, name string, lastEventID int64) error {
	if err := r.q.SetProjectorCheckpoint(ctx, sqlcgen.SetProjectorCheckpointParams{Name: name, LastEventID: lastEventID}); err != nil {
		return fmt.Errorf("set projector checkpoint: %w", err)
	}
	return nil
}
