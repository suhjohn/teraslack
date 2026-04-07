package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/johnsuh/teraslack/server/internal/api"
	"github.com/johnsuh/teraslack/server/internal/domain"
	"github.com/johnsuh/teraslack/server/internal/repository"
)

type dashboardConversationVisibility struct {
	clause string
	args   []any
	next   int
}

func (s *Server) handleDashboardOverview(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, scope, appErr := s.resolveDashboardScope(r, auth)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}

	keys, err := s.loadDashboardAPIKeySummary(r.Context(), auth.UserID, workspaceID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	webhooks, err := s.loadDashboardWebhookSummary(r.Context(), auth.UserID, workspaceID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	data, err := s.loadDashboardDataSummary(r.Context(), auth, workspaceID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	writeJSON(w, http.StatusOK, api.DashboardOverview{
		Scope:    scope,
		APIKeys:  keys,
		Webhooks: webhooks,
		Data:     data,
	})
}

func (s *Server) handleDashboardWebhooks(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, scope, appErr := s.resolveDashboardScope(r, auth)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	limit, appErr := parseLimitQuery(r.URL.Query().Get("limit"), 50, 200)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}

	response, err := s.loadDashboardWebhooksResponse(r.Context(), auth.UserID, workspaceID, scope, limit)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDashboardDataActivity(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, scope, appErr := s.resolveDashboardScope(r, auth)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	days, appErr := parseDashboardDaysQuery(r.URL.Query().Get("days"), 7, 90)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}

	response, err := s.loadDashboardDataActivityResponse(r.Context(), auth, workspaceID, scope, days)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDashboardAudit(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, scope, appErr := s.resolveDashboardScope(r, auth)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	limit, appErr := parseLimitQuery(r.URL.Query().Get("limit"), 50, 200)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}

	response, err := s.loadDashboardAuditResponse(r.Context(), auth.UserID, workspaceID, scope, limit)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) resolveDashboardScope(r *http.Request, auth domain.AuthContext) (*uuid.UUID, api.DashboardScope, *appError) {
	var workspaceID *uuid.UUID
	if raw := r.URL.Query().Get("workspace_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, api.DashboardScope{}, validationFailed("workspace_id", "invalid_uuid", "Must be a valid UUID.")
		}
		workspaceID = &parsed
	}

	if auth.APIKeyWorkspaceID != nil {
		if workspaceID == nil {
			workspaceID = auth.APIKeyWorkspaceID
		} else if *workspaceID != *auth.APIKeyWorkspaceID {
			return nil, api.DashboardScope{}, forbidden("This API key cannot access that workspace.")
		}
	}

	scope := api.DashboardScope{}
	if workspaceID == nil {
		return nil, scope, nil
	}

	if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *workspaceID); appErr != nil {
		return nil, api.DashboardScope{}, appErr
	}

	workspace, err := s.loadWorkspace(r.Context(), *workspaceID)
	if err != nil {
		return nil, api.DashboardScope{}, internalError(err)
	}

	scope.WorkspaceID = stringPtr(workspace.ID.String())
	scope.WorkspaceName = stringPtr(workspace.Name)
	return workspaceID, scope, nil
}

func parseDashboardDaysQuery(raw string, defaultValue int, maxValue int) (int, *appError) {
	value, appErr := parseLimitQuery(raw, defaultValue, maxValue)
	if appErr != nil {
		appErr.Errors[0].Field = "days"
		appErr.Errors[0].Message = fmt.Sprintf("Must be less than or equal to %d.", maxValue)
		return 0, appErr
	}
	return value, nil
}

