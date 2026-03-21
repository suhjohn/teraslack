package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
	"github.com/suhjohn/workspace/internal/repository/sqlcgen"
)

// timeNow is a package-level variable for testing.
var timeNow = time.Now

// UserRepo implements repository.UserRepository using sqlc with event sourcing.
type UserRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(db DBTX) *UserRepo {
	return &UserRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new UserRepo that operates within the given transaction.
func (r *UserRepo) WithTx(tx pgx.Tx) repository.UserRepository {
	return &UserRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *UserRepo) Create(ctx context.Context, params domain.CreateUserParams) (*domain.User, error) {
	id := generateID("U")
	profileJSON, err := json.Marshal(params.Profile)
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}

	row, err := r.q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:          id,
		TeamID:      params.TeamID,
		Name:        params.Name,
		RealName:    params.RealName,
		DisplayName: params.DisplayName,
		Email:       params.Email,
		IsBot:       params.IsBot,
		IsAdmin:     params.IsAdmin,
		Profile:     profileJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	return userToDomain(row)
}

func (r *UserRepo) Get(ctx context.Context, id string) (*domain.User, error) {
	row, err := r.q.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return userToDomain(row)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return userToDomain(row)
}

func (r *UserRepo) Update(ctx context.Context, id string, params domain.UpdateUserParams) (*domain.User, error) {
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

	row, err := r.q.UpdateUser(ctx, sqlcgen.UpdateUserParams{
		ID:           id,
		RealName:     realName,
		DisplayName:  displayName,
		Email:        email,
		IsAdmin:      isAdmin,
		IsRestricted: isRestricted,
		Deleted:      deleted,
		Profile:      profileJSON,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update user: %w", err)
	}

	return userToDomain(row)
}

func (r *UserRepo) List(ctx context.Context, params domain.ListUsersParams) (*domain.CursorPage[domain.User], error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	rows, err := r.q.ListUsers(ctx, sqlcgen.ListUsersParams{
		TeamID: params.TeamID,
		ID:     params.Cursor,
		Limit:  int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		u, err := userToDomain(row)
		if err != nil {
			return nil, fmt.Errorf("convert user: %w", err)
		}
		users = append(users, *u)
	}

	page := &domain.CursorPage[domain.User]{}
	if len(users) > limit {
		page.HasMore = true
		page.NextCursor = users[limit].ID
		page.Items = users[:limit]
	} else {
		page.Items = users
	}
	if page.Items == nil {
		page.Items = []domain.User{}
	}
	return page, nil
}

// generateID creates a Slack-style prefixed ID with random suffix for uniqueness.
func generateID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%d%s", prefix, timeNow().UnixNano(), hex.EncodeToString(b))
}
