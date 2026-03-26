package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type AuthorizationAuditRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewAuthorizationAuditRepo(db DBTX) *AuthorizationAuditRepo {
	return &AuthorizationAuditRepo{q: sqlcgen.New(db), db: db}
}

func (r *AuthorizationAuditRepo) WithTx(tx pgx.Tx) repository.AuthorizationAuditRepository {
	return &AuthorizationAuditRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *AuthorizationAuditRepo) Create(ctx context.Context, params domain.CreateAuthorizationAuditLogParams) (*domain.AuthorizationAuditLog, error) {
	row, err := r.q.CreateAuthorizationAuditLog(ctx, sqlcgen.CreateAuthorizationAuditLogParams{
		ID:         generateID("AAL"),
		WorkspaceID:     params.WorkspaceID,
		Column3:    params.ActorID,
		Column4:    params.APIKeyID,
		Column5:    params.OnBehalfOf,
		Action:     params.Action,
		Resource:   params.Resource,
		ResourceID: params.ResourceID,
		Metadata:   params.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("insert authorization audit log: %w", err)
	}
	return &domain.AuthorizationAuditLog{
		ID:         row.ID,
		WorkspaceID: row.WorkspaceID,
		ActorID:    row.ActorID,
		APIKeyID:   row.ApiKeyID,
		OnBehalfOf: row.OnBehalfOf,
		Action:     row.Action,
		Resource:   row.Resource,
		ResourceID: row.ResourceID,
		Metadata:   row.Metadata,
		CreatedAt:  tsToTime(row.CreatedAt),
	}, nil
}

func (r *AuthorizationAuditRepo) List(ctx context.Context, params domain.ListAuthorizationAuditLogsParams) ([]domain.AuthorizationAuditLog, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.q.ListAuthorizationAuditLogs(ctx, sqlcgen.ListAuthorizationAuditLogsParams{
		WorkspaceID: params.WorkspaceID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list authorization audit log: %w", err)
	}

	logs := []domain.AuthorizationAuditLog{}
	for _, row := range rows {
		logs = append(logs, domain.AuthorizationAuditLog{
			ID:         row.ID,
			WorkspaceID: row.WorkspaceID,
			ActorID:    row.ActorID,
			APIKeyID:   row.ApiKeyID,
			OnBehalfOf: row.OnBehalfOf,
			Action:     row.Action,
			Resource:   row.Resource,
			ResourceID: row.ResourceID,
			Metadata:   row.Metadata,
			CreatedAt:  tsToTime(row.CreatedAt),
		})
	}
	return logs, nil
}
