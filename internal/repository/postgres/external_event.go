package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type ExternalEventRepo struct {
	db DBTX
}

func NewExternalEventRepo(db DBTX) *ExternalEventRepo {
	return &ExternalEventRepo{db: db}
}

func (r *ExternalEventRepo) WithTx(tx pgx.Tx) repository.ExternalEventRepository {
	return &ExternalEventRepo{db: tx}
}

func (r *ExternalEventRepo) Insert(ctx context.Context, event domain.ExternalEvent) (*domain.ExternalEvent, error) {
	sourceIDs, err := json.Marshal(event.SourceInternalEventIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal source ids: %w", err)
	}

	var inserted domain.ExternalEvent
	query := `
		INSERT INTO external_events (
			team_id, type, resource_type, resource_id, occurred_at, payload,
			source_internal_event_id, source_internal_event_ids, dedupe_key
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (team_id, dedupe_key) DO UPDATE SET
			team_id = external_events.team_id
		RETURNING
			id, team_id, type, resource_type, resource_id, occurred_at, payload,
			source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
	`
	var sourceID sql.NullInt64
	var sourceIDsRaw []byte
	if err := r.db.QueryRow(
		ctx,
		query,
		event.TeamID,
		event.Type,
		event.ResourceType,
		event.ResourceID,
		event.OccurredAt,
		event.Payload,
		event.SourceInternalEventID,
		sourceIDs,
		event.DedupeKey,
	).Scan(
		&inserted.ID,
		&inserted.TeamID,
		&inserted.Type,
		&inserted.ResourceType,
		&inserted.ResourceID,
		&inserted.OccurredAt,
		&inserted.Payload,
		&sourceID,
		&sourceIDsRaw,
		&inserted.DedupeKey,
		&inserted.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert external event: %w", err)
	}
	if sourceID.Valid {
		value := sourceID.Int64
		inserted.SourceInternalEventID = &value
	}
	if len(sourceIDsRaw) > 0 {
		if err := json.Unmarshal(sourceIDsRaw, &inserted.SourceInternalEventIDs); err != nil {
			return nil, fmt.Errorf("decode source ids: %w", err)
		}
	}
	if inserted.SourceInternalEventIDs == nil {
		inserted.SourceInternalEventIDs = []int64{}
	}

	if err := r.insertFeedRow(ctx, inserted); err != nil {
		return nil, err
	}
	return &inserted, nil
}

func (r *ExternalEventRepo) RecordProjectionFailure(ctx context.Context, internalEventID int64, message string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO external_event_projection_failures (internal_event_id, error)
		VALUES ($1, $2)
		ON CONFLICT (internal_event_id) DO UPDATE SET
			error = EXCLUDED.error,
			created_at = NOW()
	`, internalEventID, message)
	if err != nil {
		return fmt.Errorf("record projection failure: %w", err)
	}
	return nil
}

func (r *ExternalEventRepo) GetSince(ctx context.Context, afterID int64, limit int) ([]domain.ExternalEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, team_id, type, resource_type, resource_id, occurred_at, payload,
		       source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
		FROM external_events
		WHERE id > $1
		ORDER BY id ASC
		LIMIT $2
	`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("get external events since: %w", err)
	}
	defer rows.Close()

	var out []domain.ExternalEvent
	for rows.Next() {
		event, scanErr := scanExternalEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *ExternalEventRepo) ListVisible(ctx context.Context, principal repository.ExternalEventPrincipal, params domain.ListExternalEventsParams) (*domain.CursorPage[domain.ExternalEvent], error) {
	if principal.TeamID == "" {
		return nil, fmt.Errorf("team_id: %w", domain.ErrInvalidArgument)
	}

	limit := params.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	afterID := params.AfterID
	args := []any{principal.TeamID, afterID}
	externalAccess, err := r.activeExternalAccessForPrincipal(ctx, principal.TeamID, principal.UserID)
	if err != nil {
		return nil, err
	}
	var query strings.Builder
	query.WriteString(`
		WITH visible_ids AS (
	`)
	query.WriteString(r.visibleIDsSubquery(principal, externalAccess, params, &args))
	query.WriteString(`
		)
		SELECT ee.id, ee.team_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload,
		       ee.source_internal_event_id, ee.source_internal_event_ids, ee.dedupe_key, ee.created_at
		FROM visible_ids vid
		JOIN external_events ee ON ee.id = vid.external_event_id
		WHERE ee.team_id = $1 AND ee.id > $2
	`)
	if params.Type != "" {
		args = append(args, params.Type)
		fmt.Fprintf(&query, " AND ee.type = $%d", len(args))
	}
	if params.ResourceType != "" {
		args = append(args, params.ResourceType)
		fmt.Fprintf(&query, " AND ee.resource_type = $%d", len(args))
	}
	if params.ResourceID != "" {
		args = append(args, params.ResourceID)
		fmt.Fprintf(&query, " AND ee.resource_id = $%d", len(args))
	}
	query.WriteString(" ORDER BY ee.id ASC")
	args = append(args, limit+1)
	fmt.Fprintf(&query, " LIMIT $%d", len(args))

	rows, err := r.db.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list visible external events: %w", err)
	}
	defer rows.Close()

	items := make([]domain.ExternalEvent, 0, limit+1)
	for rows.Next() {
		event, scanErr := scanExternalEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &domain.CursorPage[domain.ExternalEvent]{}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
	} else {
		page.Items = items
	}
	if page.Items == nil {
		page.Items = []domain.ExternalEvent{}
	}
	return page, nil
}

