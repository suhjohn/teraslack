package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type AuthorizationAuditRepo struct {
	db DBTX
}

func NewAuthorizationAuditRepo(db DBTX) *AuthorizationAuditRepo {
	return &AuthorizationAuditRepo{db: db}
}

func (r *AuthorizationAuditRepo) WithTx(tx pgx.Tx) repository.AuthorizationAuditRepository {
	return &AuthorizationAuditRepo{db: tx}
}

func (r *AuthorizationAuditRepo) Create(ctx context.Context, params domain.CreateAuthorizationAuditLogParams) (*domain.AuthorizationAuditLog, error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO authorization_audit_log (
			id, team_id, actor_id, api_key_id, on_behalf_of, action, resource, resource_id, metadata
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9)
		RETURNING id, team_id, COALESCE(actor_id, ''), COALESCE(api_key_id, ''), COALESCE(on_behalf_of, ''), action, resource, resource_id, metadata, created_at
	`, generateID("AAL"), params.TeamID, params.ActorID, params.APIKeyID, params.OnBehalfOf, params.Action, params.Resource, params.ResourceID, params.Metadata)

	var log domain.AuthorizationAuditLog
	if err := row.Scan(
		&log.ID,
		&log.TeamID,
		&log.ActorID,
		&log.APIKeyID,
		&log.OnBehalfOf,
		&log.Action,
		&log.Resource,
		&log.ResourceID,
		&log.Metadata,
		&log.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert authorization audit log: %w", err)
	}
	return &log, nil
}

func (r *AuthorizationAuditRepo) List(ctx context.Context, params domain.ListAuthorizationAuditLogsParams) ([]domain.AuthorizationAuditLog, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, team_id, COALESCE(actor_id, ''), COALESCE(api_key_id, ''), COALESCE(on_behalf_of, ''), action, resource, resource_id, metadata, created_at
		FROM authorization_audit_log
		WHERE team_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, params.TeamID, limit)
	if err != nil {
		return nil, fmt.Errorf("list authorization audit log: %w", err)
	}
	defer rows.Close()

	logs := []domain.AuthorizationAuditLog{}
	for rows.Next() {
		var log domain.AuthorizationAuditLog
		if err := rows.Scan(
			&log.ID,
			&log.TeamID,
			&log.ActorID,
			&log.APIKeyID,
			&log.OnBehalfOf,
			&log.Action,
			&log.Resource,
			&log.ResourceID,
			&log.Metadata,
			&log.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan authorization audit log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorization audit log: %w", err)
	}
	return logs, nil
}