func (s *Server) loadDashboardAPIKeySummary(ctx context.Context, userID uuid.UUID, workspaceID *uuid.UUID) (api.DashboardAPIKeySummary, error) {
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)

	var summary api.DashboardAPIKeySummary
	var lastUsedAt *time.Time
	if workspaceID == nil {
		err := s.db.QueryRow(ctx, `select
			count(*)::int,
			count(*) filter (where revoked_at is null)::int,
			count(*) filter (where revoked_at is not null)::int,
			count(*) filter (
				where revoked_at is null
				  and expires_at is not null
				  and expires_at >= now()
				  and expires_at <= now() + interval '30 days'
			)::int,
			count(*) filter (
				where revoked_at is null
				  and (last_used_at is null or last_used_at < $2)
			)::int,
			max(last_used_at)
		from api_keys
		where user_id = $1`,
			userID,
			cutoff,
		).Scan(
			&summary.Total,
			&summary.Active,
			&summary.Revoked,
			&summary.ExpiringSoon,
			&summary.Stale,
			&lastUsedAt,
		)
		if err != nil {
			return api.DashboardAPIKeySummary{}, err
		}
	} else {
		err := s.db.QueryRow(ctx, `select
			count(*)::int,
			count(*) filter (where revoked_at is null)::int,
			count(*) filter (where revoked_at is not null)::int,
			count(*) filter (
				where revoked_at is null
				  and expires_at is not null
				  and expires_at >= now()
				  and expires_at <= now() + interval '30 days'
			)::int,
			count(*) filter (
				where revoked_at is null
				  and (last_used_at is null or last_used_at < $3)
			)::int,
			max(last_used_at)
		from api_keys
		where user_id = $1
		  and scope_workspace_id = $2`,
			userID,
			*workspaceID,
			cutoff,
		).Scan(
			&summary.Total,
			&summary.Active,
			&summary.Revoked,
			&summary.ExpiringSoon,
			&summary.Stale,
			&lastUsedAt,
		)
		if err != nil {
			return api.DashboardAPIKeySummary{}, err
		}
	}

	summary.LastUsedAt = timePtrToStringPtr(lastUsedAt)
	return summary, nil
}

func (s *Server) loadDashboardWebhookSummary(ctx context.Context, userID uuid.UUID, workspaceID *uuid.UUID) (api.DashboardWebhookSummary, error) {
	var summary api.DashboardWebhookSummary
	err := s.db.QueryRow(ctx, `select
		count(distinct es.id)::int,
		count(distinct es.id) filter (where es.enabled)::int,
		count(wd.id) filter (where wd.status in ('pending', 'processing'))::int,
		count(wd.id) filter (where wd.status = 'failed')::int,
		count(wd.id) filter (
			where wd.status = 'delivered'
			  and wd.delivered_at >= now() - interval '24 hours'
		)::int,
		count(wd.id) filter (
			where wd.status = 'failed'
			  and wd.updated_at >= now() - interval '24 hours'
		)::int
	from event_subscriptions es
	left join webhook_deliveries wd on wd.subscription_id = es.id
	where es.owner_user_id = $1
	  and ($2::uuid is null or es.workspace_id = $2)`,
		userID,
		workspaceID,
	).Scan(
		&summary.Subscriptions,
		&summary.EnabledSubscriptions,
		&summary.PendingDeliveries,
		&summary.FailedDeliveries,
		&summary.Delivered24h,
		&summary.Failed24h,
	)
	if err != nil {
		return api.DashboardWebhookSummary{}, err
	}
	return summary, nil
}

