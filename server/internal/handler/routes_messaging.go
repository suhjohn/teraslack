package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnsuh/teraslack/server/internal/api"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/domain"
	"github.com/johnsuh/teraslack/server/internal/repository"
)

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceRaw := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	var workspaceID *uuid.UUID
	if workspaceRaw != "" {
		parsed, err := uuid.Parse(workspaceRaw)
		if err != nil {
			s.writeAppError(w, r, validationFailed("workspace_id", "invalid_uuid", "Must be a valid UUID."))
			return
		}
		workspaceID = &parsed
	}
	if auth.APIKeyWorkspaceID != nil {
		if workspaceID == nil || *workspaceID != *auth.APIKeyWorkspaceID {
			s.writeAppError(w, r, forbidden("This API key cannot access global conversations."))
			return
		}
	}
	if workspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *workspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}
	limit, appErr := parseLimitQuery(r.URL.Query().Get("limit"), 50, 200)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	offset, appErr := parseQueryCursor(r.URL.Query().Get("cursor"))
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	accessPolicy := strings.TrimSpace(r.URL.Query().Get("access_policy"))
	if accessPolicy != "" && accessPolicy != "members" && accessPolicy != "workspace" {
		s.writeAppError(w, r, validationFailed("access_policy", "invalid_value", "Must be one of members or workspace."))
		return
	}

	query := `select
		c.id,
		c.workspace_id,
		c.access_policy,
		c.title,
		c.description,
		c.created_by_user_id,
		c.archived_at,
		c.last_message_at,
		c.created_at,
		c.updated_at,
		coalesce(pc.participant_count, 0)
	from conversations c
	left join (
		select conversation_id, count(*)::int as participant_count
		from conversation_participants
		group by conversation_id
	) pc on pc.conversation_id = c.id
	where `
	args := []any{}
	if workspaceID == nil {
		query += `c.workspace_id is null
			and c.access_policy = 'members'
			and exists (
				select 1 from conversation_participants cp
				where cp.conversation_id = c.id and cp.user_id = $1
			)`
		args = append(args, auth.UserID)
	} else {
		query += `c.workspace_id = $1
			and (
				c.access_policy = 'workspace' or
				(c.access_policy = 'members' and exists (
					select 1 from conversation_participants cp
					where cp.conversation_id = c.id and cp.user_id = $2
				))
			)`
		args = append(args, *workspaceID, auth.UserID)
	}
	if accessPolicy != "" {
		query += fmt.Sprintf(" and c.access_policy = $%d", len(args)+1)
		args = append(args, accessPolicy)
	}
	query += fmt.Sprintf(" order by coalesce(c.last_message_at, c.created_at) desc, c.updated_at desc limit $%d offset $%d", len(args)+1, len(args)+2)
	args = append(args, limit+1, offset)

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	defer rows.Close()
	items := make([]api.Conversation, 0, limit)
	for rows.Next() {
		row, err := scanConversationRow(rows)
		if err != nil {
			s.writeAppError(w, r, internalError(err))
			return
		}
		items = append(items, conversationToAPI(row))
	}
	response := api.CollectionResponse[api.Conversation]{Items: items}
	if len(items) > limit {
		response.Items = response.Items[:limit]
		response.NextCursor = formatNextCursor(offset + limit)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureAgentSafeWrite(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.CreateConversationRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	request.AccessPolicy = strings.TrimSpace(request.AccessPolicy)
	if request.AccessPolicy == "" {
		request.AccessPolicy = "members"
	}
	workspaceID, err := parseOptionalUUID(request.WorkspaceID)
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if auth.APIKeyWorkspaceID != nil {
		if workspaceID == nil || *workspaceID != *auth.APIKeyWorkspaceID {
			s.writeAppError(w, r, forbidden("This API key can only create conversations in its workspace."))
			return
		}
	}
	if request.AccessPolicy != "members" && request.AccessPolicy != "workspace" {
		s.writeAppError(w, r, validationFailed("access_policy", "invalid_value", "Must be one of members or workspace."))
		return
	}
	if workspaceID == nil && request.AccessPolicy == "workspace" {
		s.writeAppError(w, r, validationFailed("access_policy", "invalid_value", "workspace access_policy requires workspace_id."))
		return
	}
	title := trimOptionalString(request.Title)
	description := trimOptionalString(request.Description)
	if request.AccessPolicy != "members" && title == nil {
		s.writeAppError(w, r, validationFailed("title", "required", "This conversation type requires a title."))
		return
	}
	if request.AccessPolicy != "members" && len(request.ParticipantUserIDs) > 0 {
		s.writeAppError(w, r, validationFailed("participant_user_ids", "invalid_value", "participant_user_ids is valid only for members conversations."))
		return
	}
	if workspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *workspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}

	var participants []uuid.UUID
	if request.AccessPolicy == "members" {
		var appErr *appError
		participants, appErr = normalizeParticipants(request.ParticipantUserIDs, auth.UserID)
		if appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
		if len(participants) == 0 {
			participants = []uuid.UUID{auth.UserID}
		}
	}

	var conversation conversationRow
	var conversationID uuid.UUID
	var shareLink *api.ConversationShareLink
	statusCode := http.StatusCreated
	errExec := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		if request.AccessPolicy == "members" {
			if err := s.ensureUsersExistTx(r.Context(), tx, participants); err != nil {
				return err
			}
			if workspaceID != nil {
				if err := s.ensureWorkspaceMembersTx(r.Context(), tx, *workspaceID, participants); err != nil {
					return err
				}
			}
			if workspaceID == nil && len(participants) == 2 {
				first, second := canonicalPair(participants[0], participants[1])
				existingID, err := txQueries.GetConversationPair(r.Context(), dbsqlc.GetConversationPairParams{
					FirstUserID:  first,
					SecondUserID: second,
				})
				if err == nil {
					var loadErr error
					conversation, loadErr = s.loadConversation(r.Context(), existingID)
					if loadErr != nil {
						return loadErr
					}
					statusCode = http.StatusOK
					return nil
				}
				if err != nil && err != pgx.ErrNoRows {
					return err
				}
			}
		}

		now := time.Now().UTC()
		conversationID = uuid.New()
		if err := txQueries.CreateConversation(r.Context(), dbsqlc.CreateConversationParams{
			ID:              conversationID,
			WorkspaceID:     workspaceID,
			AccessPolicy:    request.AccessPolicy,
			Title:           title,
			Description:     description,
			CreatedByUserID: auth.UserID,
			CreatedAt:       dbsqlc.Timestamptz(now),
			UpdatedAt:       dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		if request.AccessPolicy == "members" {
			for _, participant := range participants {
				if err := txQueries.CreateConversationParticipant(r.Context(), dbsqlc.CreateConversationParticipantParams{
					ConversationID: conversationID,
					UserID:         participant,
					AddedByUserID:  &auth.UserID,
					JoinedAt:       dbsqlc.Timestamptz(now),
				}); err != nil {
					return err
				}
			}
			if workspaceID == nil && len(participants) == 2 {
				first, second := canonicalPair(participants[0], participants[1])
				if err := txQueries.CreateConversationPair(r.Context(), dbsqlc.CreateConversationPairParams{
					ConversationID: conversationID,
					FirstUserID:    first,
					SecondUserID:   second,
				}); err != nil {
					return err
				}
			}
		}
		if request.AccessPolicy == "members" && !(workspaceID == nil && len(participants) == 2) {
			link, err := s.createConversationShareLinkTx(r.Context(), tx, conversationID, workspaceID, auth.UserID, time.Now().UTC(), "conversation.share_link.created")
			if err != nil {
				return err
			}
			shareLink = &link
		}
		actor := auth.UserID
		payload := map[string]any{
			"conversation_id": conversationID.String(),
			"access_policy":   request.AccessPolicy,
		}
		if workspaceID != nil {
			payload["workspace_id"] = workspaceID.String()
		}
		if title != nil {
			payload["title"] = *title
		}
		if description != nil {
			payload["description"] = *description
		}
		if err := s.appendEvent(r.Context(), tx, "conversation.created", "conversation", conversationID, workspaceID, &actor, payload); err != nil {
			return err
		}
		if request.AccessPolicy == "members" {
			for _, participant := range participants {
				if err := s.appendEvent(r.Context(), tx, "conversation.participant.added", "conversation", conversationID, workspaceID, &actor, map[string]any{
					"conversation_id": conversationID.String(),
					"user_id":         participant.String(),
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if errExec != nil {
		if appErr, ok := errExec.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(errExec))
		return
	}
	if conversation.ID == uuid.Nil {
		var err error
		conversation, err = s.loadConversation(r.Context(), conversationID)
		if err != nil {
			s.writeAppError(w, r, internalError(err))
			return
		}
	}
	writeJSON(w, statusCode, conversationToAPIWithShareLink(conversation, shareLink))
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationAccess(r.Context(), auth, conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	writeJSON(w, http.StatusOK, conversationToAPI(conversation))
}

func (s *Server) handlePatchConversation(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationPermission(r.Context(), auth, conversation, conversationPermissionUpdateMetadata); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.UpdateConversationRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	title := trimOptionalString(request.Title)
	description := trimOptionalString(request.Description)
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		archivedAt := conversation.ArchivedAt
		if request.Archived != nil {
			if *request.Archived {
				archivedAt = &now
			} else {
				archivedAt = nil
			}
		}
		if err := s.queries.WithTx(tx).UpdateConversationDetails(r.Context(), dbsqlc.UpdateConversationDetailsParams{
			Title:       title,
			Description: description,
			ArchivedAt:  dbsqlc.NullableTimestamptz(archivedAt),
			UpdatedAt:   dbsqlc.Timestamptz(now),
			ID:          conversationID,
		}); err != nil {
			return err
		}
		actor := auth.UserID
		eventType := "conversation.updated"
		if request.Archived != nil && *request.Archived {
			eventType = "conversation.archived"
		}
		if err := s.appendEvent(r.Context(), tx, eventType, "conversation", conversationID, conversation.WorkspaceID, &actor, map[string]any{
			"conversation_id": conversationID.String(),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	updated, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, conversationToAPI(updated))
}

func (s *Server) handleListConversationParticipants(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationAccess(r.Context(), auth, conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	if conversation.AccessPolicy != "members" {
		writeJSON(w, http.StatusOK, api.CollectionResponse[api.User]{Items: []api.User{}})
		return
	}
	rows, err := s.queries.ListConversationParticipants(r.Context(), conversationID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	items := make([]api.User, 0)
	for _, row := range rows {
		items = append(items, userToAPI(userRow{
			ID:            row.ID,
			PrincipalType: row.PrincipalType,
			Status:        row.Status,
			Email:         row.Email,
			Handle:        row.Handle,
			DisplayName:   row.DisplayName,
			AvatarURL:     row.AvatarUrl,
			Bio:           row.Bio,
		}))
	}
	writeJSON(w, http.StatusOK, api.CollectionResponse[api.User]{Items: items})
}

func (s *Server) handleAddConversationParticipants(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureAgentSafeWrite(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationPermission(r.Context(), auth, conversation, conversationPermissionInviteMembers); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	if conversation.AccessPolicy != "members" {
		s.writeAppError(w, r, validationFailed("conversation", "invalid_value", "Participants can only be added to member-only conversations."))
		return
	}
	isDM, err := s.isDirectMessage(r.Context(), conversationID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	if isDM {
		s.writeAppError(w, r, conflict("Canonical direct messages cannot be mutated."))
		return
	}
	var request api.AddParticipantsRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	participants, appErr := normalizeParticipants(request.UserIDs)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		if err := s.ensureUsersExistTx(r.Context(), tx, participants); err != nil {
			return err
		}
		if conversation.WorkspaceID != nil {
			if err := s.ensureWorkspaceMembersTx(r.Context(), tx, *conversation.WorkspaceID, participants); err != nil {
				return err
			}
		}
		actor := auth.UserID
		now := time.Now().UTC()
		for _, participant := range participants {
			rowsAffected, err := txQueries.CreateConversationParticipantIfMissing(r.Context(), dbsqlc.CreateConversationParticipantIfMissingParams{
				ConversationID: conversationID,
				UserID:         participant,
				AddedByUserID:  &auth.UserID,
				JoinedAt:       dbsqlc.Timestamptz(now),
			})
			if err != nil {
				return err
			}
			if rowsAffected > 0 {
				if err := s.appendEvent(r.Context(), tx, "conversation.participant.added", "conversation", conversationID, conversation.WorkspaceID, &actor, map[string]any{
					"conversation_id": conversationID.String(),
					"user_id":         participant.String(),
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	s.handleListConversationParticipants(w, r, auth)
}

func (s *Server) handleDeleteConversationParticipant(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	targetUserID, err := parseUUIDPath(r, "user_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationPermission(r.Context(), auth, conversation, conversationPermissionRemoveMembers); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	if conversation.AccessPolicy != "members" {
		s.writeAppError(w, r, validationFailed("conversation", "invalid_value", "Participants can only be removed from member-only conversations."))
		return
	}
	isDM, err := s.isDirectMessage(r.Context(), conversationID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	if isDM {
		s.writeAppError(w, r, conflict("Canonical direct messages cannot be mutated."))
		return
	}
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		count, err := txQueries.CountConversationParticipants(r.Context(), conversationID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return conflict("A member-only conversation must keep at least one participant.")
		}
		rowsAffected, err := txQueries.DeleteConversationParticipant(r.Context(), dbsqlc.DeleteConversationParticipantParams{
			ConversationID: conversationID,
			UserID:         targetUserID,
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return notFound("Conversation participant not found.")
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "conversation.participant.removed", "conversation", conversationID, conversation.WorkspaceID, &actor, map[string]any{
			"conversation_id": conversationID.String(),
			"user_id":         targetUserID.String(),
		})
	})
	if err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetConversationShareLink(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationPermission(r.Context(), auth, conversation, conversationPermissionManageShareLinks); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	if appErr := s.ensureConversationSupportsShareLink(r.Context(), conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var shareLink api.ConversationShareLink
	if err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		link, err := s.getOrCreateConversationShareLinkTx(r.Context(), tx, conversation, auth.UserID)
		if err != nil {
			return err
		}
		shareLink = link
		return nil
	}); err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, shareLink)
}

func (s *Server) handleRotateConversationShareLink(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureAgentSafeWrite(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationPermission(r.Context(), auth, conversation, conversationPermissionManageShareLinks); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	if appErr := s.ensureConversationSupportsShareLink(r.Context(), conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var shareLink api.ConversationShareLink
	if err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		if _, err := s.queries.WithTx(tx).RevokeActiveConversationInvite(r.Context(), dbsqlc.RevokeActiveConversationInviteParams{
			RevokedAt:      dbsqlc.Timestamptz(now),
			ConversationID: conversation.ID,
		}); err != nil {
			return err
		}
		link, err := s.createConversationShareLinkTx(r.Context(), tx, conversation.ID, conversation.WorkspaceID, auth.UserID, now, "conversation.share_link.rotated")
		if err != nil {
			return err
		}
		shareLink = link
		return nil
	}); err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, shareLink)
}

func (s *Server) ensureConversationSupportsShareLink(ctx context.Context, conversation conversationRow) *appError {
	if conversation.AccessPolicy != "members" {
		return validationFailed("conversation", "invalid_value", "Share links are valid only for member-only conversations.")
	}
	isDM, err := s.isDirectMessage(ctx, conversation.ID)
	if err != nil {
		return internalError(err)
	}
	if isDM {
		return conflict("Canonical direct messages do not have share links.")
	}
	return nil
}

func (s *Server) getOrCreateConversationShareLinkTx(ctx context.Context, tx pgx.Tx, conversation conversationRow, actorUserID uuid.UUID) (api.ConversationShareLink, error) {
	row, err := s.queries.WithTx(tx).GetActiveConversationInviteForUpdate(ctx, conversation.ID)
	switch {
	case err == nil:
		if row.EncryptedToken != nil && *row.EncryptedToken != "" {
			return s.conversationShareLinkToAPI(ctx, *row.EncryptedToken)
		}
		now := time.Now().UTC()
		if _, err := s.queries.WithTx(tx).RevokeActiveConversationInvite(ctx, dbsqlc.RevokeActiveConversationInviteParams{
			RevokedAt:      dbsqlc.Timestamptz(now),
			ConversationID: conversation.ID,
		}); err != nil {
			return api.ConversationShareLink{}, err
		}
		return s.createConversationShareLinkTx(ctx, tx, conversation.ID, conversation.WorkspaceID, actorUserID, now, "conversation.share_link.created")
	case errors.Is(err, pgx.ErrNoRows):
		return s.createConversationShareLinkTx(ctx, tx, conversation.ID, conversation.WorkspaceID, actorUserID, time.Now().UTC(), "conversation.share_link.created")
	default:
		return api.ConversationShareLink{}, err
	}
}

func (s *Server) createConversationShareLinkTx(ctx context.Context, tx pgx.Tx, conversationID uuid.UUID, workspaceID *uuid.UUID, actorUserID uuid.UUID, now time.Time, eventType string) (api.ConversationShareLink, error) {
	token, err := teracrypto.RandomToken(24)
	if err != nil {
		return api.ConversationShareLink{}, err
	}
	encryptedToken, err := s.protector.EncryptString(ctx, token)
	if err != nil {
		return api.ConversationShareLink{}, err
	}
	inviteID := uuid.New()
	if err := s.queries.WithTx(tx).CreateConversationInvite(ctx, dbsqlc.CreateConversationInviteParams{
		ID:              inviteID,
		ConversationID:  conversationID,
		CreatedByUserID: actorUserID,
		TokenHash:       teracrypto.SHA256Hex(token),
		EncryptedToken:  stringPtr(encryptedToken),
		ExpiresAt:       dbsqlc.NullableTimestamptz(nil),
		Mode:            "link",
		AllowedUserIds:  nil,
		AllowedEmails:   nil,
		CreatedAt:       dbsqlc.Timestamptz(now),
	}); err != nil {
		return api.ConversationShareLink{}, err
	}
	if err := s.appendEvent(ctx, tx, eventType, "conversation", conversationID, workspaceID, &actorUserID, map[string]any{
		"conversation_id": conversationID.String(),
	}); err != nil {
		return api.ConversationShareLink{}, err
	}
	return api.ConversationShareLink{
		Token: token,
		URL:   s.conversationShareLinkURL(token),
	}, nil
}

func (s *Server) conversationShareLinkToAPI(ctx context.Context, encryptedToken string) (api.ConversationShareLink, error) {
	token, err := s.protector.DecryptString(ctx, encryptedToken)
	if err != nil {
		return api.ConversationShareLink{}, err
	}
	return api.ConversationShareLink{
		Token: token,
		URL:   s.conversationShareLinkURL(token),
	}, nil
}

func (s *Server) conversationShareLinkURL(token string) string {
	baseURL := strings.TrimSpace(s.cfg.FrontendURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(s.cfg.BaseURL)
	}
	return strings.TrimRight(baseURL, "/") + "/join/conversations/" + url.PathEscape(token)
}

func (s *Server) handleJoinConversation(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureAgentSafeWrite(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.JoinConversationRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	token := strings.TrimSpace(request.Token)
	if token == "" {
		s.writeAppError(w, r, validationFailed("token", "required", "Must not be empty."))
		return
	}
	var conversation conversationRow
	err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		invite, err := txQueries.GetConversationInviteByTokenHashForUpdate(r.Context(), teracrypto.SHA256Hex(token))
		if err != nil {
			if err == pgx.ErrNoRows {
				return notFound("Conversation share link not found.")
			}
			return err
		}
		conversation.ID = invite.ConversationID
		mode := invite.Mode
		var allowedUserJSON []byte
		if invite.AllowedUserIds != nil {
			allowedUserJSON = append(allowedUserJSON, (*invite.AllowedUserIds)...)
		}
		var allowedEmailJSON []byte
		if invite.AllowedEmails != nil {
			allowedEmailJSON = append(allowedEmailJSON, (*invite.AllowedEmails)...)
		}
		expiresAt := dbsqlc.TimePtr(invite.ExpiresAt)
		revokedAt := dbsqlc.TimePtr(invite.RevokedAt)
		if revokedAt != nil || (expiresAt != nil && expiresAt.Before(time.Now().UTC())) {
			return forbidden("Conversation share link is no longer valid.")
		}
		var loadErr error
		conversation, loadErr = s.loadConversation(r.Context(), conversation.ID)
		if loadErr != nil {
			return loadErr
		}
		if conversation.AccessPolicy != "members" {
			return conflict("Conversation share link is not valid for this conversation.")
		}
		isDM, err := s.isDirectMessage(r.Context(), conversation.ID)
		if err != nil {
			return err
		}
		if isDM {
			return conflict("Conversation share link is not valid for direct messages.")
		}
		if conversation.WorkspaceID != nil {
			if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *conversation.WorkspaceID); appErr != nil {
				return appErr
			}
		}
		if mode == "restricted" {
			allowedUsers := stringSliceFromJSON(allowedUserJSON)
			allowedEmails := stringSliceFromJSON(allowedEmailJSON)
			user, err := s.loadUser(r.Context(), auth.UserID)
			if err != nil {
				return err
			}
			userAllowed := slices.Contains(allowedUsers, auth.UserID.String())
			emailAllowed := user.Email != nil && slices.Contains(allowedEmails, normalizeEmail(*user.Email))
			if !userAllowed && !emailAllowed {
				return forbidden("This share link is not valid for the authenticated user.")
			}
		}
		rowsAffected, err := txQueries.CreateConversationParticipantIfMissing(r.Context(), dbsqlc.CreateConversationParticipantIfMissingParams{
			ConversationID: conversation.ID,
			UserID:         auth.UserID,
			JoinedAt:       dbsqlc.Timestamptz(time.Now().UTC()),
		})
		if err != nil {
			return err
		}
		if rowsAffected > 0 {
			actor := auth.UserID
			if err := s.appendEvent(r.Context(), tx, "conversation.participant.added", "conversation", conversation.ID, conversation.WorkspaceID, &actor, map[string]any{
				"conversation_id": conversation.ID.String(),
				"user_id":         auth.UserID.String(),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	updated, err := s.loadConversation(r.Context(), conversation.ID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, conversationToAPI(updated))
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationAccess(r.Context(), auth, conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	limit, appErr := parseLimitQuery(r.URL.Query().Get("limit"), 50, 200)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	offset, appErr := parseQueryCursor(r.URL.Query().Get("cursor"))
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var beforeMessageID *uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("before_message_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			s.writeAppError(w, r, validationFailed("before_message_id", "invalid_uuid", "Must be a valid UUID."))
			return
		}
		beforeMessageID = &parsed
	}
	query := `select id, conversation_id, author_user_id, body_text, body_rich, metadata, edited_at, deleted_at, created_at
		from messages
		where conversation_id = $1`
	args := []any{conversationID}
	if beforeMessageID != nil {
		query += fmt.Sprintf(" and created_at < (select created_at from messages where id = $%d)", len(args)+1)
		args = append(args, *beforeMessageID)
	}
	query += fmt.Sprintf(" order by created_at desc limit $%d offset $%d", len(args)+1, len(args)+2)
	args = append(args, limit+1, offset)
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	defer rows.Close()
	items := make([]api.Message, 0, limit)
	for rows.Next() {
		row, err := scanMessageRow(rows)
		if err != nil {
			s.writeAppError(w, r, internalError(err))
			return
		}
		items = append(items, messageToAPI(row))
	}
	response := api.CollectionResponse[api.Message]{Items: items}
	if len(items) > limit {
		response.Items = response.Items[:limit]
		response.NextCursor = formatNextCursor(offset + limit)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureAgentSafeWrite(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationAccess(r.Context(), auth, conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.CreateMessageRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	request.BodyText = strings.TrimSpace(request.BodyText)
	if request.BodyText == "" && len(request.BodyRich) == 0 {
		s.writeAppError(w, r, validationFailed("body_text", "required", "body_text is required when body_rich is omitted."))
		return
	}
	now := time.Now().UTC()
	messageID := uuid.New()
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		bodyRich, _ := json.Marshal(request.BodyRich)
		metadata, _ := json.Marshal(request.Metadata)
		txQueries := s.queries.WithTx(tx)
		if err := txQueries.CreateMessage(r.Context(), dbsqlc.CreateMessageParams{
			ID:             messageID,
			ConversationID: conversationID,
			AuthorUserID:   auth.UserID,
			BodyText:       request.BodyText,
			BodyRich:       dbsqlc.RawMessagePtr(bodyRich),
			Metadata:       dbsqlc.RawMessagePtr(metadata),
			CreatedAt:      dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		if err := txQueries.TouchConversationLastMessage(r.Context(), dbsqlc.TouchConversationLastMessageParams{
			UpdatedAt: dbsqlc.Timestamptz(now),
			ID:        conversationID,
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "message.posted", "message", messageID, conversation.WorkspaceID, &actor, map[string]any{
			"message_id":      messageID.String(),
			"conversation_id": conversationID.String(),
			"author_user_id":  auth.UserID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	message, err := s.loadMessage(r.Context(), messageID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusCreated, messageToAPI(message))
}

func (s *Server) handlePatchMessage(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	messageID, err := parseUUIDPath(r, "message_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	message, err := s.loadMessage(r.Context(), messageID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Message not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if message.AuthorUserID != auth.UserID {
		s.writeAppError(w, r, forbidden("Only the original author may edit this message."))
		return
	}
	var request api.UpdateMessageRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	request.BodyText = strings.TrimSpace(request.BodyText)
	if request.BodyText == "" && len(request.BodyRich) == 0 {
		s.writeAppError(w, r, validationFailed("body_text", "required", "body_text is required when body_rich is omitted."))
		return
	}
	conversation, err := s.loadConversation(r.Context(), message.ConversationID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	now := time.Now().UTC()
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		bodyRich, _ := json.Marshal(request.BodyRich)
		metadata, _ := json.Marshal(request.Metadata)
		if err := s.queries.WithTx(tx).UpdateMessageContent(r.Context(), dbsqlc.UpdateMessageContentParams{
			BodyText: request.BodyText,
			BodyRich: dbsqlc.RawMessagePtr(bodyRich),
			Metadata: dbsqlc.RawMessagePtr(metadata),
			EditedAt: dbsqlc.Timestamptz(now),
			ID:       messageID,
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "message.updated", "message", messageID, conversation.WorkspaceID, &actor, map[string]any{
			"message_id":      messageID.String(),
			"conversation_id": message.ConversationID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	updated, err := s.loadMessage(r.Context(), messageID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, messageToAPI(updated))
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	messageID, err := parseUUIDPath(r, "message_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	message, err := s.loadMessage(r.Context(), messageID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Message not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if message.AuthorUserID != auth.UserID {
		s.writeAppError(w, r, forbidden("Only the original author may delete this message."))
		return
	}
	conversation, err := s.loadConversation(r.Context(), message.ConversationID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).SoftDeleteMessage(r.Context(), dbsqlc.SoftDeleteMessageParams{
			DeletedAt: dbsqlc.Timestamptz(time.Now().UTC()),
			ID:        messageID,
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "message.deleted", "message", messageID, conversation.WorkspaceID, &actor, map[string]any{
			"message_id":      messageID.String(),
			"conversation_id": message.ConversationID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePutReadState(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureAgentSafeWrite(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	conversationID, err := parseUUIDPath(r, "conversation_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	conversation, err := s.loadConversation(r.Context(), conversationID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Conversation not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureConversationAccess(r.Context(), auth, conversation); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.UpdateReadStateRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	messageID, err := uuid.Parse(strings.TrimSpace(request.LastReadMessageID))
	if err != nil {
		s.writeAppError(w, r, validationFailed("last_read_message_id", "invalid_uuid", "Must be a valid UUID."))
		return
	}
	exists, err := s.queries.MessageExistsInConversation(r.Context(), dbsqlc.MessageExistsInConversationParams{
		MessageID:      messageID,
		ConversationID: conversationID,
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	if !exists {
		s.writeAppError(w, r, validationFailed("last_read_message_id", "invalid_value", "Message does not belong to the conversation."))
		return
	}
	now := time.Now().UTC()
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).UpsertConversationRead(r.Context(), dbsqlc.UpsertConversationReadParams{
			ConversationID:    conversationID,
			UserID:            auth.UserID,
			LastReadMessageID: &messageID,
			LastReadAt:        dbsqlc.Timestamptz(now),
			UpdatedAt:         dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "conversation.read.updated", "conversation", conversationID, conversation.WorkspaceID, &actor, map[string]any{
			"conversation_id":      conversationID.String(),
			"user_id":              auth.UserID.String(),
			"last_read_message_id": messageID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	limit, appErr := parseLimitQuery(r.URL.Query().Get("limit"), 100, 200)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	cursorID, appErr := parseQueryInt64Cursor(r.URL.Query().Get("cursor"))
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	resourceID := strings.TrimSpace(r.URL.Query().Get("resource_id"))
	if appErr := validateExternalEventResourceType("resource_type", resourceType); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	workspaceRaw := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	var workspaceID *uuid.UUID
	if workspaceRaw != "" {
		parsed, err := uuid.Parse(workspaceRaw)
		if err != nil {
			s.writeAppError(w, r, validationFailed("workspace_id", "invalid_uuid", "Must be a valid UUID."))
			return
		}
		workspaceID = &parsed
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, parsed); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}
	if auth.APIKeyWorkspaceID != nil {
		if workspaceID == nil || *workspaceID != *auth.APIKeyWorkspaceID {
			s.writeAppError(w, r, forbidden("This API key can only access events in its workspace."))
			return
		}
	}
	if resourceID != "" && resourceType == "" {
		s.writeAppError(w, r, validationFailed("resource_type", "required", "resource_id requires resource_type."))
		return
	}
	var resourceUUID *uuid.UUID
	if resourceID != "" {
		parsed, err := uuid.Parse(resourceID)
		if err != nil {
			s.writeAppError(w, r, validationFailed("resource_id", "invalid_uuid", "Must be a valid UUID."))
			return
		}
		resourceUUID = &parsed
	}
	visibility := repository.ExternalEventVisibilityPredicate("ee", "$1")
	rows, err := s.db.Query(
		r.Context(),
		fmt.Sprintf(`select ee.id, ee.sequence_id, ee.workspace_id, ee.type, ee.resource_type, ee.resource_id, ee.occurred_at, ee.payload
		from external_events ee
		where %s
		  and ee.sequence_id > $2
		  and ($3::uuid is null or ee.workspace_id = $3)
		  and ($4::text = '' or ee.type = $4)
		  and ($5::text = '' or ee.resource_type = $5)
		  and ($6::uuid is null or ee.resource_id = $6)
		order by ee.sequence_id asc
		limit $7`, visibility),
		auth.UserID,
		cursorID,
		workspaceID,
		typeFilter,
		resourceType,
		resourceUUID,
		limit+1,
	)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	defer rows.Close()

	items := make([]api.ExternalEvent, 0, limit)
	var lastSeenSequenceID int64
	for rows.Next() {
		var item api.ExternalEvent
		var eventID uuid.UUID
		var sequenceID int64
		var workspace *uuid.UUID
		var resourceUUID uuid.UUID
		var payload []byte
		var occurredAt time.Time
		if err := rows.Scan(&eventID, &sequenceID, &workspace, &item.Type, &item.ResourceType, &resourceUUID, &occurredAt, &payload); err != nil {
			s.writeAppError(w, r, internalError(err))
			return
		}
		lastSeenSequenceID = sequenceID
		if len(items) >= limit {
			continue
		}
		item.ID = eventID.String()
		item.WorkspaceID = uuidPtrToStringPtr(workspace)
		item.ResourceID = resourceUUID.String()
		item.OccurredAt = occurredAt.Format(time.RFC3339)
		item.Payload = readJSONMap(payload)
		items = append(items, item)
	}
	response := api.CollectionResponse[api.ExternalEvent]{Items: items}
	if lastSeenSequenceID > 0 && len(items) == limit {
		response.NextCursor = formatInt64Cursor(lastSeenSequenceID)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListEventSubscriptions(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var workspaceID *uuid.UUID
	if auth.APIKeyWorkspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *auth.APIKeyWorkspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
		workspaceID = auth.APIKeyWorkspaceID
	}
	rows, err := s.queries.ListEventSubscriptionsByOwner(r.Context(), dbsqlc.ListEventSubscriptionsByOwnerParams{
		OwnerUserID: auth.UserID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	items := make([]api.EventSubscription, 0)
	for _, row := range rows {
		items = append(items, eventSubscriptionListRowToAPI(row))
	}
	writeJSON(w, http.StatusOK, api.CollectionResponse[api.EventSubscription]{Items: items})
}

func (s *Server) handleCreateEventSubscription(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.CreateEventSubscriptionRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if err := expectURL(request.URL, "url"); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	secret := strings.TrimSpace(request.Secret)
	if secret == "" {
		s.writeAppError(w, r, validationFailed("secret", "required", "Must not be empty."))
		return
	}
	eventType := trimOptionalString(request.EventType)
	resourceType := trimOptionalString(request.ResourceType)
	if resourceType != nil {
		if appErr := validateExternalEventResourceType("resource_type", *resourceType); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}
	workspaceID, appErr := parseOptionalUUID(request.WorkspaceID)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	resourceUUID, appErr := parseOptionalUUID(request.ResourceID)
	if appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	if resourceUUID != nil && resourceType == nil {
		s.writeAppError(w, r, validationFailed("resource_type", "required", "resource_id requires resource_type."))
		return
	}
	if auth.APIKeyWorkspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *auth.APIKeyWorkspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
		if workspaceID == nil {
			workspaceID = auth.APIKeyWorkspaceID
		} else if *workspaceID != *auth.APIKeyWorkspaceID {
			s.writeAppError(w, r, forbidden("This API key can only manage event subscriptions in its workspace."))
			return
		}
	}
	if workspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *workspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}
	if resourceType != nil && resourceUUID != nil {
		if appErr := s.ensureSubscriptionResourceVisible(r.Context(), auth, *resourceType, *resourceUUID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}
	encryptedSecret, err := s.protector.EncryptString(r.Context(), secret)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	now := time.Now().UTC()
	subscriptionID := uuid.New()
	if err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).CreateEventSubscription(r.Context(), dbsqlc.CreateEventSubscriptionParams{
			ID:              subscriptionID,
			OwnerUserID:     auth.UserID,
			WorkspaceID:     workspaceID,
			Url:             request.URL,
			EncryptedSecret: encryptedSecret,
			EventType:       eventType,
			ResourceType:    resourceType,
			ResourceID:      resourceUUID,
			CreatedAt:       dbsqlc.Timestamptz(now),
			UpdatedAt:       dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "event_subscription.created", "event_subscription", subscriptionID, workspaceID, &actor, map[string]any{
			"event_subscription_id": subscriptionID.String(),
			"workspace_id":          uuidPtrToStringPtr(workspaceID),
			"resource_type":         resourceType,
			"resource_id":           uuidPtrToStringPtr(resourceUUID),
			"event_type":            eventType,
		})
	}); err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusCreated, api.EventSubscription{
		ID:           subscriptionID.String(),
		WorkspaceID:  uuidPtrToStringPtr(workspaceID),
		URL:          request.URL,
		Enabled:      true,
		EventType:    eventType,
		ResourceType: resourceType,
		ResourceID:   uuidPtrToStringPtr(resourceUUID),
		CreatedAt:    now.Format(time.RFC3339),
		UpdatedAt:    now.Format(time.RFC3339),
	})
}

func (s *Server) handlePatchEventSubscription(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	subscriptionID, err := parseUUIDPath(r, "subscription_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	var request api.UpdateEventSubscriptionRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if request.Enabled == nil {
		s.writeAppError(w, r, validationFailed("enabled", "required", "enabled is required."))
		return
	}
	var subscriptionWorkspaceID *uuid.UUID
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		subscriptionWorkspaceID, err = txQueries.GetEventSubscriptionWorkspaceForOwnerForUpdate(r.Context(), dbsqlc.GetEventSubscriptionWorkspaceForOwnerForUpdateParams{
			ID:          subscriptionID,
			OwnerUserID: auth.UserID,
		})
		if err != nil {
			if err == pgx.ErrNoRows {
				return notFound("Event subscription not found.")
			}
			return err
		}
		if auth.APIKeyWorkspaceID != nil {
			if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *auth.APIKeyWorkspaceID); appErr != nil {
				return appErr
			}
			if subscriptionWorkspaceID == nil || *subscriptionWorkspaceID != *auth.APIKeyWorkspaceID {
				return forbidden("This API key cannot access that resource.")
			}
		}
		rowsAffected, err := txQueries.UpdateEventSubscriptionEnabledByOwner(r.Context(), dbsqlc.UpdateEventSubscriptionEnabledByOwnerParams{
			ID:          subscriptionID,
			OwnerUserID: auth.UserID,
			Enabled:     *request.Enabled,
			UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return notFound("Event subscription not found.")
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "event_subscription.updated", "event_subscription", subscriptionID, subscriptionWorkspaceID, &actor, map[string]any{
			"event_subscription_id": subscriptionID.String(),
			"enabled":               *request.Enabled,
		})
	})
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	row, err := s.queries.GetEventSubscriptionByIDAndOwner(r.Context(), dbsqlc.GetEventSubscriptionByIDAndOwnerParams{
		ID:          subscriptionID,
		OwnerUserID: auth.UserID,
		WorkspaceID: auth.APIKeyWorkspaceID,
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, eventSubscriptionGetRowToAPI(row))
}

func (s *Server) handleDeleteEventSubscription(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanActor(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	subscriptionID, err := parseUUIDPath(r, "subscription_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	var subscriptionWorkspaceID *uuid.UUID
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		subscriptionWorkspaceID, err = txQueries.GetEventSubscriptionWorkspaceForOwnerForUpdate(r.Context(), dbsqlc.GetEventSubscriptionWorkspaceForOwnerForUpdateParams{
			ID:          subscriptionID,
			OwnerUserID: auth.UserID,
		})
		if err != nil {
			if err == pgx.ErrNoRows {
				return notFound("Event subscription not found.")
			}
			return err
		}
		if auth.APIKeyWorkspaceID != nil {
			if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *auth.APIKeyWorkspaceID); appErr != nil {
				return appErr
			}
			if subscriptionWorkspaceID == nil || *subscriptionWorkspaceID != *auth.APIKeyWorkspaceID {
				return forbidden("This API key cannot access that resource.")
			}
		}
		rowsAffected, err := txQueries.DeleteEventSubscriptionByOwner(r.Context(), dbsqlc.DeleteEventSubscriptionByOwnerParams{
			ID:          subscriptionID,
			OwnerUserID: auth.UserID,
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return notFound("Event subscription not found.")
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "event_subscription.deleted", "event_subscription", subscriptionID, subscriptionWorkspaceID, &actor, map[string]any{
			"event_subscription_id": subscriptionID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type conversationPermissionAction string

const (
	conversationPermissionUpdateMetadata   conversationPermissionAction = "update_metadata"
	conversationPermissionInviteMembers    conversationPermissionAction = "invite_members"
	conversationPermissionRemoveMembers    conversationPermissionAction = "remove_members"
	conversationPermissionManageShareLinks conversationPermissionAction = "manage_share_links"
)

type conversationPermissionRule string

const (
	conversationPermissionRuleAnyParticipant          conversationPermissionRule = "any_participant"
	conversationPermissionRuleCreatorOnly             conversationPermissionRule = "creator_only"
	conversationPermissionRuleWorkspaceAdminOrCreator conversationPermissionRule = "workspace_admin_or_creator"
)

type conversationPermissionSet struct {
	UpdateMetadata   conversationPermissionRule
	InviteMembers    conversationPermissionRule
	RemoveMembers    conversationPermissionRule
	ManageShareLinks conversationPermissionRule
}

type conversationPermissionPolicy struct {
	Global    conversationPermissionSet
	Workspace conversationPermissionSet
}

// Conversation permissions are action-specific so invite/member rules can evolve
// independently from metadata, removal, or share-link management.
var defaultConversationPermissionPolicy = conversationPermissionPolicy{
	Global: conversationPermissionSet{
		UpdateMetadata:   conversationPermissionRuleCreatorOnly,
		InviteMembers:    conversationPermissionRuleAnyParticipant,
		RemoveMembers:    conversationPermissionRuleCreatorOnly,
		ManageShareLinks: conversationPermissionRuleCreatorOnly,
	},
	Workspace: conversationPermissionSet{
		UpdateMetadata:   conversationPermissionRuleWorkspaceAdminOrCreator,
		InviteMembers:    conversationPermissionRuleAnyParticipant,
		RemoveMembers:    conversationPermissionRuleWorkspaceAdminOrCreator,
		ManageShareLinks: conversationPermissionRuleWorkspaceAdminOrCreator,
	},
}

func (s *Server) ensureConversationPermission(ctx context.Context, auth domain.AuthContext, conversation conversationRow, action conversationPermissionAction) *appError {
	if appErr := s.ensureConversationAccess(ctx, auth, conversation); appErr != nil {
		return appErr
	}
	switch conversationPermissionRuleFor(conversation, action) {
	case conversationPermissionRuleAnyParticipant:
		return nil
	case conversationPermissionRuleCreatorOnly:
		if conversation.CreatedByUserID == auth.UserID {
			return nil
		}
	case conversationPermissionRuleWorkspaceAdminOrCreator:
		if conversation.WorkspaceID != nil {
			role, appErr := s.ensureWorkspaceActiveMember(ctx, auth, *conversation.WorkspaceID)
			if appErr != nil {
				return appErr
			}
			if role == "owner" || role == "admin" || conversation.CreatedByUserID == auth.UserID {
				return nil
			}
			break
		}
		if conversation.CreatedByUserID == auth.UserID {
			return nil
		}
	default:
		return forbidden("This conversation action is not allowed.")
	}
	return forbidden(conversationPermissionDeniedMessage(action))
}

func conversationPermissionRuleFor(conversation conversationRow, action conversationPermissionAction) conversationPermissionRule {
	rules := defaultConversationPermissionPolicy.Global
	if conversation.WorkspaceID != nil {
		rules = defaultConversationPermissionPolicy.Workspace
	}
	switch action {
	case conversationPermissionUpdateMetadata:
		return rules.UpdateMetadata
	case conversationPermissionInviteMembers:
		return rules.InviteMembers
	case conversationPermissionRemoveMembers:
		return rules.RemoveMembers
	case conversationPermissionManageShareLinks:
		return rules.ManageShareLinks
	default:
		return ""
	}
}

func conversationPermissionDeniedMessage(action conversationPermissionAction) string {
	switch action {
	case conversationPermissionInviteMembers:
		return "You do not have access to invite members to this conversation."
	case conversationPermissionRemoveMembers:
		return "You do not have access to remove members from this conversation."
	case conversationPermissionManageShareLinks:
		return "You do not have access to manage share links for this conversation."
	default:
		return "You do not have access to manage this conversation."
	}
}

func (s *Server) ensureUsersExistTx(ctx context.Context, tx pgx.Tx, userIDs []uuid.UUID) error {
	if len(userIDs) == 0 {
		return nil
	}
	count, err := dbsqlc.New(tx).CountActiveUsersByIDs(ctx, userIDs)
	if err != nil {
		return err
	}
	if int(count) != len(userIDs) {
		return validationFailed("participant_user_ids", "invalid_value", "One or more users do not exist or are inactive.")
	}
	return nil
}

func (s *Server) ensureWorkspaceMembersTx(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, userIDs []uuid.UUID) error {
	if len(userIDs) == 0 {
		return nil
	}
	count, err := dbsqlc.New(tx).CountActiveWorkspaceMembersByIDs(ctx, dbsqlc.CountActiveWorkspaceMembersByIDsParams{
		WorkspaceID: workspaceID,
		Column2:     userIDs,
	})
	if err != nil {
		return err
	}
	if int(count) != len(userIDs) {
		return validationFailed("participant_user_ids", "invalid_value", "All users must be active members of the workspace.")
	}
	return nil
}

func canonicalPair(left uuid.UUID, right uuid.UUID) (uuid.UUID, uuid.UUID) {
	if left.String() < right.String() {
		return left, right
	}
	return right, left
}

func normalizeParticipants(rawIDs []string, implicit ...uuid.UUID) ([]uuid.UUID, *appError) {
	seen := map[uuid.UUID]struct{}{}
	participants := make([]uuid.UUID, 0, len(rawIDs)+len(implicit))
	for _, implicitID := range implicit {
		seen[implicitID] = struct{}{}
		participants = append(participants, implicitID)
	}
	for _, rawID := range rawIDs {
		value := strings.TrimSpace(rawID)
		if value == "" {
			continue
		}
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, validationFailed("participant_user_ids", "invalid_uuid", "All participant_user_ids must be valid UUIDs.")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		participants = append(participants, id)
	}
	return participants, nil
}

func scanConversationRow(rows pgx.Rows) (conversationRow, error) {
	var row conversationRow
	err := rows.Scan(
		&row.ID,
		&row.WorkspaceID,
		&row.AccessPolicy,
		&row.Title,
		&row.Description,
		&row.CreatedByUserID,
		&row.ArchivedAt,
		&row.LastMessageAt,
		&row.CreatedAt,
		&row.UpdatedAt,
		&row.ParticipantCount,
	)
	return row, err
}

func scanMessageRow(rows pgx.Rows) (messageRow, error) {
	var row messageRow
	var bodyRich, metadata []byte
	err := rows.Scan(
		&row.ID,
		&row.ConversationID,
		&row.AuthorUserID,
		&row.BodyText,
		&bodyRich,
		&metadata,
		&row.EditedAt,
		&row.DeletedAt,
		&row.CreatedAt,
	)
	row.BodyRich = readJSONMap(bodyRich)
	row.Metadata = readJSONMap(metadata)
	return row, err
}

func (s *Server) loadMessage(ctx context.Context, messageID uuid.UUID) (messageRow, error) {
	row, err := s.queries.GetMessage(ctx, messageID)
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

func eventSubscriptionListRowToAPI(row dbsqlc.ListEventSubscriptionsByOwnerRow) api.EventSubscription {
	return api.EventSubscription{
		ID:           row.ID.String(),
		WorkspaceID:  uuidPtrToStringPtr(row.WorkspaceID),
		URL:          row.Url,
		Enabled:      row.Enabled,
		EventType:    row.EventType,
		ResourceType: row.ResourceType,
		ResourceID:   uuidPtrToStringPtr(row.ResourceID),
		CreatedAt:    dbsqlc.TimeValue(row.CreatedAt).Format(time.RFC3339),
		UpdatedAt:    dbsqlc.TimeValue(row.UpdatedAt).Format(time.RFC3339),
	}
}

func eventSubscriptionGetRowToAPI(row dbsqlc.GetEventSubscriptionByIDAndOwnerRow) api.EventSubscription {
	return api.EventSubscription{
		ID:           row.ID.String(),
		WorkspaceID:  uuidPtrToStringPtr(row.WorkspaceID),
		URL:          row.Url,
		Enabled:      row.Enabled,
		EventType:    row.EventType,
		ResourceType: row.ResourceType,
		ResourceID:   uuidPtrToStringPtr(row.ResourceID),
		CreatedAt:    dbsqlc.TimeValue(row.CreatedAt).Format(time.RFC3339),
		UpdatedAt:    dbsqlc.TimeValue(row.UpdatedAt).Format(time.RFC3339),
	}
}

func uuidSliceToStrings(values []uuid.UUID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.String())
	}
	return result
}

func stringSliceFromJSON(raw []byte) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

func (s *Server) ensureSubscriptionResourceVisible(ctx context.Context, auth domain.AuthContext, resourceType string, resourceID uuid.UUID) *appError {
	switch resourceType {
	case "workspace":
		_, appErr := s.ensureWorkspaceActiveMember(ctx, auth, resourceID)
		return appErr
	case "conversation":
		conversationID := resourceID
		conversation, err := s.loadConversation(ctx, conversationID)
		if err != nil {
			if err == pgx.ErrNoRows {
				return notFound("Conversation not found.")
			}
			return internalError(err)
		}
		return s.ensureConversationAccess(ctx, auth, conversation)
	case "user":
		if resourceID != auth.UserID {
			return forbidden("You do not have access to subscribe to another user's events.")
		}
		return nil
	default:
		return validationFailed("resource_type", "invalid_value", "Unsupported resource_type.")
	}
}

func validateExternalEventResourceType(field string, value string) *appError {
	if value == "" {
		return nil
	}
	switch value {
	case "conversation", "user", "workspace":
		return nil
	default:
		return validationFailed(field, "invalid_value", "Must be one of: conversation, user, workspace.")
	}
}
