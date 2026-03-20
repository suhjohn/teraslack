package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suhjohn/slackbackend/internal/domain"
)

// UserRepo implements repository.UserRepository using Postgres.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	id := generateID("U")
	profileJSON, err := json.Marshal(params.Profile)
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}

	var u domain.User
	var profileBytes []byte
	err = r.pool.QueryRow(ctx, `
		INSERT INTO users (id, team_id, name, real_name, display_name, email, is_bot, is_admin, profile)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, team_id, name, real_name, display_name, email, is_bot, is_admin, is_owner,
		          is_restricted, deleted, profile, created_at, updated_at`,
		id, params.TeamID, params.Name, params.RealName, params.DisplayName,
		params.Email, params.IsBot, params.IsAdmin, profileJSON,
	).Scan(
		&u.ID, &u.TeamID, &u.Name, &u.RealName, &u.DisplayName, &u.Email,
		&u.IsBot, &u.IsAdmin, &u.IsOwner, &u.IsRestricted, &u.Deleted,
		&profileBytes, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	if err := json.Unmarshal(profileBytes, &u.Profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &u, nil
}

func (r *UserRepo) Get(ctx context.Context, id string) (*domain.User, error) {
	return r.scanUser(ctx, `
		SELECT id, team_id, name, real_name, display_name, email, is_bot, is_admin, is_owner,
		       is_restricted, deleted, profile, created_at, updated_at
		FROM users WHERE id = $1`, id)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.scanUser(ctx, `
		SELECT id, team_id, name, real_name, display_name, email, is_bot, is_admin, is_owner,
		       is_restricted, deleted, profile, created_at, updated_at
		FROM users WHERE email = $1`, email)
}

func (r *UserRepo) Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error) {
	// Build dynamic update
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	realName := existing.RealName
	if params.RealName != nil {
		realName = *params.RealName
	}
	displayName := existing.DisplayName
	if params.DisplayName != nil {
		displayName = *params.DisplayName
	}
	email := existing.Email
	if params.Email != nil {
		email = *params.Email
	}
	isAdmin := existing.IsAdmin
	if params.IsAdmin != nil {
		isAdmin = *params.IsAdmin
	}
	isRestricted := existing.IsRestricted
	if params.IsRestricted != nil {
		isRestricted = *params.IsRestricted
	}
	deleted := existing.Deleted
	if params.Deleted != nil {
		deleted = *params.Deleted
	}
	profile := existing.Profile
	if params.Profile != nil {
		profile = *params.Profile
	}

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}

	var u domain.User
	var profileBytes []byte
	err = r.pool.QueryRow(ctx, `
		UPDATE users
		SET real_name = $2, display_name = $3, email = $4, is_admin = $5,
		    is_restricted = $6, deleted = $7, profile = $8
		WHERE id = $1
		RETURNING id, team_id, name, real_name, display_name, email, is_bot, is_admin, is_owner,
		          is_restricted, deleted, profile, created_at, updated_at`,
		id, realName, displayName, email, isAdmin, isRestricted, deleted, profileJSON,
	).Scan(
		&u.ID, &u.TeamID, &u.Name, &u.RealName, &u.DisplayName, &u.Email,
		&u.IsBot, &u.IsAdmin, &u.IsOwner, &u.IsRestricted, &u.Deleted,
		&profileBytes, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update user: %w", err)
	}
	if err := json.Unmarshal(profileBytes, &u.Profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &u, nil
}

func (r *UserRepo) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	var args []any
	query := `
		SELECT id, team_id, name, real_name, display_name, email, is_bot, is_admin, is_owner,
		       is_restricted, deleted, profile, created_at, updated_at
		FROM users
		WHERE team_id = $1`
	args = append(args, params.TeamID)

	if params.Cursor != "" {
		query += fmt.Sprintf(` AND id > $%d`, len(args)+1)
		args = append(args, params.Cursor)
	}

	query += ` ORDER BY id ASC`
	query += fmt.Sprintf(` LIMIT $%d`, len(args)+1)
	args = append(args, limit+1) // fetch one extra to determine has_more

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var u domain.User
		var profileBytes []byte
		if err := rows.Scan(
			&u.ID, &u.TeamID, &u.Name, &u.RealName, &u.DisplayName, &u.Email,
			&u.IsBot, &u.IsAdmin, &u.IsOwner, &u.IsRestricted, &u.Deleted,
			&profileBytes, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		if err := json.Unmarshal(profileBytes, &u.Profile); err != nil {
			return nil, fmt.Errorf("unmarshal profile: %w", err)
		}
		users = append(users, u)
	}

	page := &domain.CursorPage[domain.User]{}
	if len(users) > limit {
		page.HasMore = true
		page.NextCursor = users[limit-1].ID
		page.Items = users[:limit]
	} else {
		page.Items = users
	}
	if page.Items == nil {
		page.Items = []domain.User{}
	}
	return page, nil
}

func (r *UserRepo) scanUser(ctx context.Context, query string, args ...any) (*domain.User, error) {
	var u domain.User
	var profileBytes []byte
	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&u.ID, &u.TeamID, &u.Name, &u.RealName, &u.DisplayName, &u.Email,
		&u.IsBot, &u.IsAdmin, &u.IsOwner, &u.IsRestricted, &u.Deleted,
		&profileBytes, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}
	if err := json.Unmarshal(profileBytes, &u.Profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &u, nil
}

// generateID creates a Slack-style prefixed ID.
func generateID(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}