func (s *Server) loadDashboardDataSummary(ctx context.Context, auth domain.AuthContext, workspaceID *uuid.UUID) (api.DashboardDataSummary, error) {
	visibility := buildDashboardConversationVisibility("c", workspaceID, auth.UserID)

	var summary api.DashboardDataSummary
	row := s.db.QueryRow(ctx, fmt.Sprintf(`select
		count(*)::int,
		count(*) filter (where c.access_policy = 'members')::int,
		count(*) filter (where c.access_policy <> 'members')::int
	from conversations c
	where %s`, visibility.clause), visibility.args...)
	if err := row.Scan(&summary.Conversations, &summary.MemberConversations, &summary.BroadcastConversations); err != nil {
		return api.DashboardDataSummary{}, err
	}

	messageCutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	messageArgs := append([]any(nil), visibility.args...)
	messageArgs = append(messageArgs, messageCutoff)
	row = s.db.QueryRow(ctx, fmt.Sprintf(`select
		count(m.id)::int
	from messages m
	join conversations c on c.id = m.conversation_id
	where %s
	  and m.deleted_at is null
	  and m.created_at >= $%d`, visibility.clause, visibility.next), messageArgs...)
	if err := row.Scan(&summary.Messages7d); err != nil {
		return api.DashboardDataSummary{}, err
	}

	eventCutoff := time.Now().UTC().Add(-24 * time.Hour)
	eventArgs := []any{auth.UserID}
	eventQuery := fmt.Sprintf(`select
		count(*)::int
	from external_events ee
	where %s
	  and ee.occurred_at >= $2`, repository.ExternalEventVisibilityPredicate("ee", "$1"))
	if workspaceID != nil {
		eventArgs = append(eventArgs, *workspaceID)
		eventArgs = append(eventArgs, eventCutoff)
		eventQuery = fmt.Sprintf(`select
			count(*)::int
		from external_events ee
		where %s
		  and ee.workspace_id = $2
		  and ee.occurred_at >= $3`, repository.ExternalEventVisibilityPredicate("ee", "$1"))
	} else {
		eventArgs = append(eventArgs, eventCutoff)
	}
	if err := s.db.QueryRow(ctx, eventQuery, eventArgs...).Scan(&summary.RecentEvents24h); err != nil {
		return api.DashboardDataSummary{}, err
	}

	return summary, nil
}

func (s *Server) loadDashboardWebhooksResponse(ctx context.Context, userID uuid.UUID, workspaceID *uuid.UUID, scope api.DashboardScope, limit int) (api.DashboardWebhooksResponse, error) {
	response := api.DashboardWebhooksResponse{
		Scope: scope,
	}

	summary, err := s.loadDashboardWebhookSummary(ctx, userID, workspaceID)
	if err != nil {
		return api.DashboardWebhooksResponse{}, err
	}
	response.Summary = summary

	rows, err := s.db.Query(ctx, `select
		es.id,
		es.url,
		es.enabled,
		es.event_type,
		es.resource_type,
		es.resource_id,
		es.created_at,
		es.updated_at,
		count(wd.id)::int,
		count(wd.id) filter (where wd.status = 'delivered')::int,
		count(wd.id) filter (where wd.status = 'failed')::int,
		count(wd.id) filter (where wd.status in ('pending', 'processing'))::int,
		max(coalesce(wd.delivered_at, wd.updated_at)),
		(array_agg(wd.status order by coalesce(wd.delivered_at, wd.updated_at, wd.created_at) desc) filter (where wd.id is not null))[1],
		(array_agg(wd.last_error order by coalesce(wd.delivered_at, wd.updated_at, wd.created_at) desc) filter (where wd.id is not null))[1]
	from event_subscriptions es
	left join webhook_deliveries wd on wd.subscription_id = es.id
	where es.owner_user_id = $1
	  and ($2::uuid is null or es.workspace_id = $2)
	group by es.id, es.url, es.enabled, es.event_type, es.resource_type, es.resource_id, es.created_at, es.updated_at
	order by es.updated_at desc`,
		userID,
		workspaceID,
	)
	if err != nil {
		return api.DashboardWebhooksResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item api.DashboardWebhookSubscription
		var subscriptionID uuid.UUID
		var resourceID *uuid.UUID
		var createdAt time.Time
		var updatedAt time.Time
		var lastDeliveryAt *time.Time
		if err := rows.Scan(
			&subscriptionID,
			&item.URL,
			&item.Enabled,
			&item.EventType,
			&item.ResourceType,
			&resourceID,
			&createdAt,
			&updatedAt,
			&item.TotalDeliveries,
			&item.DeliveredCount,
			&item.FailedCount,
			&item.PendingCount,
			&lastDeliveryAt,
			&item.LastStatus,
			&item.LastError,
		); err != nil {
			return api.DashboardWebhooksResponse{}, err
		}
		item.SubscriptionID = subscriptionID.String()
		item.ResourceID = uuidPtrToStringPtr(resourceID)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		item.LastDeliveryAt = timePtrToStringPtr(lastDeliveryAt)
		response.Subscriptions = append(response.Subscriptions, item)
	}

	rows, err = s.db.Query(ctx, `select
		wd.id,
		es.id,
		es.url,
		ee.id,
		ee.type,
		ee.resource_type,
		ee.resource_id,
		wd.status,
		wd.attempt_count,
		wd.last_error,
		wd.delivered_at,
		wd.created_at
	from webhook_deliveries wd
	join event_subscriptions es on es.id = wd.subscription_id
	join external_events ee on ee.id = wd.external_event_id
	where es.owner_user_id = $1
	  and ($2::uuid is null or es.workspace_id = $2)
	order by coalesce(wd.delivered_at, wd.updated_at, wd.created_at) desc, wd.id desc
	limit $3`,
		userID,
		workspaceID,
		limit,
	)
	if err != nil {
		return api.DashboardWebhooksResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item api.DashboardWebhookDelivery
		var subscriptionID uuid.UUID
		var resourceID uuid.UUID
		var deliveredAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(
			&item.DeliveryID,
			&subscriptionID,
			&item.URL,
			&item.EventID,
			&item.EventType,
			&item.ResourceType,
			&resourceID,
			&item.Status,
			&item.AttemptCount,
			&item.LastError,
			&deliveredAt,
			&createdAt,
		); err != nil {
			return api.DashboardWebhooksResponse{}, err
		}
		item.SubscriptionID = subscriptionID.String()
		item.ResourceID = resourceID.String()
		item.DeliveredAt = timePtrToStringPtr(deliveredAt)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		response.RecentDeliveries = append(response.RecentDeliveries, item)
	}

	return response, nil
}

