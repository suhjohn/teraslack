package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type rowScanner interface {
	Scan(dest ...any) error
}

// WorkspaceRepo implements repository.WorkspaceRepository using raw SQL.
type WorkspaceRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

// NewWorkspaceRepo creates a new WorkspaceRepo.
func NewWorkspaceRepo(db DBTX) *WorkspaceRepo {
	return &WorkspaceRepo{q: sqlcgen.New(db), db: db}
}

// WithTx returns a new WorkspaceRepo bound to tx.
func (r *WorkspaceRepo) WithTx(tx pgx.Tx) repository.WorkspaceRepository {
	return &WorkspaceRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *WorkspaceRepo) Create(ctx context.Context, params domain.CreateWorkspaceParams) (*domain.Workspace, error) {
	id := generateID("T")
	discoverability := string(params.Discoverability)
	if discoverability == "" {
		discoverability = string(domain.WorkspaceDiscoverabilityInviteOnly)
	}
	defaultChannels := params.DefaultChannels
	if defaultChannels == nil {
		defaultChannels = []string{}
	}

	preferences := []byte("{}")
	if len(params.Preferences) > 0 {
		preferences = params.Preferences
	}
	billing := params.Billing
	if billing.Plan == "" {
		billing.Plan = "free"
	}
	if billing.Status == "" {
		billing.Status = "active"
	}

	row, err := r.q.CreateWorkspace(ctx, sqlcgen.CreateWorkspaceParams{
		ID:                id,
		Name:              params.Name,
		Domain:            params.Domain,
		EmailDomain:       params.EmailDomain,
		Description:       params.Description,
		IconImageOriginal: params.Icon.ImageOriginal,
		IconImage34:       params.Icon.Image34,
		IconImage44:       params.Icon.Image44,
		Discoverability:   discoverability,
		DefaultChannels:   defaultChannels,
		Preferences:       preferences,
		BillingPlan:       billing.Plan,
		BillingStatus:     billing.Status,
		BillingEmail:      billing.BillingEmail,
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return workspaceFromSQLC(row)
}

func (r *WorkspaceRepo) Get(ctx context.Context, id string) (*domain.Workspace, error) {
	row, err := r.q.GetWorkspace(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return workspaceFromSQLC(row)
}

func (r *WorkspaceRepo) List(ctx context.Context) ([]domain.Workspace, error) {
	rows, err := r.q.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	workspaces := make([]domain.Workspace, 0)
	for _, row := range rows {
		ws, err := workspaceFromSQLC(row)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, *ws)
	}
	return workspaces, nil
}

func (r *WorkspaceRepo) Update(ctx context.Context, id string, params domain.UpdateWorkspaceParams) (*domain.Workspace, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	if params.Name != nil {
		name = *params.Name
	}
	domainName := existing.Domain
	if params.Domain != nil {
		domainName = *params.Domain
	}
	emailDomain := existing.EmailDomain
	if params.EmailDomain != nil {
		emailDomain = *params.EmailDomain
	}
	description := existing.Description
	if params.Description != nil {
		description = *params.Description
	}
	icon := existing.Icon
	if params.Icon != nil {
		icon = *params.Icon
	}
	discoverability := string(existing.Discoverability)
	if params.Discoverability != nil {
		discoverability = string(*params.Discoverability)
	}
	defaultChannels := existing.DefaultChannels
	if params.DefaultChannels != nil {
		defaultChannels = *params.DefaultChannels
	}
	preferences := existing.Preferences
	if len(params.Preferences) > 0 {
		preferences = params.Preferences
	}
	billing := existing.Billing
	if params.Billing != nil {
		billing = *params.Billing
	}
	if billing.Plan == "" {
		billing.Plan = "free"
	}
	if billing.Status == "" {
		billing.Status = "active"
	}

	preferencesJSON := []byte("{}")
	if len(preferences) > 0 {
		preferencesJSON = preferences
	}

	row, err := r.q.UpdateWorkspace(ctx, sqlcgen.UpdateWorkspaceParams{
		ID:                id,
		Name:              name,
		Domain:            domainName,
		EmailDomain:       emailDomain,
		Description:       description,
		IconImageOriginal: icon.ImageOriginal,
		IconImage34:       icon.Image34,
		IconImage44:       icon.Image44,
		Discoverability:   discoverability,
		DefaultChannels:   defaultChannels,
		Preferences:       preferencesJSON,
		BillingPlan:       billing.Plan,
		BillingStatus:     billing.Status,
		BillingEmail:      billing.BillingEmail,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update workspace: %w", err)
	}
	return workspaceFromSQLC(row)
}

func (r *WorkspaceRepo) ListAdmins(ctx context.Context, workspaceID string) ([]domain.User, error) {
	rows, err := r.q.ListWorkspaceAdmins(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list users by role: %w", err)
	}
	return workspaceUsersFromRows(rows)
}

func (r *WorkspaceRepo) ListOwners(ctx context.Context, workspaceID string) ([]domain.User, error) {
	rows, err := r.q.ListWorkspaceOwners(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list users by role: %w", err)
	}
	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		user, err := userFieldsToDomain(userFields{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, RealName: row.RealName,
			DisplayName: row.DisplayName, Email: row.Email, PrincipalType: row.PrincipalType,
			OwnerID: row.OwnerID, AccountType: row.AccountType, IsBot: row.IsBot, Deleted: row.Deleted, Profile: row.Profile,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *user)
	}
	return users, nil
}

func (r *WorkspaceRepo) ListBillableInfo(ctx context.Context, workspaceID string) ([]domain.WorkspaceBillableInfo, error) {
	rows, err := r.q.ListWorkspaceBillableInfo(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list billable info: %w", err)
	}

	info := make([]domain.WorkspaceBillableInfo, 0)
	for _, row := range rows {
		info = append(info, domain.WorkspaceBillableInfo{
			UserID:        row.ID,
			BillingActive: row.BillingActive.Bool,
		})
	}
	return info, nil
}

func (r *WorkspaceRepo) ListAccessLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceAccessLog, error) {
	limit = clampWorkspaceLogLimit(limit)

	rows, err := r.q.ListWorkspaceAccessLogs(ctx, sqlcgen.ListWorkspaceAccessLogsParams{
		WorkspaceID: workspaceID,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list access logs: %w", err)
	}

	logs := make([]domain.WorkspaceAccessLog, 0)
	for _, row := range rows {
		logs = append(logs, domain.WorkspaceAccessLog{
			UserID:    row.ActorID,
			Username:  row.Username,
			EventType: row.EventType,
			DateFirst: tsToTime(row.DateFirst),
			DateLast:  tsToTime(row.DateLast),
		})
	}
	return logs, nil
}

func (r *WorkspaceRepo) ListIntegrationLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceIntegrationLog, error) {
	limit = clampWorkspaceLogLimit(limit)

	rows, err := r.q.ListWorkspaceIntegrationLogs(ctx, sqlcgen.ListWorkspaceIntegrationLogsParams{
		WorkspaceID: workspaceID,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list integration logs: %w", err)
	}

	logs := make([]domain.WorkspaceIntegrationLog, 0)
	for _, row := range rows {
		logs = append(logs, domain.WorkspaceIntegrationLog{
			AppID:    row.AggregateID,
			AppType:  row.AppType.(string),
			AppName:  row.AppName.(string),
			UserID:   row.ActorID,
			UserName: row.UserName,
			Action:   row.EventType,
			Date:     row.CreatedAt,
		})
	}
	return logs, nil
}

func (r *WorkspaceRepo) ListExternalWorkspaces(ctx context.Context, workspaceID string) ([]domain.ExternalWorkspace, error) {
	rows, err := r.q.ListWorkspaceExternalWorkspaces(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list external workspaces: %w", err)
	}

	workspaces := make([]domain.ExternalWorkspace, 0)
	for _, row := range rows {
		workspaces = append(workspaces, domain.ExternalWorkspace{
			ID:                  row.ID,
			ExternalWorkspaceID: row.ExternalWorkspaceID,
			Name:                row.ExternalWorkspaceName,
			ConnectionType:      row.ConnectionType,
			Connected:           row.Connected,
			CreatedAt:           row.CreatedAt,
			DisconnectedAt:      row.DisconnectedAt,
		})
	}
	return workspaces, nil
}

func (r *WorkspaceRepo) CreateExternalWorkspace(ctx context.Context, params domain.CreateExternalWorkspaceParams) (*domain.ExternalWorkspace, error) {
	if params.ConnectionType == "" {
		params.ConnectionType = "slack_connect"
	}
	row, err := r.q.CreateWorkspaceExternalWorkspace(ctx, sqlcgen.CreateWorkspaceExternalWorkspaceParams{
		ID:                  generateID("EW"),
		WorkspaceID:         params.WorkspaceID,
		ExternalWorkspaceID: params.ExternalWorkspaceID,
		ExternalWorkspaceName: params.Name,
		ConnectionType:      params.ConnectionType,
	})
	if err != nil {
		return nil, fmt.Errorf("create external workspace: %w", err)
	}
	return &domain.ExternalWorkspace{
		ID:                  row.ID,
		ExternalWorkspaceID: row.ExternalWorkspaceID,
		Name:                row.ExternalWorkspaceName,
		ConnectionType:      row.ConnectionType,
		Connected:           row.Connected,
		CreatedAt:           row.CreatedAt,
		DisconnectedAt:      row.DisconnectedAt,
	}, nil
}

func (r *WorkspaceRepo) GetExternalWorkspace(ctx context.Context, workspaceID, externalWorkspaceID string) (*domain.ExternalWorkspace, error) {
	row, err := r.q.GetWorkspaceExternalWorkspace(ctx, sqlcgen.GetWorkspaceExternalWorkspaceParams{
		WorkspaceID:         workspaceID,
		ExternalWorkspaceID: externalWorkspaceID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get external workspace: %w", err)
	}
	return &domain.ExternalWorkspace{
		ID:                  row.ID,
		ExternalWorkspaceID: row.ExternalWorkspaceID,
		Name:                row.ExternalWorkspaceName,
		ConnectionType:      row.ConnectionType,
		Connected:           row.Connected,
		CreatedAt:           row.CreatedAt,
		DisconnectedAt:      row.DisconnectedAt,
	}, nil
}

func (r *WorkspaceRepo) DisconnectExternalWorkspace(ctx context.Context, workspaceID, externalWorkspaceID string) error {
	rowsAffected, err := r.q.DisconnectWorkspaceExternalWorkspace(ctx, sqlcgen.DisconnectWorkspaceExternalWorkspaceParams{
		WorkspaceID:         workspaceID,
		ExternalWorkspaceID: externalWorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("disconnect external workspace: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanUser(row rowScanner) (*domain.User, error) {
	var fields userFields
	if err := row.Scan(
		&fields.ID,
		&fields.WorkspaceID,
		&fields.Name,
		&fields.RealName,
		&fields.DisplayName,
		&fields.Email,
		&fields.PrincipalType,
		&fields.OwnerID,
		&fields.IsBot,
		&fields.AccountType,
		&fields.Deleted,
		&fields.Profile,
		&fields.CreatedAt,
		&fields.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return userFieldsToDomain(fields)
}

func workspaceFromSQLC(row sqlcgen.Workspace) (*domain.Workspace, error) {
	ws := &domain.Workspace{
		ID:              row.ID,
		Name:            row.Name,
		Domain:          row.Domain,
		EmailDomain:     row.EmailDomain,
		Description:     row.Description,
		Discoverability: domain.WorkspaceDiscoverability(row.Discoverability),
		DefaultChannels: row.DefaultChannels,
		Billing: domain.WorkspaceBilling{
			Plan:         row.BillingPlan,
			Status:       row.BillingStatus,
			BillingEmail: row.BillingEmail,
		},
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	ws.Icon.ImageOriginal = row.IconImageOriginal
	ws.Icon.Image34 = row.IconImage34
	ws.Icon.Image44 = row.IconImage44
	if len(row.Preferences) == 0 {
		ws.Preferences = json.RawMessage("{}")
	} else {
		ws.Preferences = json.RawMessage(row.Preferences)
	}
	if ws.DefaultChannels == nil {
		ws.DefaultChannels = []string{}
	}
	return ws, nil
}

func workspaceUsersFromRows(rows []sqlcgen.ListWorkspaceAdminsRow) ([]domain.User, error) {
	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		user, err := userFieldsToDomain(userFields{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, RealName: row.RealName,
			DisplayName: row.DisplayName, Email: row.Email, PrincipalType: row.PrincipalType,
			OwnerID: row.OwnerID, AccountType: row.AccountType, IsBot: row.IsBot, Deleted: row.Deleted, Profile: row.Profile,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, nil
}

func clampWorkspaceLogLimit(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 100
	}
	return limit
}
