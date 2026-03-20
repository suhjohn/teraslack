package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

type UsergroupRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewUsergroupRepo(pool *pgxpool.Pool) *UsergroupRepo {
	return &UsergroupRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *UsergroupRepo) Create(ctx context.Context, params domain.CreateUsergroupParams) (*domain.Usergroup, error) {
	id := generateID("S")

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.CreateUsergroup(ctx, sqlcgen.CreateUsergroupParams{
		ID:          id,
		TeamID:      params.TeamID,
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

	ug := usergroupToDomain(row)
	ug.UserCount = len(params.Users)

	eventData, _ := json.Marshal(ug)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupCreated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	row, err := qtx.UpdateUsergroup(ctx, sqlcgen.UpdateUsergroupParams{
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

	updatedUg := usergroupToDomain(row)

	eventData, _ := json.Marshal(updatedUg)
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupUpdated,
		EventData:     eventData,
	}); err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return updatedUg, nil
}

func (r *UsergroupRepo) List(ctx context.Context, params domain.ListUsergroupsParams) ([]domain.Usergroup, error) {
	var rows []sqlcgen.Usergroup
	var err error

	if params.IncludeDisabled {
		rows, err = r.q.ListUsergroupsIncludeDisabled(ctx, params.TeamID)
	} else {
		rows, err = r.q.ListUsergroups(ctx, params.TeamID)
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.EnableUsergroup(ctx, id); err != nil {
		return fmt.Errorf("enable usergroup: %w", err)
	}

	// Fetch full entity after enable for snapshot
	ugRow, err := qtx.GetUsergroup(ctx, id)
	if err != nil {
		return fmt.Errorf("get enabled usergroup: %w", err)
	}
	eventData, _ := json.Marshal(usergroupToDomain(ugRow))
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupEnabled,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *UsergroupRepo) Disable(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.q.WithTx(tx)

	if err := qtx.DisableUsergroup(ctx, id); err != nil {
		return fmt.Errorf("disable usergroup: %w", err)
	}

	// Fetch full entity after disable for snapshot
	ugRow, err := qtx.GetUsergroup(ctx, id)
	if err != nil {
		return fmt.Errorf("get disabled usergroup: %w", err)
	}
	eventData, _ := json.Marshal(usergroupToDomain(ugRow))
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   id,
		EventType:     domain.EventUsergroupDisabled,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return tx.Commit(ctx)
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
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

	// Fetch full entity after user set for snapshot
	ugRow, ugErr := qtx.GetUsergroup(ctx, usergroupID)
	if ugErr != nil {
		return fmt.Errorf("get usergroup after set users: %w", ugErr)
	}
	eventData, _ := json.Marshal(map[string]interface{}{
		"user_ids":  userIDs,
		"usergroup": usergroupToDomain(ugRow),
	})
	if _, err := qtx.AppendEvent(ctx, sqlcgen.AppendEventParams{
		AggregateType: domain.AggregateUsergroup,
		AggregateID:   usergroupID,
		EventType:     domain.EventUsergroupUserSet,
		EventData:     eventData,
	}); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return tx.Commit(ctx)
}
