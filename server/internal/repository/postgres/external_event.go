package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
	"github.com/suhjohn/teraslack/internal/repository/sqlcgen"
)

type ExternalEventRepo struct {
	q  *sqlcgen.Queries
	db DBTX
}

func NewExternalEventRepo(db DBTX) *ExternalEventRepo {
	return &ExternalEventRepo{q: sqlcgen.New(db), db: db}
}

func (r *ExternalEventRepo) WithTx(tx pgx.Tx) repository.ExternalEventRepository {
	return &ExternalEventRepo{q: sqlcgen.New(tx), db: tx}
}

func (r *ExternalEventRepo) Insert(ctx context.Context, event domain.ExternalEvent) (*domain.ExternalEvent, error) {
	sourceIDs, err := json.Marshal(event.SourceInternalEventIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal source ids: %w", err)
	}

	row, err := r.q.CreateExternalEvent(ctx, sqlcgen.CreateExternalEventParams{
		WorkspaceID:            event.WorkspaceID,
		Type:                   event.Type,
		ResourceType:           event.ResourceType,
		ResourceID:             event.ResourceID,
		OccurredAt:             event.OccurredAt,
		Payload:                event.Payload,
		SourceInternalEventID:  int64ToPgtypeInt8(event.SourceInternalEventID),
		SourceInternalEventIds: sourceIDs,
		DedupeKey:              event.DedupeKey,
	})
	if err != nil {
		return nil, fmt.Errorf("insert external event: %w", err)
	}
	inserted, err := externalEventFromSQLC(row)
	if err != nil {
		return nil, fmt.Errorf("decode source ids: %w", err)
	}

	if err := r.insertFeedRow(ctx, *inserted); err != nil {
		return nil, err
	}
	return inserted, nil
}

func (r *ExternalEventRepo) RecordProjectionFailure(ctx context.Context, internalEventID int64, message string) error {
	if err := r.q.RecordExternalEventProjectionFailure(ctx, sqlcgen.RecordExternalEventProjectionFailureParams{
		InternalEventID: internalEventID,
		Error:           message,
	}); err != nil {
		return fmt.Errorf("record projection failure: %w", err)
	}
	return nil
}

func (r *ExternalEventRepo) GetSince(ctx context.Context, afterID int64, limit int) ([]domain.ExternalEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.q.GetExternalEventsSince(ctx, sqlcgen.GetExternalEventsSinceParams{
		ID:    afterID,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get external events since: %w", err)
	}

	var out []domain.ExternalEvent
	for _, row := range rows {
		event, scanErr := externalEventFromSQLC(row)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *event)
	}
	return out, nil
}

func (r *ExternalEventRepo) ListVisible(ctx context.Context, principal repository.ExternalEventPrincipal, params domain.ListExternalEventsParams) (*domain.CursorPage[domain.ExternalEvent], error) {
	if principal.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id: %w", domain.ErrInvalidArgument)
	}

	limit := params.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	events, err := r.listVisibleEvents(ctx, principal, params, limit+1)
	if err != nil {
		return nil, err
	}

	items := make([]domain.ExternalEvent, 0, len(events))
	for _, row := range events {
		event, scanErr := externalEventFromSQLC(row)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *event)
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
	if err := sqlcgen.New(tx).TruncateExternalEventsAndFeeds(ctx); err != nil {
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

	q := sqlcgen.New(tx)
	if err := q.TruncateExternalEventFeeds(ctx); err != nil {
		return fmt.Errorf("truncate feed tables: %w", err)
	}

	rows, err := q.ListAllExternalEvents(ctx)
	if err != nil {
		return fmt.Errorf("query external events: %w", err)
	}
	for _, row := range rows {
		event, scanErr := externalEventFromSQLC(row)
		if scanErr != nil {
			return scanErr
		}
		if err := (&ExternalEventRepo{q: q, db: tx}).insertFeedRow(ctx, *event); err != nil {
			return err
		}
	}

	if ownTx {
		return tx.Commit(ctx)
	}
	return nil
}

