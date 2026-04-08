package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/repository"
)

func (r *Runtime) loadUser(ctx context.Context, userID uuid.UUID) (userRow, error) {
	row := r.db.QueryRow(ctx, `
		select u.id, u.principal_type, u.status, u.email, p.handle, p.display_name, p.avatar_url, p.bio, a.metadata, u.created_at, greatest(u.updated_at, p.updated_at, coalesce(a.updated_at, u.updated_at))
		from users u
		join user_profiles p on p.user_id = u.id
		left join agents a on a.user_id = u.id
		where u.id = $1`, userID)
	var user userRow
	var metadata []byte
	if err := row.Scan(
		&user.ID,
		&user.PrincipalType,
		&user.Status,
		&user.Email,
		&user.Handle,
		&user.DisplayName,
		&user.AvatarURL,
		&user.Bio,
		&metadata,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		return userRow{}, err
	}
	user.Metadata = readJSONMap(metadata)
	user.CreatedAt = user.CreatedAt.UTC()
	user.UpdatedAt = user.UpdatedAt.UTC()
	return user, nil
}

func (r *Runtime) loadWorkspace(ctx context.Context, workspaceID uuid.UUID) (workspaceRow, error) {
	row, err := r.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return workspaceRow{}, err
	}
	return workspaceRow{
		ID:              row.ID,
		Slug:            row.Slug,
		Name:            row.Name,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       dbsqlc.TimeValue(row.CreatedAt),
		UpdatedAt:       dbsqlc.TimeValue(row.UpdatedAt),
	}, nil
}

func (r *Runtime) loadConversation(ctx context.Context, conversationID uuid.UUID) (conversationRow, error) {
	row, err := r.queries.GetConversation(ctx, conversationID)
	if err != nil {
		return conversationRow{}, err
	}
	return conversationRow{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		AccessPolicy:     row.AccessPolicy,
		Title:            row.Title,
		Description:      row.Description,
		CreatedByUserID:  row.CreatedByUserID,
		ArchivedAt:       dbsqlc.TimePtr(row.ArchivedAt),
		LastMessageAt:    dbsqlc.TimePtr(row.LastMessageAt),
		CreatedAt:        dbsqlc.TimeValue(row.CreatedAt),
		UpdatedAt:        dbsqlc.TimeValue(row.UpdatedAt),
		ParticipantCount: int(row.ParticipantCount),
	}, nil
}

func (r *Runtime) loadMessage(ctx context.Context, messageID uuid.UUID) (messageRow, error) {
	row, err := r.queries.GetMessage(ctx, messageID)
	if err != nil {
		return messageRow{}, err
	}
	var bodyRich []byte
	if row.BodyRich != nil {
		bodyRich = append(bodyRich, (*row.BodyRich)...)
	}
	var metadata []byte
	if row.Metadata != nil {
		metadata = append(metadata, (*row.Metadata)...)
	}
	return messageRow{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		AuthorUserID:   row.AuthorUserID,
		BodyText:       row.BodyText,
		BodyRich:       readJSONMap(bodyRich),
		Metadata:       readJSONMap(metadata),
		EditedAt:       dbsqlc.TimePtr(row.EditedAt),
		DeletedAt:      dbsqlc.TimePtr(row.DeletedAt),
		CreatedAt:      dbsqlc.TimeValue(row.CreatedAt),
	}, nil
}

func (r *Runtime) loadExternalEventByID(ctx context.Context, eventID uuid.UUID) (externalEventRow, error) {
	row := r.db.QueryRow(ctx, `
		select id, workspace_id, type, resource_type, resource_id, occurred_at, payload, source_internal_event_id
		from external_events
		where id = $1`, eventID)
	return scanExternalEvent(row)
}

func (r *Runtime) loadExternalEventBySourceInternalEventID(ctx context.Context, sourceInternalEventID uuid.UUID) (externalEventRow, error) {
	row := r.db.QueryRow(ctx, `
		select id, workspace_id, type, resource_type, resource_id, occurred_at, payload, source_internal_event_id
		from external_events
		where source_internal_event_id = $1`, sourceInternalEventID)
	return scanExternalEvent(row)
}

func scanExternalEvent(row pgx.Row) (externalEventRow, error) {
	var event externalEventRow
	var payload []byte
	if err := row.Scan(
		&event.ID,
		&event.WorkspaceID,
		&event.Type,
		&event.ResourceType,
		&event.ResourceID,
		&event.OccurredAt,
		&payload,
		&event.SourceInternalEventID,
	); err != nil {
		return externalEventRow{}, err
	}
	event.Payload = readJSONMap(payload)
	event.OccurredAt = event.OccurredAt.UTC()
	return event, nil
}

