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
)

type rowScanner interface {
	Scan(dest ...any) error
}

// WorkspaceRepo implements repository.WorkspaceRepository using raw SQL.
type WorkspaceRepo struct {
	db DBTX
}

// NewWorkspaceRepo creates a new WorkspaceRepo.
func NewWorkspaceRepo(db DBTX) *WorkspaceRepo {
	return &WorkspaceRepo{db: db}
}

// WithTx returns a new WorkspaceRepo bound to tx.
func (r *WorkspaceRepo) WithTx(tx pgx.Tx) repository.WorkspaceRepository {
	return &WorkspaceRepo{db: tx}
}

func (r *WorkspaceRepo) Create(ctx context.Context, params domain.CreateWorkspaceParams) (*domain.Workspace, error) {
	id := generateID("T")
	discoverability := string(params.Discoverability)
	if discoverability == "" {
		discoverability = string(domain.WorkspaceDiscoverabilityInviteOnly)
	}

	preferences := []byte("{}")
	if len(params.Preferences) > 0 {
		preferences = params.Preferences
	}
	profileFields, err := json.Marshal(params.ProfileFields)
	if err != nil {
		return nil, fmt.Errorf("marshal profile fields: %w", err)
	}
	billing := params.Billing
	if billing.Plan == "" {
		billing.Plan = "free"
	}
	if billing.Status == "" {
		billing.Status = "active"
	}

	row := r.db.QueryRow(ctx, `
		INSERT INTO workspaces (
			id, name, domain, email_domain, description,
			icon_image_original, icon_image_34, icon_image_44,
			discoverability, default_channels, preferences, profile_fields,
			billing_plan, billing_status, billing_email
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, name, domain, email_domain, description,
			icon_image_original, icon_image_34, icon_image_44,
			discoverability, default_channels, preferences, profile_fields,
			billing_plan, billing_status, billing_email, created_at, updated_at
	`,
		id, params.Name, params.Domain, params.EmailDomain, params.Description,
		params.Icon.ImageOriginal, params.Icon.Image34, params.Icon.Image44,
		discoverability, params.DefaultChannels, preferences, profileFields,
		billing.Plan, billing.Status, billing.BillingEmail,
	)
	ws, err := scanWorkspace(row)
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return ws, nil
}