func (s *Server) loadDashboardDataActivityResponse(ctx context.Context, auth domain.AuthContext, workspaceID *uuid.UUID, scope api.DashboardScope, days int) (api.DashboardDataActivityResponse, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days+1)
	cutoff = utcStartOfDay(cutoff)

	response := api.DashboardDataActivityResponse{
		Scope: scope,
		Days:  days,
	}

	summary, err := s.loadDashboardDataSummary(ctx, auth, workspaceID)
	if err != nil {
		return api.DashboardDataActivityResponse{}, err
	}
	response.Summary = summary

	visibility := buildDashboardConversationVisibility("c", workspaceID, auth.UserID)

	roomMixRows, err := s.db.Query(ctx, fmt.Sprintf(`select
		case
			when c.workspace_id is null and c.access_policy = 'authenticated' then 'Global broadcast'
			when c.workspace_id is null and c.access_policy = 'members' then 'Global member-only'
			when c.workspace_id is not null and c.access_policy = 'workspace' then 'Workspace broadcast'
			else 'Workspace member-only'
		end as label,
		count(*)::int
	from conversations c
	where %s
	group by 1
	order by 2 desc, 1 asc`, visibility.clause), visibility.args...)
	if err != nil {
		return api.DashboardDataActivityResponse{}, err
	}
	defer roomMixRows.Close()
	for roomMixRows.Next() {
		var bucket api.DashboardCountBucket
		if err := roomMixRows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return api.DashboardDataActivityResponse{}, err
		}
		response.RoomMix = append(response.RoomMix, bucket)
	}

	topConversationArgs := append([]any(nil), visibility.args...)
	topConversationArgs = append(topConversationArgs, cutoff)
	topConversationRows, err := s.db.Query(ctx, fmt.Sprintf(`select
		c.id,
		c.title,
		c.access_policy,
		coalesce(pc.participant_count, 0)::int,
		c.last_message_at,
		count(m.id)::int
	from conversations c
	left join (
		select conversation_id, count(*)::int as participant_count
		from conversation_participants
		group by conversation_id
	) pc on pc.conversation_id = c.id
	left join messages m
	  on m.conversation_id = c.id
	 and m.deleted_at is null
	 and m.created_at >= $%d
	where %s
	group by c.id, c.title, c.access_policy, pc.participant_count, c.last_message_at
	order by count(m.id) desc, coalesce(c.last_message_at, c.created_at) desc
	limit 10`, visibility.next, visibility.clause), topConversationArgs...)
	if err != nil {
		return api.DashboardDataActivityResponse{}, err
	}
	defer topConversationRows.Close()
	for topConversationRows.Next() {
		var item api.DashboardConversationActivity
		var conversationID uuid.UUID
		var lastMessageAt *time.Time
		if err := topConversationRows.Scan(
			&conversationID,
			&item.Title,
			&item.AccessPolicy,
			&item.ParticipantCount,
			&lastMessageAt,
			&item.MessageCount,
		); err != nil {
			return api.DashboardDataActivityResponse{}, err
		}
		item.ConversationID = conversationID.String()
		item.LastMessageAt = timePtrToStringPtr(lastMessageAt)
		response.TopConversations = append(response.TopConversations, item)
	}

	seriesMap := make(map[string]api.DashboardDataPoint)

	conversationSeriesRows, err := s.db.Query(ctx, fmt.Sprintf(`select
		date_trunc('day', c.created_at)::date,
		count(*)::int
	from conversations c
	where %s
	  and c.created_at >= $%d
	group by 1
	order by 1 asc`, visibility.clause, visibility.next), append(append([]any(nil), visibility.args...), cutoff)...)
	if err != nil {
		return api.DashboardDataActivityResponse{}, err
	}
	defer conversationSeriesRows.Close()
	for conversationSeriesRows.Next() {
		var day time.Time
		var count int
		if err := conversationSeriesRows.Scan(&day, &count); err != nil {
			return api.DashboardDataActivityResponse{}, err
		}
		key := day.UTC().Format("2006-01-02")
		point := seriesMap[key]
		point.Date = key
		point.ConversationsCreated = count
		seriesMap[key] = point
	}

	messageSeriesArgs := append([]any(nil), visibility.args...)
	messageSeriesArgs = append(messageSeriesArgs, cutoff)
	messageSeriesRows, err := s.db.Query(ctx, fmt.Sprintf(`select
		date_trunc('day', m.created_at)::date,
		count(m.id)::int
	from messages m
	join conversations c on c.id = m.conversation_id
	where %s
	  and m.deleted_at is null
	  and m.created_at >= $%d
	group by 1
	order by 1 asc`, visibility.clause, visibility.next), messageSeriesArgs...)
	if err != nil {
		return api.DashboardDataActivityResponse{}, err
	}
	defer messageSeriesRows.Close()
	for messageSeriesRows.Next() {
		var day time.Time
		var count int
		if err := messageSeriesRows.Scan(&day, &count); err != nil {
			return api.DashboardDataActivityResponse{}, err
		}
		key := day.UTC().Format("2006-01-02")
		point := seriesMap[key]
		point.Date = key
		point.MessagesCreated = count
		seriesMap[key] = point
	}

	eventSeriesArgs := []any{auth.UserID}
	eventSeriesQuery := fmt.Sprintf(`select
		date_trunc('day', ee.occurred_at)::date,
		count(*)::int
	from external_events ee
	where %s
	  and ee.occurred_at >= $2
	group by 1
	order by 1 asc`, repository.ExternalEventVisibilityPredicate("ee", "$1"))
	if workspaceID != nil {
		eventSeriesArgs = append(eventSeriesArgs, *workspaceID, cutoff)
		eventSeriesQuery = fmt.Sprintf(`select
			date_trunc('day', ee.occurred_at)::date,
			count(*)::int
		from external_events ee
		where %s
		  and ee.workspace_id = $2
		  and ee.occurred_at >= $3
		group by 1
		order by 1 asc`, repository.ExternalEventVisibilityPredicate("ee", "$1"))
	} else {
		eventSeriesArgs = append(eventSeriesArgs, cutoff)
	}
	eventSeriesRows, err := s.db.Query(ctx, eventSeriesQuery, eventSeriesArgs...)
	if err != nil {
		return api.DashboardDataActivityResponse{}, err
	}
	defer eventSeriesRows.Close()
	for eventSeriesRows.Next() {
		var day time.Time
		var count int
		if err := eventSeriesRows.Scan(&day, &count); err != nil {
			return api.DashboardDataActivityResponse{}, err
		}
		key := day.UTC().Format("2006-01-02")
		point := seriesMap[key]
		point.Date = key
		point.EventsPublished = count
		seriesMap[key] = point
	}

	response.Series = fillDataSeries(seriesMap, cutoff, days)
	return response, nil
}

