package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/mcpoauth"
	"github.com/suhjohn/teraslack/internal/repository"
)

type MCPOAuthRepo struct {
	db DBTX
}

func NewMCPOAuthRepo(db DBTX) *MCPOAuthRepo {
	return &MCPOAuthRepo{db: db}
}

func (r *MCPOAuthRepo) WithTx(tx pgx.Tx) repository.MCPOAuthRepository {
	return &MCPOAuthRepo{db: tx}
}

func (r *MCPOAuthRepo) CreateAuthorizationCode(ctx context.Context, params domain.CreateMCPOAuthAuthorizationCodeParams) (*domain.MCPOAuthAuthorizationCode, string, error) {
	raw, hash, err := mcpoauth.RandomSecret("mcpcode")
	if err != nil {
		return nil, "", err
	}

	row := r.db.QueryRow(ctx, `
		INSERT INTO oauth_authorization_codes (
			id, code_hash, client_id, client_name, redirect_uri, workspace_id, user_id,
			scope, resource, code_challenge, code_challenge_method, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, code_hash, client_id, client_name, redirect_uri, workspace_id, user_id,
			scope, resource, code_challenge, code_challenge_method, expires_at, used_at, created_at, updated_at
	`,
		generateID("OAC"),
		hash,
		params.ClientID,
		params.ClientName,
		params.RedirectURI,
		params.WorkspaceID,
		params.UserID,
		params.Scopes,
		params.Resource,
		params.CodeChallenge,
		params.CodeChallengeMethod,
		params.ExpiresAt.UTC(),
	)

	code, err := scanAuthorizationCode(row)
	if err != nil {
		return nil, "", err
	}
	return code, raw, nil
}

func (r *MCPOAuthRepo) GetAuthorizationCodeByHash(ctx context.Context, codeHash string) (*domain.MCPOAuthAuthorizationCode, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, code_hash, client_id, client_name, redirect_uri, workspace_id, user_id,
			scope, resource, code_challenge, code_challenge_method, expires_at, used_at, created_at, updated_at
		FROM oauth_authorization_codes
		WHERE code_hash = $1
	`, codeHash)
	code, err := scanAuthorizationCode(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, err
	}
	return code, nil
}

func (r *MCPOAuthRepo) MarkAuthorizationCodeUsed(ctx context.Context, id string, usedAt time.Time) error {
	tag, err := r.db.Exec(ctx, `UPDATE oauth_authorization_codes SET used_at = $2, updated_at = now() WHERE id = $1`, id, usedAt.UTC())
	if err != nil {
		return fmt.Errorf("mark auth code used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *MCPOAuthRepo) CreateRefreshToken(ctx context.Context, params domain.CreateMCPOAuthRefreshTokenParams) (*domain.MCPOAuthRefreshToken, string, error) {
	raw, hash, err := mcpoauth.RandomSecret("mcprt")
	if err != nil {
		return nil, "", err
	}

	row := r.db.QueryRow(ctx, `
		INSERT INTO oauth_refresh_tokens (
			id, token_hash, client_id, client_name, workspace_id, user_id, scope, resource, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, token_hash, client_id, client_name, workspace_id, user_id, scope, resource,
			expires_at, revoked_at, rotated_to_id, created_at, updated_at
	`,
		generateID("ORT"),
		hash,
		params.ClientID,
		params.ClientName,
		params.WorkspaceID,
		params.UserID,
		params.Scopes,
		params.Resource,
		params.ExpiresAt.UTC(),
	)

	token, err := scanRefreshToken(row)
	if err != nil {
		return nil, "", err
	}
	return token, raw, nil
}

func (r *MCPOAuthRepo) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.MCPOAuthRefreshToken, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, token_hash, client_id, client_name, workspace_id, user_id, scope, resource,
			expires_at, revoked_at, rotated_to_id, created_at, updated_at
		FROM oauth_refresh_tokens
		WHERE token_hash = $1
	`, tokenHash)
	token, err := scanRefreshToken(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidAuth
		}
		return nil, err
	}
	return token, nil
}

func (r *MCPOAuthRepo) RotateRefreshToken(ctx context.Context, oldID, newID string, revokedAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE oauth_refresh_tokens
		SET rotated_to_id = $2, revoked_at = $3, updated_at = now()
		WHERE id = $1
	`, oldID, newID, revokedAt.UTC())
	if err != nil {
		return fmt.Errorf("rotate refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *MCPOAuthRepo) RevokeRefreshToken(ctx context.Context, id string, revokedAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE oauth_refresh_tokens
		SET revoked_at = $2, updated_at = now()
		WHERE id = $1
	`, id, revokedAt.UTC())
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanAuthorizationCode(row pgx.Row) (*domain.MCPOAuthAuthorizationCode, error) {
	var (
		code   domain.MCPOAuthAuthorizationCode
		scopes []string
		usedAt *time.Time
	)
	if err := row.Scan(
		&code.ID,
		&code.CodeHash,
		&code.ClientID,
		&code.ClientName,
		&code.RedirectURI,
		&code.WorkspaceID,
		&code.UserID,
		&scopes,
		&code.Resource,
		&code.CodeChallenge,
		&code.CodeChallengeMethod,
		&code.ExpiresAt,
		&usedAt,
		&code.CreatedAt,
		&code.UpdatedAt,
	); err != nil {
		return nil, err
	}
	code.Scopes = scopes
	code.UsedAt = usedAt
	return &code, nil
}

func scanRefreshToken(row pgx.Row) (*domain.MCPOAuthRefreshToken, error) {
	var (
		token       domain.MCPOAuthRefreshToken
		scopes      []string
		revoked     *time.Time
		rotatedToID *string
	)
	if err := row.Scan(
		&token.ID,
		&token.TokenHash,
		&token.ClientID,
		&token.ClientName,
		&token.WorkspaceID,
		&token.UserID,
		&scopes,
		&token.Resource,
		&token.ExpiresAt,
		&revoked,
		&rotatedToID,
		&token.CreatedAt,
		&token.UpdatedAt,
	); err != nil {
		return nil, err
	}
	token.Scopes = scopes
	token.RevokedAt = revoked
	if rotatedToID != nil {
		token.RotatedToID = *rotatedToID
	}
	return &token, nil
}
