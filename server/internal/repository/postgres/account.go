package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type AccountRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewAccountRepo(db DBTX) *AccountRepo {
	return &AccountRepo{q: sqlcgen.New(db), db: db}
}

func (r *AccountRepo) WithTx(tx pgx.Tx) repository.AccountRepository {
	return &AccountRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *AccountRepo) Create(ctx context.Context, params domain.CreateAccountParams) (*domain.Account, error) {
	profileJSON, err := json.Marshal(params.Profile)
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}

	row, err := r.q.CreateAccount(ctx, sqlcgen.CreateAccountParams{
		ID:            generateID("A"),
		PrincipalType: string(params.PrincipalType),
		Name:          params.Name,
		RealName:      params.RealName,
		DisplayName:   params.DisplayName,
		Email:         params.Email,
		IsBot:         params.IsBot,
		Deleted:       params.Deleted,
		Profile:       profileJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	return accountFromRow(
		row.ID,
		row.PrincipalType,
		row.Name,
		row.RealName,
		row.DisplayName,
		row.Email,
		row.IsBot,
		row.Deleted,
		row.Profile,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func (r *AccountRepo) Get(ctx context.Context, id string) (*domain.Account, error) {
	row, err := r.q.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get account: %w", err)
	}
	return accountFromRow(
		row.ID,
		row.PrincipalType,
		row.Name,
		row.RealName,
		row.DisplayName,
		row.Email,
		row.IsBot,
		row.Deleted,
		row.Profile,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func (r *AccountRepo) GetByEmail(ctx context.Context, email string) (*domain.Account, error) {
	row, err := r.q.GetAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get account by email: %w", err)
	}
	return accountFromRow(
		row.ID,
		row.PrincipalType,
		row.Name,
		row.RealName,
		row.DisplayName,
		row.Email,
		row.IsBot,
		row.Deleted,
		row.Profile,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func accountFromRow(
	id string,
	principalType string,
	name string,
	realName string,
	displayName string,
	email string,
	isBot bool,
	deleted bool,
	profileJSON []byte,
	createdAt, updatedAt time.Time,
) (*domain.Account, error) {
	var profile domain.UserProfile
	if len(profileJSON) > 0 {
		if err := json.Unmarshal(profileJSON, &profile); err != nil {
			return nil, fmt.Errorf("decode account profile: %w", err)
		}
	}
	account := &domain.Account{
		ID:            id,
		PrincipalType: domain.PrincipalType(principalType),
		Name:          name,
		RealName:      realName,
		DisplayName:   displayName,
		Email:         email,
		IsBot:         isBot,
		Deleted:       deleted,
		Profile:       profile,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
	return account, nil
}