func (r *WorkspaceRepo) Get(ctx context.Context, id string) (*domain.Workspace, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, name, domain, email_domain, description,
			icon_image_original, icon_image_34, icon_image_44,
			discoverability, default_channels, preferences, profile_fields,
			billing_plan, billing_status, billing_email, created_at, updated_at
		FROM workspaces
		WHERE id = $1
	`, id)
	ws, err := scanWorkspace(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return ws, nil
}

func (r *WorkspaceRepo) List(ctx context.Context) ([]domain.Workspace, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, domain, email_domain, description,
			icon_image_original, icon_image_34, icon_image_44,
			discoverability, default_channels, preferences, profile_fields,
			billing_plan, billing_status, billing_email, created_at, updated_at
		FROM workspaces
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	workspaces := make([]domain.Workspace, 0)
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, *ws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
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
	profileFields := existing.ProfileFields
	if params.ProfileFields != nil {
		profileFields = *params.ProfileFields
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

	profileFieldsJSON, err := json.Marshal(profileFields)
	if err != nil {
		return nil, fmt.Errorf("marshal profile fields: %w", err)
	}
	preferencesJSON := []byte("{}")
	if len(preferences) > 0 {
		preferencesJSON = preferences
	}

	row := r.db.QueryRow(ctx, `
		UPDATE workspaces
		SET name = $2,
			domain = $3,
			email_domain = $4,
			description = $5,
			icon_image_original = $6,
			icon_image_34 = $7,
			icon_image_44 = $8,
			discoverability = $9,
			default_channels = $10,
			preferences = $11,
			profile_fields = $12,
			billing_plan = $13,
			billing_status = $14,
			billing_email = $15
		WHERE id = $1
		RETURNING id, name, domain, email_domain, description,
			icon_image_original, icon_image_34, icon_image_44,
			discoverability, default_channels, preferences, profile_fields,
			billing_plan, billing_status, billing_email, created_at, updated_at
	`,
		id, name, domainName, emailDomain, description,
		icon.ImageOriginal, icon.Image34, icon.Image44,
		discoverability, defaultChannels, preferencesJSON, profileFieldsJSON,
		billing.Plan, billing.Status, billing.BillingEmail,
	)
	ws, err := scanWorkspace(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update workspace: %w", err)
	}
	return ws, nil
}

func (r *WorkspaceRepo) ListAdmins(ctx context.Context, workspaceID string) ([]domain.User, error) {
	return r.listUsersByRole(ctx, workspaceID, "account_type IN ('primary_admin', 'admin')")
}

func (r *WorkspaceRepo) ListOwners(ctx context.Context, workspaceID string) ([]domain.User, error) {
	return r.listUsersByRole(ctx, workspaceID, "account_type = 'primary_admin'")
}

func (r *WorkspaceRepo) ListBillableInfo(ctx context.Context, workspaceID string) ([]domain.WorkspaceBillableInfo, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, (NOT is_bot AND NOT deleted) AS billing_active
		FROM users
		WHERE team_id = $1
		ORDER BY id ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list billable info: %w", err)
	}
	defer rows.Close()

	info := make([]domain.WorkspaceBillableInfo, 0)
	for rows.Next() {
		var row domain.WorkspaceBillableInfo
		if err := rows.Scan(&row.UserID, &row.BillingActive); err != nil {
			return nil, fmt.Errorf("scan billable info: %w", err)
		}
		info = append(info, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billable info: %w", err)
	}
	return info, nil
}

func (r *WorkspaceRepo) ListAccessLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceAccessLog, error) {
	limit = clampWorkspaceLogLimit(limit)

	rows, err := r.db.Query(ctx, `
		SELECT se.actor_id,
			COALESCE(NULLIF(u.name, ''), se.actor_id) AS username,
			se.event_type,
			MIN(se.created_at) AS date_first,
			MAX(se.created_at) AS date_last
		FROM internal_events se
		LEFT JOIN users u ON u.id = se.actor_id
		WHERE se.team_id = $1
			AND se.actor_id <> ''
			AND se.aggregate_type IN ('token', 'api_key')
		GROUP BY se.actor_id, COALESCE(NULLIF(u.name, ''), se.actor_id), se.event_type
		ORDER BY MAX(se.created_at) DESC
		LIMIT $2
	`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list access logs: %w", err)
	}
	defer rows.Close()

	logs := make([]domain.WorkspaceAccessLog, 0)
	for rows.Next() {
		var log domain.WorkspaceAccessLog
		if err := rows.Scan(&log.UserID, &log.Username, &log.EventType, &log.DateFirst, &log.DateLast); err != nil {
			return nil, fmt.Errorf("scan access log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access logs: %w", err)
	}
	return logs, nil
}

func (r *WorkspaceRepo) ListIntegrationLogs(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceIntegrationLog, error) {
	limit = clampWorkspaceLogLimit(limit)

	rows, err := r.db.Query(ctx, `
		SELECT se.aggregate_id,
			CASE
				WHEN se.aggregate_type = 'event_subscription' THEN 'webhook'
				ELSE se.aggregate_type
			END AS app_type,
			CASE
				WHEN se.aggregate_type = 'event_subscription' THEN 'event_subscription'
				ELSE se.aggregate_type
			END AS app_name,
			se.actor_id,
			COALESCE(NULLIF(u.name, ''), se.actor_id) AS user_name,
			se.event_type,
			se.created_at
		FROM internal_events se
		LEFT JOIN users u ON u.id = se.actor_id
		WHERE se.team_id = $1
			AND se.aggregate_type IN ('api_key', 'event_subscription')
		ORDER BY se.created_at DESC
		LIMIT $2
	`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list integration logs: %w", err)
	}
	defer rows.Close()

	logs := make([]domain.WorkspaceIntegrationLog, 0)
	for rows.Next() {
		var log domain.WorkspaceIntegrationLog
		if err := rows.Scan(&log.AppID, &log.AppType, &log.AppName, &log.UserID, &log.UserName, &log.Action, &log.Date); err != nil {
			return nil, fmt.Errorf("scan integration log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate integration logs: %w", err)
	}
	return logs, nil
}

func (r *WorkspaceRepo) ListExternalTeams(ctx context.Context, workspaceID string) ([]domain.ExternalTeam, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, external_team_id, external_team_name, connection_type,
			connected, created_at, disconnected_at
		FROM workspace_external_teams
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list external teams: %w", err)
	}
	defer rows.Close()

	teams := make([]domain.ExternalTeam, 0)
	for rows.Next() {
		var team domain.ExternalTeam
		var disconnectedAt *time.Time
		if err := rows.Scan(
			&team.ID,
			&team.ExternalTeamID,
			&team.Name,
			&team.ConnectionType,
			&team.Connected,
			&team.CreatedAt,
			&disconnectedAt,
		); err != nil {
			return nil, fmt.Errorf("scan external team: %w", err)
		}
		team.DisconnectedAt = disconnectedAt
		teams = append(teams, team)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external teams: %w", err)
	}
	return teams, nil
}

func (r *WorkspaceRepo) DisconnectExternalTeam(ctx context.Context, workspaceID, externalTeamID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE workspace_external_teams
		SET connected = FALSE, disconnected_at = NOW()
		WHERE workspace_id = $1 AND external_team_id = $2 AND connected = TRUE
	`, workspaceID, externalTeamID)
	if err != nil {
		return fmt.Errorf("disconnect external team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *WorkspaceRepo) listUsersByRole(ctx context.Context, workspaceID, where string) ([]domain.User, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, team_id, name, real_name, display_name, email,
			principal_type, owner_id, is_bot, account_type,
			deleted, profile, created_at, updated_at
		FROM users
		WHERE team_id = $1 AND deleted = FALSE AND `+where+`
		ORDER BY name ASC, id ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list users by role: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users by role: %w", err)
	}
	return users, nil
}

