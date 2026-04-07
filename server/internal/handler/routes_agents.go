package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnsuh/teraslack/server/internal/api"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/domain"
)

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	rows, err := s.queries.ListAgentsManagedByUser(r.Context(), &auth.UserID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	items := make([]api.Agent, 0, len(rows))
	for _, row := range rows {
		items = append(items, agentToAPI(userRow{
			ID:            row.UserID,
			PrincipalType: row.PrincipalType,
			Status:        row.Status,
			Email:         row.Email,
			Handle:        row.Handle,
			DisplayName:   row.DisplayName,
			AvatarURL:     row.AvatarUrl,
			Bio:           row.Bio,
		}, agentRow{
			UserID:           row.UserID,
			OwnerUserID:      row.OwnerUserID,
			OwnerWorkspaceID: row.OwnerWorkspaceID,
			Mode:             row.Mode,
			CreatedByUserID:  row.CreatedByUserID,
			CreatedAt:        dbsqlc.TimeValue(row.CreatedAt),
			UpdatedAt:        dbsqlc.TimeValue(row.UpdatedAt),
		}))
	}
	writeJSON(w, http.StatusOK, api.CollectionResponse[api.Agent]{Items: items})
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.CreateAgentRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.OwnerType = strings.TrimSpace(request.OwnerType)
	request.Mode = strings.TrimSpace(request.Mode)
	if request.DisplayName == "" {
		s.writeAppError(w, r, validationFailed("display_name", "required", "Display name is required."))
		return
	}
	switch request.OwnerType {
	case "user", "workspace":
	default:
		s.writeAppError(w, r, validationFailed("owner_type", "invalid_value", "Must be user or workspace."))
		return
	}
	if request.Mode == "" {
		request.Mode = "safe_write"
	}
	switch request.Mode {
	case "read_only", "safe_write":
	default:
		s.writeAppError(w, r, validationFailed("mode", "invalid_value", "Must be read_only or safe_write."))
		return
	}
	if request.Handle != nil && strings.TrimSpace(*request.Handle) == "" {
		s.writeAppError(w, r, validationFailed("handle", "invalid_format", "Handle cannot be empty."))
		return
	}
	ownerWorkspaceID, err := parseOptionalUUID(request.OwnerWorkspaceID)
	if err != nil {
		s.writeAppError(w, r, validationFailed("owner_workspace_id", "invalid_uuid", "Must be a valid UUID."))
		return
	}
	if request.OwnerType == "user" && ownerWorkspaceID != nil {
		s.writeAppError(w, r, validationFailed("owner_workspace_id", "invalid_value", "User-owned agents must omit owner_workspace_id."))
		return
	}
	if request.OwnerType == "workspace" && ownerWorkspaceID == nil {
		s.writeAppError(w, r, validationFailed("owner_workspace_id", "required", "Workspace-owned agents require owner_workspace_id."))
		return
	}
	avatarURL := trimOptionalString(request.AvatarURL)
	if avatarURL != nil {
		if appErr := expectURL(*avatarURL, "avatar_url"); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}
	bio := trimOptionalString(request.Bio)
	var ownerUserID *uuid.UUID
	if request.OwnerType == "user" {
		ownerUserID = &auth.UserID
	}
	if ownerWorkspaceID != nil {
		if appErr := s.ensureWorkspaceAdmin(r.Context(), auth, *ownerWorkspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}

	var createdUser userRow
	var createdAgent agentRow
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		user, err := s.insertAgentWithProfile(r.Context(), tx, request.DisplayName, request.Handle, avatarURL, bio)
		if err != nil {
			return err
		}
		if err := s.queries.WithTx(tx).CreateAgent(r.Context(), dbsqlc.CreateAgentParams{
			UserID:           user.ID,
			OwnerUserID:      ownerUserID,
			OwnerWorkspaceID: ownerWorkspaceID,
			Mode:             request.Mode,
			CreatedByUserID:  auth.UserID,
			CreatedAt:        dbsqlc.Timestamptz(now),
			UpdatedAt:        dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		var membershipID uuid.UUID
		if ownerWorkspaceID != nil {
			membershipID = uuid.New()
			if err := s.queries.WithTx(tx).CreateWorkspaceMembership(r.Context(), dbsqlc.CreateWorkspaceMembershipParams{
				ID:          membershipID,
				WorkspaceID: *ownerWorkspaceID,
				UserID:      user.ID,
				Role:        "member",
				Status:      "active",
				JoinedAt:    dbsqlc.Timestamptz(now),
				CreatedAt:   dbsqlc.Timestamptz(now),
				UpdatedAt:   dbsqlc.Timestamptz(now),
			}); err != nil {
				return err
			}
		}
		actor := auth.UserID
		if err := s.appendEvent(r.Context(), tx, "user.created", "user", user.ID, ownerWorkspaceID, &actor, map[string]any{
			"user_id":        user.ID.String(),
			"principal_type": "agent",
			"owner_type":     request.OwnerType,
		}); err != nil {
			return err
		}
		if ownerWorkspaceID != nil {
			if err := s.appendEvent(r.Context(), tx, "workspace.membership.added", "workspace_membership", membershipID, ownerWorkspaceID, &actor, map[string]any{
				"workspace_id": ownerWorkspaceID.String(),
				"user_id":      user.ID.String(),
				"role":         "member",
			}); err != nil {
				return err
			}
		}
		createdUser = user
		createdAgent = agentRow{
			UserID:           user.ID,
			OwnerUserID:      ownerUserID,
			OwnerWorkspaceID: ownerWorkspaceID,
			Mode:             request.Mode,
			CreatedByUserID:  auth.UserID,
			CreatedAt:        now,
			UpdatedAt:        now,
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
	writeJSON(w, http.StatusCreated, agentToAPI(createdUser, createdAgent))
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	agentUserID, err := parseUUIDPath(r, "agent_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	agent, err := s.loadAgent(r.Context(), agentUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.writeAppError(w, r, notFound("Agent not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureAgentManager(r.Context(), auth, agent); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	user, err := s.loadUser(r.Context(), agentUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.writeAppError(w, r, notFound("Agent not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, agentToAPI(user, agent))
}

func (s *Server) handlePatchAgent(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureHumanGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	agentUserID, err := parseUUIDPath(r, "agent_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	agent, err := s.loadAgent(r.Context(), agentUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.writeAppError(w, r, notFound("Agent not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	if appErr := s.ensureAgentManager(r.Context(), auth, agent); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	user, err := s.loadUser(r.Context(), agentUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.writeAppError(w, r, notFound("Agent not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}

	var request api.UpdateAgentRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if request.DisplayName == nil && request.Handle == nil && request.AvatarURL == nil && request.Bio == nil && request.Mode == nil && request.Status == nil {
		s.writeAppError(w, r, validationFailed("body", "required", "At least one field must be provided."))
		return
	}
	if request.Handle != nil && strings.TrimSpace(*request.Handle) == "" {
		s.writeAppError(w, r, validationFailed("handle", "invalid_format", "Handle cannot be empty."))
		return
	}
	if request.DisplayName != nil && strings.TrimSpace(*request.DisplayName) == "" {
		s.writeAppError(w, r, validationFailed("display_name", "invalid_format", "Display name cannot be empty."))
		return
	}
	if request.Mode != nil {
		mode := strings.TrimSpace(*request.Mode)
		switch mode {
		case "read_only", "safe_write":
			request.Mode = &mode
		default:
			s.writeAppError(w, r, validationFailed("mode", "invalid_value", "Must be read_only or safe_write."))
			return
		}
	}
	if request.Status != nil {
		status := strings.TrimSpace(*request.Status)
		switch status {
		case "active", "suspended":
			request.Status = &status
		default:
			s.writeAppError(w, r, validationFailed("status", "invalid_value", "Must be active or suspended."))
			return
		}
	}
	handle := user.Handle
	if request.Handle != nil {
		handle = strings.TrimSpace(*request.Handle)
	}
	displayName := user.DisplayName
	if request.DisplayName != nil {
		displayName = strings.TrimSpace(*request.DisplayName)
	}
	avatarURL := user.AvatarURL
	if request.AvatarURL != nil {
		avatarURL = trimOptionalString(request.AvatarURL)
		if avatarURL != nil {
			if appErr := expectURL(*avatarURL, "avatar_url"); appErr != nil {
				s.writeAppError(w, r, appErr)
				return
			}
		}
	}
	bio := user.Bio
	if request.Bio != nil {
		bio = trimOptionalString(request.Bio)
	}

	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		if err := s.queries.WithTx(tx).UpdateUserProfile(r.Context(), dbsqlc.UpdateUserProfileParams{
			UserID:      agentUserID,
			Handle:      &handle,
			DisplayName: &displayName,
			AvatarUrl:   avatarURL,
			Bio:         bio,
			UpdatedAt:   dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		if request.Status != nil {
			if err := s.queries.WithTx(tx).UpdateUserStatus(r.Context(), dbsqlc.UpdateUserStatusParams{
				ID:        agentUserID,
				Status:    *request.Status,
				UpdatedAt: dbsqlc.Timestamptz(now),
			}); err != nil {
				return err
			}
			user.Status = *request.Status
		}
		if err := s.queries.WithTx(tx).UpdateAgent(r.Context(), dbsqlc.UpdateAgentParams{
			UserID:    agentUserID,
			Mode:      request.Mode,
			UpdatedAt: dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		user.Handle = handle
		user.DisplayName = displayName
		user.AvatarURL = avatarURL
		user.Bio = bio
		if request.Mode != nil {
			agent.Mode = *request.Mode
		}
		agent.UpdatedAt = now
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "user.updated", "user", agentUserID, agent.OwnerWorkspaceID, &actor, map[string]any{
			"user_id": agentUserID.String(),
			"status":  user.Status,
			"mode":    agent.Mode,
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
	writeJSON(w, http.StatusOK, agentToAPI(user, agent))
}