func (s *Server) loadDashboardAuditResponse(ctx context.Context, userID uuid.UUID, workspaceID *uuid.UUID, scope api.DashboardScope, limit int) (api.DashboardAuditResponse, error) {
	response := api.DashboardAuditResponse{
		Scope: scope,
	}

	rows, err := s.db.Query(ctx, `select
		id,
		event_type,
		aggregate_type,
		aggregate_id,
		workspace_id,
		actor_user_id,
		payload,
		created_at
	from internal_events
	where actor_user_id = $1
	  and ($2::uuid is null or workspace_id = $2)
	order by created_at desc, id desc
	limit $3`,
		userID,
		workspaceID,
		limit,
	)
	if err != nil {
		return api.DashboardAuditResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item api.DashboardAuditItem
		var aggregateID uuid.UUID
		var workspaceIDValue *uuid.UUID
		var actorUserID *uuid.UUID
		var payload []byte
		var createdAt time.Time
		if err := rows.Scan(
			&item.ID,
			&item.EventType,
			&item.AggregateType,
			&aggregateID,
			&workspaceIDValue,
			&actorUserID,
			&payload,
			&createdAt,
		); err != nil {
			return api.DashboardAuditResponse{}, err
		}
		item.AggregateID = aggregateID.String()
		item.WorkspaceID = uuidPtrToStringPtr(workspaceIDValue)
		item.ActorUserID = uuidPtrToStringPtr(actorUserID)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.Payload = readJSONMap(payload)
		response.Items = append(response.Items, item)
	}

	rows, err = s.db.Query(ctx, `select
		event_type,
		count(*)::int
	from internal_events
	where actor_user_id = $1
	  and ($2::uuid is null or workspace_id = $2)
	group by event_type
	order by count(*) desc, event_type asc
	limit 10`,
		userID,
		workspaceID,
	)
	if err != nil {
		return api.DashboardAuditResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket api.DashboardCountBucket
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return api.DashboardAuditResponse{}, err
		}
		response.TopTypes = append(response.TopTypes, bucket)
	}

	return response, nil
}