func scanWorkspace(row rowScanner) (*domain.Workspace, error) {
	var ws domain.Workspace
	var discoverability string
	var preferences []byte
	var profileFields []byte
	if err := row.Scan(
		&ws.ID,
		&ws.Name,
		&ws.Domain,
		&ws.EmailDomain,
		&ws.Description,
		&ws.Icon.ImageOriginal,
		&ws.Icon.Image34,
		&ws.Icon.Image44,
		&discoverability,
		&ws.DefaultChannels,
		&preferences,
		&profileFields,
		&ws.Billing.Plan,
		&ws.Billing.Status,
		&ws.Billing.BillingEmail,
		&ws.CreatedAt,
		&ws.UpdatedAt,
	); err != nil {
		return nil, err
	}
	ws.Discoverability = domain.WorkspaceDiscoverability(discoverability)
	if len(preferences) == 0 {
		ws.Preferences = json.RawMessage("{}")
	} else {
		ws.Preferences = json.RawMessage(preferences)
	}
	if len(profileFields) > 0 {
		if err := json.Unmarshal(profileFields, &ws.ProfileFields); err != nil {
			return nil, fmt.Errorf("unmarshal profile fields: %w", err)
		}
	}
	if ws.DefaultChannels == nil {
		ws.DefaultChannels = []string{}
	}
	if ws.ProfileFields == nil {
		ws.ProfileFields = []domain.WorkspaceProfileField{}
	}
	return &ws, nil
}

func scanUser(row rowScanner) (*domain.User, error) {
	var fields userFields
	if err := row.Scan(
		&fields.ID,
		&fields.TeamID,
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

func clampWorkspaceLogLimit(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 100
	}
	return limit
}