func (r *Runtime) workspaceVisible(ctx context.Context, viewerID uuid.UUID, workspaceID uuid.UUID) (bool, error) {
	row, err := r.queries.GetWorkspaceMembership(ctx, dbsqlc.GetWorkspaceMembershipParams{
		WorkspaceID: workspaceID,
		UserID:      viewerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.Status == "active", nil
}

func (r *Runtime) isConversationParticipant(ctx context.Context, conversationID uuid.UUID, userID uuid.UUID) (bool, error) {
	return r.queries.IsConversationParticipant(ctx, dbsqlc.IsConversationParticipantParams{
		ConversationID: conversationID,
		UserID:         userID,
	})
}

func (r *Runtime) conversationVisible(ctx context.Context, viewerID uuid.UUID, allowGlobal bool, conversation conversationRow) (bool, error) {
	if conversation.WorkspaceID == nil {
		if !allowGlobal || conversation.AccessPolicy != "members" {
			return false, nil
		}
		return r.isConversationParticipant(ctx, conversation.ID, viewerID)
	}
	visible, err := r.workspaceVisible(ctx, viewerID, *conversation.WorkspaceID)
	if err != nil || !visible {
		return visible, err
	}
	if conversation.AccessPolicy == "workspace" {
		return true, nil
	}
	return r.isConversationParticipant(ctx, conversation.ID, viewerID)
}

func (r *Runtime) listMemberConversationScopesByUser(ctx context.Context, userID uuid.UUID, workspaceID *uuid.UUID) ([]documentAnchor, error) {
	rows, err := r.db.Query(ctx, `
		select c.id, c.workspace_id
		from conversations c
		join conversation_participants cp on cp.conversation_id = c.id
		where cp.user_id = $1
		  and c.access_policy = 'members'
		  and ($2::uuid is null or c.workspace_id = $2)
		order by c.id asc`, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	anchors := make([]documentAnchor, 0)
	for rows.Next() {
		var conversationID uuid.UUID
		var scopedWorkspaceID *uuid.UUID
		if err := rows.Scan(&conversationID, &scopedWorkspaceID); err != nil {
			return nil, err
		}
		anchors = append(anchors, documentAnchor{
			PrincipalID:    conversationPrincipalID(conversationID),
			WorkspaceID:    scopedWorkspaceID,
			ConversationID: &conversationID,
			AnchorKey:      "conversation:" + conversationID.String(),
		})
	}
	return anchors, rows.Err()
}

func (r *Runtime) resolveQueryPrincipals(ctx context.Context, userID uuid.UUID, allowGlobal bool, workspaceID *uuid.UUID) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]struct{}{}
	items := make([]uuid.UUID, 0, 16)
	add := func(id uuid.UUID) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		items = append(items, id)
	}

	if allowGlobal && workspaceID == nil {
		add(userPrincipalID(userID))
	}

	if workspaceID != nil {
		add(workspacePrincipalID(*workspaceID))
	} else {
		workspaces, err := r.queries.ListActiveWorkspacesByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, workspace := range workspaces {
			add(workspacePrincipalID(workspace.ID))
		}
	}

	anchors, err := r.listMemberConversationScopesByUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, anchor := range anchors {
		add(anchor.PrincipalID)
	}
	return items, nil
}

