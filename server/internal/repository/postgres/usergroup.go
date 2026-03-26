package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type UsergroupRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewUsergroupRepo(db DBTX) *UsergroupRepo {
	return &UsergroupRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new UsergroupRepo that operates within the given transaction.
func (r *UsergroupRepo) WithTx(tx pgx.Tx) repository.UsergroupRepository {
	return &UsergroupRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *UsergroupRepo) Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	id := generateID("S")

	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateUsergroup(ctx, sqlcgen.CreateUsergroupParams{
		ID:          id,
		WorkspaceID:      params.WorkspaceID,
		Name:        params.Name,
		Handle:      params.Handle,
		Description: params.Description,
		CreatedBy:   params.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("insert usergroup: %w", err)
	}

	for _, userID := range params.Users {
		if err := qtx.AddUsergroupMember(ctx, sqlcgen.AddUsergroupMemberParams{
			UsergroupID: id,
			UserID:      userID,
		}); err != nil {
			return nil, fmt.Errorf("add member: %w", err)
		}
	}
	if len(params.Users) > 0 {
		if err := qtx.UpdateUsergroupUserCount(ctx, id); err != nil {
			return nil, fmt.Errorf("update user count: %w", err)
		}
	}

	if ownTx {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
	}

	ug := usergroupToDomain(row)
	ug.UserCount = len(params.Users)
	return ug, nil
}

func (r *UsergroupRepo) Get(ctx context.Context, id string) (*domain.Usergroup, error) {
	row, err := r.q.GetUsergroup(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get usergroup: %w", err)
	}
	return usergroupToDomain(row), nil
}

func (r *UsergroupRepo) Update(ctx context.Context, id string, params domain.UpdateUsergroupParams) (*domain.Usergroup, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if params.Name != nil {
		name = *params.Name
	}
	handle := existing.Handle
	if params.Handle != nil {
		handle = *params.Handle
	}
	description := existing.Description
	if params.Description != nil {
		description = *params.Description
	}

	row, err := r.q.UpdateUsergroup(ctx, sqlcgen.UpdateUsergroupParams{
		ID:          id,
		Name:        name,
		Handle:      handle,
		Description: description,
		UpdatedBy:   params.UpdatedBy,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update usergroup: %w", err)
	}

	return usergroupToDomain(row), nil
}

func (r *UsergroupRepo) List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	var rows []sqlcgen.Usergroup
	var err error

	if params.IncludeDisabled {
		rows, err = r.q.ListUsergroupsIncludeDisabled(ctx, params.WorkspaceID)
	} else {
		rows, err = r.q.ListUsergroups(ctx, params.WorkspaceID)
	}
	if err != nil {
		return nil, fmt.Errorf("list usergroups: %w", err)
	}

	result := make([]domain.Usergroup, 0, len(rows))
	for _, row := range rows {
		result = append(result, *usergroupToDomain(row))
	}
	return result, nil
}

func (r *UsergroupRepo) Enable(ctx context.Context, id string) error {
	return r.q.EnableUsergroup(ctx, id)
}

func (r *UsergroupRepo) Disable(ctx context.Context, id string) error {
	return r.q.DisableUsergroup(ctx, id)
}

func (r *UsergroupRepo) AddUser(ctx context.Context, usergroupID, userID string) error {
	if err := r.q.AddUsergroupMember(ctx, sqlcgen.AddUsergroupMemberParams{
		UsergroupID: usergroupID,
		UserID:      userID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return r.q.UpdateUsergroupUserCount(ctx, usergroupID)
}

func (r *UsergroupRepo) ListUsers(ctx context.Context, usergroupID string) ([]string, error) {
	users, err := r.q.ListUsergroupMembers(ctx, usergroupID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return users, nil
}

func (r *UsergroupRepo) SetUsers(ctx context.Context, usergroupID string, userIDs []string) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}
	qtx := r.q.WithTx(tx)

	if err := qtx.DeleteUsergroupMembers(ctx, usergroupID); err != nil {
		return fmt.Errorf("delete members: %w", err)
	}

	for _, userID := range userIDs {
		if err := qtx.InsertUsergroupMember(ctx, sqlcgen.InsertUsergroupMemberParams{
			UsergroupID: usergroupID,
			UserID:      userID,
		}); err != nil {
			return fmt.Errorf("insert member: %w", err)
		}
	}

	if err := qtx.SetUsergroupUserCount(ctx, sqlcgen.SetUsergroupUserCountParams{
		ID:        usergroupID,
		UserCount: int32(len(userIDs)),
	}); err != nil {
		return fmt.Errorf("set user count: %w", err)
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
}