func buildDashboardConversationVisibility(alias string, workspaceID *uuid.UUID, userID uuid.UUID) dashboardConversationVisibility {
	if workspaceID == nil {
		return dashboardConversationVisibility{
			clause: fmt.Sprintf(`%s.workspace_id is null
				and (
					%s.access_policy = 'authenticated' or
					(%s.access_policy = 'members' and exists (
						select 1
						from conversation_participants cp
						where cp.conversation_id = %s.id
						  and cp.user_id = $1
					))
				)`, alias, alias, alias, alias),
			args: []any{userID},
			next: 2,
		}
	}
	return dashboardConversationVisibility{
		clause: fmt.Sprintf(`%s.workspace_id = $1
			and (
				%s.access_policy = 'workspace' or
				(%s.access_policy = 'members' and exists (
					select 1
					from conversation_participants cp
					where cp.conversation_id = %s.id
					  and cp.user_id = $2
				))
			)`, alias, alias, alias, alias),
		args: []any{*workspaceID, userID},
		next: 3,
	}
}

func fillDataSeries(points map[string]api.DashboardDataPoint, start time.Time, days int) []api.DashboardDataPoint {
	items := make([]api.DashboardDataPoint, 0, days)
	for offset := 0; offset < days; offset++ {
		date := start.AddDate(0, 0, offset).Format("2006-01-02")
		point, ok := points[date]
		if !ok {
			point = api.DashboardDataPoint{Date: date}
		}
		items = append(items, point)
	}
	return items
}

func utcStartOfDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