func (r *Runtime) conversationTitle(ctx context.Context, conversation conversationRow) *string {
	if value := trimOptionalStringValue(conversation.Title); value != nil {
		return value
	}
	if conversation.AccessPolicy != "members" {
		return nil
	}
	rows, err := r.queries.ListConversationParticipants(ctx, conversation.ID)
	if err != nil {
		return nil
	}
	parts := make([]string, 0, len(rows))
	for _, participant := range rows {
		name := strings.TrimSpace(participant.DisplayName)
		if name == "" {
			name = strings.TrimSpace(participant.Handle)
		}
		if name != "" {
			parts = append(parts, name)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return nil
	}
	value := strings.Join(parts, ", ")
	return &value
}

func (r *Runtime) listActiveWorkspaceAnchorsForUser(ctx context.Context, userID uuid.UUID) ([]documentAnchor, error) {
	rows, err := r.db.Query(ctx, `
		select workspace_id
		from workspace_memberships
		where user_id = $1
		  and status = 'active'
		order by workspace_id asc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]documentAnchor, 0)
	for rows.Next() {
		var workspaceID uuid.UUID
		if err := rows.Scan(&workspaceID); err != nil {
			return nil, err
		}
		items = append(items, documentAnchor{
			PrincipalID: workspacePrincipalID(workspaceID),
			WorkspaceID: &workspaceID,
			AnchorKey:   "workspace:" + workspaceID.String(),
		})
	}
	return items, rows.Err()
}

func (r *Runtime) userVisible(ctx context.Context, viewerID uuid.UUID, targetID uuid.UUID, workspaceScope *uuid.UUID, conversationScope *uuid.UUID) (bool, error) {
	switch {
	case viewerID == targetID:
		return true, nil
	case conversationScope != nil:
		row := r.db.QueryRow(ctx, `
			select exists (
				select 1
				from conversation_participants viewer
				join conversation_participants target
				  on target.conversation_id = viewer.conversation_id
				where viewer.conversation_id = $1
				  and viewer.user_id = $2
				  and target.user_id = $3
			)`, *conversationScope, viewerID, targetID)
		var visible bool
		if err := row.Scan(&visible); err != nil {
			return false, err
		}
		return visible, nil
	case workspaceScope != nil:
		row := r.db.QueryRow(ctx, `
			select exists (
				select 1
				from workspace_memberships viewer
				join workspace_memberships target
				  on target.workspace_id = viewer.workspace_id
				where viewer.workspace_id = $1
				  and viewer.user_id = $2
				  and target.user_id = $3
				  and viewer.status = 'active'
				  and target.status = 'active'
			)`, *workspaceScope, viewerID, targetID)
		var visible bool
		if err := row.Scan(&visible); err != nil {
			return false, err
		}
		return visible, nil
	default:
		return false, nil
	}
}

func (r *Runtime) loadVisibleExternalEvent(ctx context.Context, viewerID uuid.UUID, eventID uuid.UUID) (externalEventRow, error) {
	visibility := repository.ExternalEventVisibilityPredicate("ee", "$1")
	row := r.db.QueryRow(ctx, fmt.Sprintf(`
		select ee.id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload, ee.source_internal_event_id
		from external_events ee
		where ee.id = $2
		  and %s`, visibility), viewerID, eventID)
	return scanExternalEvent(row)
}

func (r *Runtime) listEventAnchors(ctx context.Context, event externalEventRow) ([]documentAnchor, error) {
	type feedAnchor struct {
		scopeType      string
		principalRawID string
		workspaceID    *uuid.UUID
		conversationID *uuid.UUID
		accessPolicy   *string
	}

	rows, err := r.db.Query(ctx, `
		select 'user'::text as scope_type, uef.user_id::text as principal_raw_id, null::uuid as workspace_id, null::uuid as conversation_id, null::text as access_policy
		from user_event_feed uef
		where uef.external_event_id = $1
		union all
		select 'workspace'::text as scope_type, wef.workspace_id::text as principal_raw_id, wef.workspace_id, null::uuid as conversation_id, null::text as access_policy
		from workspace_event_feed wef
		where wef.external_event_id = $1
		union all
		select 'conversation'::text as scope_type, cef.conversation_id::text as principal_raw_id, c.workspace_id, cef.conversation_id, c.access_policy
		from conversation_event_feed cef
		join conversations c on c.id = cef.conversation_id
		where cef.external_event_id = $1`, event.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	anchors := make([]documentAnchor, 0)
	for rows.Next() {
		var row feedAnchor
		if err := rows.Scan(&row.scopeType, &row.principalRawID, &row.workspaceID, &row.conversationID, &row.accessPolicy); err != nil {
			return nil, err
		}
		var anchor documentAnchor
		switch row.scopeType {
		case "user":
			userID, err := uuid.Parse(row.principalRawID)
			if err != nil {
				return nil, err
			}
			anchor = documentAnchor{
				PrincipalID: userPrincipalID(userID),
				AnchorKey:   "user:" + userID.String(),
			}
		case "workspace":
			workspaceID, err := uuid.Parse(row.principalRawID)
			if err != nil {
				return nil, err
			}
			anchor = documentAnchor{
				PrincipalID: workspacePrincipalID(workspaceID),
				WorkspaceID: &workspaceID,
				AnchorKey:   "workspace:" + workspaceID.String(),
			}
		case "conversation":
			conversationID, err := uuid.Parse(row.principalRawID)
			if err != nil {
				return nil, err
			}
			switch {
			case row.workspaceID != nil && row.accessPolicy != nil && *row.accessPolicy == "workspace":
				anchor = documentAnchor{
					PrincipalID: workspacePrincipalID(*row.workspaceID),
					WorkspaceID: row.workspaceID,
					AnchorKey:   "workspace:" + row.workspaceID.String(),
				}
			default:
				anchor = documentAnchor{
					PrincipalID:    conversationPrincipalID(conversationID),
					WorkspaceID:    row.workspaceID,
					ConversationID: &conversationID,
					AnchorKey:      "conversation:" + conversationID.String(),
				}
			}
		default:
			return nil, fmt.Errorf("unsupported event scope %q", row.scopeType)
		}
		if _, ok := seen[anchor.AnchorKey]; ok {
			continue
		}
		seen[anchor.AnchorKey] = struct{}{}
		anchors = append(anchors, anchor)
	}
	return anchors, rows.Err()
}