func (r *ExternalEventRepo) Rebuild(ctx context.Context, events []domain.ExternalEvent) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	repo := r.WithTx(tx)
	if _, err := tx.Exec(ctx, `
		TRUNCATE usergroup_event_feed, user_event_feed, file_event_feed,
		         conversation_event_feed, team_event_feed, external_events
		         RESTART IDENTITY CASCADE
	`); err != nil {
		return fmt.Errorf("truncate external event tables: %w", err)
	}

	for _, event := range events {
		if _, err := repo.Insert(ctx, event); err != nil {
			return err
		}
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
}

func (r *ExternalEventRepo) RebuildFeeds(ctx context.Context) error {
	tx, ownTx, err := beginOwnedTx(ctx, r.db)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if ownTx {
		defer tx.Rollback(ctx)
	}

	if _, err := tx.Exec(ctx, `
		TRUNCATE usergroup_event_feed, user_event_feed, file_event_feed,
		         conversation_event_feed, team_event_feed RESTART IDENTITY
	`); err != nil {
		return fmt.Errorf("truncate feed tables: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, team_id, type, resource_type, resource_id, occurred_at, payload,
		       source_internal_event_id, source_internal_event_ids, dedupe_key, created_at
		FROM external_events
		ORDER BY id ASC
	`)
	if err != nil {
		return fmt.Errorf("query external events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		event, scanErr := scanExternalEvent(rows)
		if scanErr != nil {
			return scanErr
		}
		if err := (&ExternalEventRepo{db: tx}).insertFeedRow(ctx, event); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
}

func (r *ExternalEventRepo) insertFeedRow(ctx context.Context, event domain.ExternalEvent) error {
	var query string
	var args []any
	switch event.ResourceType {
	case domain.ResourceTypeTeam:
		query = `INSERT INTO team_event_feed (team_id, external_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		args = []any{event.ResourceID, event.ID}
	case domain.ResourceTypeConversation:
		query = `INSERT INTO conversation_event_feed (conversation_id, external_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		args = []any{event.ResourceID, event.ID}
	case domain.ResourceTypeFile:
		query = `INSERT INTO file_event_feed (file_id, external_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		args = []any{event.ResourceID, event.ID}
	case domain.ResourceTypeUser:
		query = `INSERT INTO user_event_feed (user_id, external_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		args = []any{event.ResourceID, event.ID}
	case domain.ResourceTypeUsergroup:
		query = `INSERT INTO usergroup_event_feed (usergroup_id, external_event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		args = []any{event.ResourceID, event.ID}
	default:
		return fmt.Errorf("unknown resource_type %q: %w", event.ResourceType, domain.ErrInvalidArgument)
	}

	if _, err := r.db.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert %s feed row: %w", event.ResourceType, err)
	}
	return nil
}

type externalAccessState struct {
	ID                  string
	AllowedCapabilities []string
}

func (r *ExternalEventRepo) visibleIDsSubquery(principal repository.ExternalEventPrincipal, externalAccess *externalAccessState, params domain.ListExternalEventsParams, args *[]any) string {
	if externalAccess != nil {
		return r.externalAccessVisibleIDsSubquery(externalAccess, params, args)
	}
	if !principalCanReadExternalResourceType(principal, params.ResourceType) {
		return `SELECT NULL::BIGINT AS external_event_id WHERE FALSE`
	}

	switch params.ResourceType {
	case domain.ResourceTypeTeam:
		if params.ResourceID != "" {
			*args = append(*args, params.ResourceID)
			return fmt.Sprintf(`
				SELECT external_event_id
				FROM team_event_feed
				WHERE team_id = $%d AND external_event_id > $2
			`, len(*args))
		}
		return `
			SELECT external_event_id
			FROM team_event_feed
			WHERE team_id = $1 AND external_event_id > $2
		`
	case domain.ResourceTypeConversation:
		return r.feedSubquery(args, "conversation_event_feed", "conversations", "conversation_id", "id", "team_id", params.ResourceID)
	case domain.ResourceTypeFile:
		return r.feedSubquery(args, "file_event_feed", "files", "file_id", "id", "team_id", params.ResourceID)
	case domain.ResourceTypeUser:
		return r.feedSubquery(args, "user_event_feed", "users", "user_id", "id", "team_id", params.ResourceID)
	case domain.ResourceTypeUsergroup:
		return r.feedSubquery(args, "usergroup_event_feed", "usergroups", "usergroup_id", "id", "team_id", params.ResourceID)
	default:
		subqueries := make([]string, 0, 5)
		if principalCanReadExternalResourceType(principal, domain.ResourceTypeTeam) {
			subqueries = append(subqueries, `SELECT external_event_id FROM team_event_feed WHERE team_id = $1 AND external_event_id > $2`)
		}
		if principalCanReadExternalResourceType(principal, domain.ResourceTypeConversation) {
			subqueries = append(subqueries, r.feedSubquery(args, "conversation_event_feed", "conversations", "conversation_id", "id", "team_id", ""))
		}
		if principalCanReadExternalResourceType(principal, domain.ResourceTypeFile) {
			subqueries = append(subqueries, r.feedSubquery(args, "file_event_feed", "files", "file_id", "id", "team_id", ""))
		}
		if principalCanReadExternalResourceType(principal, domain.ResourceTypeUser) {
			subqueries = append(subqueries, r.feedSubquery(args, "user_event_feed", "users", "user_id", "id", "team_id", ""))
		}
		if principalCanReadExternalResourceType(principal, domain.ResourceTypeUsergroup) {
			subqueries = append(subqueries, r.feedSubquery(args, "usergroup_event_feed", "usergroups", "usergroup_id", "id", "team_id", ""))
		}
		if len(subqueries) == 0 {
			return `SELECT NULL::BIGINT AS external_event_id WHERE FALSE`
		}
		return strings.Join(subqueries, "\nUNION\n")
	}
}

func (r *ExternalEventRepo) externalAccessVisibleIDsSubquery(access *externalAccessState, params domain.ListExternalEventsParams, args *[]any) string {
	resourceAllowed := func(resourceType string) bool {
		switch resourceType {
		case domain.ResourceTypeConversation:
			return hasPermission(access.AllowedCapabilities, domain.PermissionMessagesRead)
		case domain.ResourceTypeFile:
			return hasPermission(access.AllowedCapabilities, domain.PermissionFilesRead) ||
				hasPermission(access.AllowedCapabilities, domain.PermissionFilesWrite)
		default:
			return false
		}
	}

	switch params.ResourceType {
	case domain.ResourceTypeConversation:
		if !resourceAllowed(domain.ResourceTypeConversation) {
			return `SELECT NULL::BIGINT AS external_event_id WHERE FALSE`
		}
		*args = append(*args, access.ID)
		accessIDArg := len(*args)
		query := fmt.Sprintf(`
			SELECT f.external_event_id
			FROM conversation_event_feed f
			JOIN external_principal_conversation_assignments eca
			  ON eca.conversation_id = f.conversation_id
			WHERE eca.access_id = $%d AND f.external_event_id > $2
		`, accessIDArg)
		if params.ResourceID != "" {
			*args = append(*args, params.ResourceID)
			query += fmt.Sprintf(" AND f.conversation_id = $%d", len(*args))
		}
		return query
	case domain.ResourceTypeFile:
		if !resourceAllowed(domain.ResourceTypeFile) {
			return `SELECT NULL::BIGINT AS external_event_id WHERE FALSE`
		}
		*args = append(*args, access.ID)
		accessIDArg := len(*args)
		query := fmt.Sprintf(`
			SELECT DISTINCT f.external_event_id
			FROM file_event_feed f
			JOIN file_channels fc ON fc.file_id = f.file_id
			JOIN external_principal_conversation_assignments eca
			  ON eca.conversation_id = fc.channel_id
			WHERE eca.access_id = $%d AND f.external_event_id > $2
		`, accessIDArg)
		if params.ResourceID != "" {
			*args = append(*args, params.ResourceID)
			query += fmt.Sprintf(" AND f.file_id = $%d", len(*args))
		}
		return query
	case "", domain.ResourceTypeTeam, domain.ResourceTypeUser, domain.ResourceTypeUsergroup:
		subqueries := make([]string, 0, 2)
		if resourceAllowed(domain.ResourceTypeConversation) {
			subqueries = append(subqueries, r.externalAccessVisibleIDsSubquery(access, domain.ListExternalEventsParams{
				ResourceType: domain.ResourceTypeConversation,
				ResourceID:   "",
			}, args))
		}
		if resourceAllowed(domain.ResourceTypeFile) {
			subqueries = append(subqueries, r.externalAccessVisibleIDsSubquery(access, domain.ListExternalEventsParams{
				ResourceType: domain.ResourceTypeFile,
				ResourceID:   "",
			}, args))
		}
		if len(subqueries) == 0 {
			return `SELECT NULL::BIGINT AS external_event_id WHERE FALSE`
		}
		return strings.Join(subqueries, "\nUNION\n")
	default:
		return `SELECT NULL::BIGINT AS external_event_id WHERE FALSE`
	}
}

func principalCanReadExternalResourceType(principal repository.ExternalEventPrincipal, resourceType string) bool {
	if resourceType == "" {
		return true
	}
	if principal.APIKeyID == "" {
		return true
	}
	if len(principal.Permissions) == 0 {
		return false
	}
	switch resourceType {
	case domain.ResourceTypeConversation:
		return hasPermission(principal.Permissions, domain.PermissionMessagesRead)
	case domain.ResourceTypeFile:
		return hasPermission(principal.Permissions, domain.PermissionFilesRead) ||
			hasPermission(principal.Permissions, domain.PermissionFilesWrite)
	case domain.ResourceTypeTeam, domain.ResourceTypeUser, domain.ResourceTypeUsergroup:
		return true
	default:
		return false
	}
}

func hasPermission(perms []string, required string) bool {
	if required == "" {
		return true
	}
	for _, candidate := range perms {
		if candidate == "*" || candidate == required {
			return true
		}
		if strings.HasSuffix(candidate, ".*") {
			prefix := strings.TrimSuffix(candidate, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
	}
	return false
}

func (r *ExternalEventRepo) feedSubquery(args *[]any, feedTable, resourceTable, feedColumn, resourceColumn, teamColumn, resourceID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
		SELECT f.external_event_id
		FROM %s f
		JOIN %s r ON r.%s = f.%s
		WHERE r.%s = $1 AND f.external_event_id > $2
	`, feedTable, resourceTable, resourceColumn, feedColumn, teamColumn)
	if resourceID != "" {
		*args = append(*args, resourceID)
		fmt.Fprintf(&b, " AND r.%s = $%d", resourceColumn, len(*args))
	}
	return b.String()
}

func (r *ExternalEventRepo) activeExternalAccessForPrincipal(ctx context.Context, hostTeamID, principalID string) (*externalAccessState, error) {
	if hostTeamID == "" || principalID == "" {
		return nil, nil
	}
	var (
		id       string
		capsJSON []byte
	)
	err := r.db.QueryRow(ctx, `
		SELECT id, allowed_capabilities
		FROM external_principal_access
		WHERE host_team_id = $1
		  AND principal_id = $2
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $3)
		ORDER BY created_at DESC
		LIMIT 1
	`, hostTeamID, principalID, time.Now().UTC()).Scan(&id, &capsJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup external access for events: %w", err)
	}
	var caps []string
	if len(capsJSON) > 0 {
		if err := json.Unmarshal(capsJSON, &caps); err != nil {
			return nil, fmt.Errorf("decode external access capabilities: %w", err)
		}
	}
	return &externalAccessState{ID: id, AllowedCapabilities: caps}, nil
}

type externalEventScanner interface {
	Scan(dest ...any) error
}

func scanExternalEvent(row externalEventScanner) (domain.ExternalEvent, error) {
	var event domain.ExternalEvent
	var sourceID sql.NullInt64
	var sourceIDsRaw []byte
	if err := row.Scan(
		&event.ID,
		&event.TeamID,
		&event.Type,
		&event.ResourceType,
		&event.ResourceID,
		&event.OccurredAt,
		&event.Payload,
		&sourceID,
		&sourceIDsRaw,
		&event.DedupeKey,
		&event.CreatedAt,
	); err != nil {
		return domain.ExternalEvent{}, fmt.Errorf("scan external event: %w", err)
	}
	if sourceID.Valid {
		v := sourceID.Int64
		event.SourceInternalEventID = &v
	}
	if len(sourceIDsRaw) > 0 {
		if err := json.Unmarshal(sourceIDsRaw, &event.SourceInternalEventIDs); err != nil {
			return domain.ExternalEvent{}, fmt.Errorf("decode source internal event ids: %w", err)
		}
	}
	if event.SourceInternalEventIDs == nil {
		event.SourceInternalEventIDs = []int64{}
	}
	return event, nil
}