func (r *ExternalEventRepo) insertFeedRow(ctx context.Context, event domain.ExternalEvent) error {
	switch event.ResourceType {
	case domain.ResourceTypeWorkspace:
		if err := r.q.InsertWorkspaceEventFeed(ctx, sqlcgen.InsertWorkspaceEventFeedParams{WorkspaceID: event.ResourceID, ExternalEventID: event.ID}); err != nil {
			return fmt.Errorf("insert %s feed row: %w", event.ResourceType, err)
		}
	case domain.ResourceTypeConversation:
		if err := r.q.InsertConversationEventFeed(ctx, sqlcgen.InsertConversationEventFeedParams{ConversationID: event.ResourceID, ExternalEventID: event.ID}); err != nil {
			return fmt.Errorf("insert %s feed row: %w", event.ResourceType, err)
		}
	case domain.ResourceTypeFile:
		if err := r.q.InsertFileEventFeed(ctx, sqlcgen.InsertFileEventFeedParams{FileID: event.ResourceID, ExternalEventID: event.ID}); err != nil {
			return fmt.Errorf("insert %s feed row: %w", event.ResourceType, err)
		}
	case domain.ResourceTypeUser:
		if err := r.q.InsertUserEventFeed(ctx, sqlcgen.InsertUserEventFeedParams{UserID: event.ResourceID, ExternalEventID: event.ID}); err != nil {
			return fmt.Errorf("insert %s feed row: %w", event.ResourceType, err)
		}
	default:
		return fmt.Errorf("unknown resource_type %q: %w", event.ResourceType, domain.ErrInvalidArgument)
	}
	return nil
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
	case domain.ResourceTypeWorkspace, domain.ResourceTypeUser:
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

func (r *ExternalEventRepo) listVisibleEvents(ctx context.Context, principal repository.ExternalEventPrincipal, params domain.ListExternalEventsParams, limit int) ([]sqlcgen.ExternalEvent, error) {
	if principal.HasWorkspaceUserContext {
		resourceTypes := allowedResourceTypes(principal)
		if params.ResourceType != "" {
			if !principalCanReadExternalResourceType(principal, params.ResourceType) {
				return []sqlcgen.ExternalEvent{}, nil
			}
			resourceTypes = []string{params.ResourceType}
		}
		if len(resourceTypes) == 0 {
			return []sqlcgen.ExternalEvent{}, nil
		}
		rows, err := r.q.ListVisibleExternalEventsByWorkspaceAndResourceTypes(ctx, sqlcgen.ListVisibleExternalEventsByWorkspaceAndResourceTypesParams{
			WorkspaceID: principal.WorkspaceID,
			ID:          params.AfterID,
			Column3:     resourceTypes,
			Limit:       int32(limit),
			EventType:   stringToText(params.Type),
			ResourceID:  stringToText(params.ResourceID),
		})
		if err != nil {
			return nil, fmt.Errorf("list visible external events: %w", err)
		}
		return rows, nil
	}
	if principal.AccountID != "" {
		return r.listVisibleExternalMemberEvents(ctx, principal, params, limit)
	}
	resourceTypes := allowedResourceTypes(principal)
	if params.ResourceType != "" {
		if !principalCanReadExternalResourceType(principal, params.ResourceType) {
			return []sqlcgen.ExternalEvent{}, nil
		}
		resourceTypes = []string{params.ResourceType}
	}
	if len(resourceTypes) == 0 {
		return []sqlcgen.ExternalEvent{}, nil
	}
	rows, err := r.q.ListVisibleExternalEventsByWorkspaceAndResourceTypes(ctx, sqlcgen.ListVisibleExternalEventsByWorkspaceAndResourceTypesParams{
		WorkspaceID: principal.WorkspaceID,
		ID:          params.AfterID,
		Column3:     resourceTypes,
		Limit:       int32(limit),
		EventType:   stringToText(params.Type),
		ResourceID:  stringToText(params.ResourceID),
	})
	if err != nil {
		return nil, fmt.Errorf("list visible external events: %w", err)
	}
	return rows, nil
}

func (r *ExternalEventRepo) listVisibleExternalMemberEvents(ctx context.Context, principal repository.ExternalEventPrincipal, params domain.ListExternalEventsParams, limit int) ([]sqlcgen.ExternalEvent, error) {
	switch params.ResourceType {
	case domain.ResourceTypeConversation:
		if !principalCanReadExternalResourceType(principal, domain.ResourceTypeConversation) {
			return []sqlcgen.ExternalEvent{}, nil
		}
		rows, err := r.q.ListVisibleConversationExternalEventsByExternalMember(ctx, sqlcgen.ListVisibleConversationExternalEventsByExternalMemberParams{
			AccountID:       principal.AccountID,
			HostWorkspaceID: principal.WorkspaceID,
			ID:              params.AfterID,
			Limit:           int32(limit),
			EventType:       stringToText(params.Type),
			ConversationID:  stringToText(params.ResourceID),
		})
		if err != nil {
			return nil, fmt.Errorf("list visible conversation external member events: %w", err)
		}
		return rows, nil
	case domain.ResourceTypeFile:
		if !principalCanReadExternalResourceType(principal, domain.ResourceTypeFile) {
			return []sqlcgen.ExternalEvent{}, nil
		}
		rows, err := r.q.ListVisibleFileExternalEventsByExternalMember(ctx, sqlcgen.ListVisibleFileExternalEventsByExternalMemberParams{
			AccountID:       principal.AccountID,
			HostWorkspaceID: principal.WorkspaceID,
			ID:              params.AfterID,
			Limit:           int32(limit),
			EventType:       stringToText(params.Type),
			FileID:          stringToText(params.ResourceID),
		})
		if err != nil {
			return nil, fmt.Errorf("list visible file external member events: %w", err)
		}
		return rows, nil
	case "":
		resourceTypes := externalMemberResourceTypes(principal)
		if len(resourceTypes) == 0 {
			return []sqlcgen.ExternalEvent{}, nil
		}
		rows, err := r.q.ListVisibleExternalEventsByExternalMemberAndResourceTypes(ctx, sqlcgen.ListVisibleExternalEventsByExternalMemberAndResourceTypesParams{
			AccountID:       principal.AccountID,
			HostWorkspaceID: principal.WorkspaceID,
			ID:              params.AfterID,
			Column4:         resourceTypes,
			Limit:           int32(limit),
			EventType:       stringToText(params.Type),
		})
		if err != nil {
			return nil, fmt.Errorf("list visible external member events: %w", err)
		}
		return rows, nil
	default:
		return []sqlcgen.ExternalEvent{}, nil
	}
}

func allowedResourceTypes(principal repository.ExternalEventPrincipal) []string {
	resourceTypes := []string{domain.ResourceTypeWorkspace, domain.ResourceTypeUser}
	if principalCanReadExternalResourceType(principal, domain.ResourceTypeConversation) {
		resourceTypes = append(resourceTypes, domain.ResourceTypeConversation)
	}
	if principalCanReadExternalResourceType(principal, domain.ResourceTypeFile) {
		resourceTypes = append(resourceTypes, domain.ResourceTypeFile)
	}
	return resourceTypes
}

func externalMemberResourceTypes(principal repository.ExternalEventPrincipal) []string {
	resourceTypes := make([]string, 0, 2)
	if principalCanReadExternalResourceType(principal, domain.ResourceTypeConversation) {
		resourceTypes = append(resourceTypes, domain.ResourceTypeConversation)
	}
	if principalCanReadExternalResourceType(principal, domain.ResourceTypeFile) {
		resourceTypes = append(resourceTypes, domain.ResourceTypeFile)
	}
	return resourceTypes
}

func int64ToPgtypeInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func externalEventFromSQLC(row sqlcgen.ExternalEvent) (*domain.ExternalEvent, error) {
	event := &domain.ExternalEvent{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		Type:         row.Type,
		ResourceType: row.ResourceType,
		ResourceID:   row.ResourceID,
		OccurredAt:   row.OccurredAt,
		Payload:      row.Payload,
		DedupeKey:    row.DedupeKey,
		CreatedAt:    row.CreatedAt,
	}
	if row.SourceInternalEventID.Valid {
		value := row.SourceInternalEventID.Int64
		event.SourceInternalEventID = &value
	}
	if len(row.SourceInternalEventIds) > 0 {
		if err := json.Unmarshal(row.SourceInternalEventIds, &event.SourceInternalEventIDs); err != nil {
			return nil, err
		}
	}
	if event.SourceInternalEventIDs == nil {
		event.SourceInternalEventIDs = []int64{}
	}
	return event, nil
}
