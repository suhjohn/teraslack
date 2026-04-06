package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnsuh/teraslack/server/internal/api"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/domain"
)

func (s *Server) handleStartEmailLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ResendAPIKey == "" || s.cfg.AuthEmailFrom == "" {
		s.writeAppError(w, r, notConfigured("email_auth_not_configured", "Email login is not configured."))
		return
	}
	var request api.StartEmailLoginRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	email := normalizeEmail(request.Email)
	if !strings.Contains(email, "@") {
		s.writeAppError(w, r, validationFailed("email", "invalid_format", "Must be a valid email address."))
		return
	}
	if !s.limiter.allow("auth:email:start:ip:"+clientIP(r), 20, time.Hour) {
		s.writeAppError(w, r, rateLimited("Too many email login attempts from this IP."))
		return
	}
	if !s.limiter.allow("auth:email:start:email:"+email, 5, time.Hour) {
		s.writeAppError(w, r, rateLimited("Too many email login attempts for this address."))
		return
	}
	code, err := s.createEmailLoginChallenge(r.Context(), email)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	if err := s.sendEmailLoginCode(r.Context(), email, code); err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusAccepted, api.GenericStatusResponse{Status: "ok"})
}

func (s *Server) handleVerifyEmailLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow("auth:email:verify:ip:"+clientIP(r), 30, time.Hour) {
		s.writeAppError(w, r, rateLimited("Too many verification attempts from this IP."))
		return
	}
	var request api.VerifyEmailLoginRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	email := normalizeEmail(request.Email)
	if !strings.Contains(email, "@") {
		s.writeAppError(w, r, validationFailed("email", "invalid_format", "Must be a valid email address."))
		return
	}
	code := strings.TrimSpace(request.Code)
	if len(code) < 4 {
		s.writeAppError(w, r, validationFailed("code", "invalid_format", "Must be a valid verification code."))
		return
	}

	var response api.AuthResponse
	var sessionToken string
	var sessionID uuid.UUID
	err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		now := time.Now().UTC()
		challenge, err := txQueries.GetEmailLoginChallengeForVerification(r.Context(), dbsqlc.GetEmailLoginChallengeForVerificationParams{
			Email:    email,
			CodeHash: teracrypto.SHA256Hex(code),
		})
		if err != nil {
			if err == pgx.ErrNoRows {
				return unauthorized("Invalid verification code.")
			}
			return err
		}
		expiresAt := dbsqlc.TimeValue(challenge.ExpiresAt)
		if expiresAt.Before(now) {
			return unauthorized("Verification code expired.")
		}
		if err := txQueries.ConsumeEmailLoginChallenge(r.Context(), dbsqlc.ConsumeEmailLoginChallengeParams{
			ConsumedAt: dbsqlc.Timestamptz(now),
			ID:         challenge.ID,
		}); err != nil {
			return err
		}

		user, created, err := s.resolveOrCreateUserByEmailTx(r.Context(), tx, email)
		if err != nil {
			return err
		}
		if created {
			userID := user.ID
			if err := s.appendEvent(r.Context(), tx, "user.created", "user", user.ID, nil, &userID, map[string]any{
				"user_id": user.ID.String(),
				"email":   email,
			}); err != nil {
				return err
			}
		}

		sessionID, sessionToken, response.Session, err = s.createSessionTx(r.Context(), tx, user.ID)
		if err != nil {
			return err
		}
		userID := user.ID
		if err := s.appendEvent(r.Context(), tx, "auth.session.created", "auth_session", sessionID, nil, &userID, map[string]any{
			"session_id": sessionID.String(),
			"user_id":    user.ID.String(),
		}); err != nil {
			return err
		}
		response.User = userToAPI(user)
		return nil
	})
	if err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
		} else {
			s.writeAppError(w, r, internalError(err))
		}
		return
	}

	s.setSessionCookie(w, sessionToken)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStartGoogleOAuth(w http.ResponseWriter, r *http.Request) {
	s.handleOAuthStart("google", s.cfg.GoogleOAuthClientID, s.cfg.BaseURL+"/auth/oauth/google/callback", w, r)
}

func (s *Server) handleGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	s.handleOAuthCallback("google", w, r)
}

func (s *Server) handleStartGitHubOAuth(w http.ResponseWriter, r *http.Request) {
	s.handleOAuthStart("github", s.cfg.GitHubOAuthClientID, s.cfg.BaseURL+"/auth/oauth/github/callback", w, r)
}

func (s *Server) handleGitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	s.handleOAuthCallback("github", w, r)
}

func (s *Server) handleDeleteCurrentSession(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if auth.SessionID == nil {
		s.writeAppError(w, r, forbidden("Current auth credential is not a session."))
		return
	}
	err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		rowsAffected, err := s.queries.WithTx(tx).RevokeAuthSession(r.Context(), dbsqlc.RevokeAuthSessionParams{
			RevokedAt: dbsqlc.Timestamptz(time.Now().UTC()),
			ID:        *auth.SessionID,
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return notFound("Session not found.")
		}
		userID := auth.UserID
		return s.appendEvent(r.Context(), tx, "auth.session.revoked", "auth_session", *auth.SessionID, nil, &userID, map[string]any{
			"session_id": auth.SessionID.String(),
			"user_id":    auth.UserID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	user, err := s.loadUser(r.Context(), auth.UserID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	rows, err := s.queries.ListWorkspaceMembershipSummariesByUser(r.Context(), auth.UserID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	var workspaces []api.WorkspaceMembershipSummary
	for _, row := range rows {
		item := api.WorkspaceMembershipSummary{
			WorkspaceID: row.WorkspaceID.String(),
			Role:        row.Role,
			Status:      row.Status,
			Name:        row.Name,
		}
		workspaces = append(workspaces, item)
	}
	writeJSON(w, http.StatusOK, api.MeResponse{
		User:       userToAPI(user),
		Workspaces: workspaces,
	})
}

func (s *Server) handlePatchMeProfile(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.UpdateProfileRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}

	if request.Handle != nil && strings.TrimSpace(*request.Handle) == "" {
		s.writeAppError(w, r, validationFailed("handle", "invalid_format", "Handle cannot be empty."))
		return
	}
	displayName := trimOptionalString(request.DisplayName)
	handle := trimOptionalString(request.Handle)
	avatarURL := trimOptionalString(request.AvatarURL)
	if request.AvatarURL != nil && avatarURL == nil {
		request.AvatarURL = nil
	}
	if avatarURL != nil {
		if err := expectURL(*avatarURL, "avatar_url"); err != nil {
			s.writeAppError(w, r, err)
			return
		}
	}
	bio := request.Bio

	err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).UpdateUserProfile(r.Context(), dbsqlc.UpdateUserProfileParams{
			Handle:      handle,
			DisplayName: displayName,
			AvatarUrl:   avatarURL,
			Bio:         bio,
			UpdatedAt:   dbsqlc.Timestamptz(time.Now().UTC()),
			UserID:      auth.UserID,
		}); err != nil {
			return err
		}
		userID := auth.UserID
		return s.appendEvent(r.Context(), tx, "user.updated", "user", auth.UserID, nil, &userID, map[string]any{
			"user_id": auth.UserID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	user, err := s.loadUser(r.Context(), auth.UserID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, userToAPI(user))
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	rows, err := s.queries.ListAPIKeysByUser(r.Context(), auth.UserID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	items := make([]api.APIKey, 0)
	for _, row := range rows {
		item := api.APIKey{
			ID:               row.ID.String(),
			Label:            row.Label,
			ScopeType:        row.ScopeType,
			ScopeWorkspaceID: uuidPtrToStringPtr(row.ScopeWorkspaceID),
			ExpiresAt:        timePtrToStringPtr(dbsqlc.TimePtr(row.ExpiresAt)),
			LastUsedAt:       timePtrToStringPtr(dbsqlc.TimePtr(row.LastUsedAt)),
			RevokedAt:        timePtrToStringPtr(dbsqlc.TimePtr(row.RevokedAt)),
			CreatedAt:        dbsqlc.TimeValue(row.CreatedAt).Format(time.RFC3339),
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.CollectionResponse[api.APIKey]{Items: items})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.CreateAPIKeyRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	request.Label = strings.TrimSpace(request.Label)
	if request.Label == "" {
		s.writeAppError(w, r, validationFailed("label", "required", "Label is required."))
		return
	}
	if request.ScopeType != "user" && request.ScopeType != "workspace" {
		s.writeAppError(w, r, validationFailed("scope_type", "invalid_value", "Must be either user or workspace."))
		return
	}
	scopeWorkspaceID, err := parseOptionalUUID(request.ScopeWorkspaceID)
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if request.ScopeType == "workspace" && scopeWorkspaceID == nil {
		s.writeAppError(w, r, validationFailed("scope_workspace_id", "required", "Workspace scope requires scope_workspace_id."))
		return
	}
	if request.ScopeType == "user" && scopeWorkspaceID != nil {
		s.writeAppError(w, r, validationFailed("scope_workspace_id", "invalid_value", "User-scoped keys must omit scope_workspace_id."))
		return
	}
	expiresAt, parseErr := parseTimeRFC3339(request.ExpiresAt, "expires_at")
	if parseErr != nil {
		s.writeAppError(w, r, parseErr)
		return
	}
	if scopeWorkspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *scopeWorkspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
	}

	secret, err := teracrypto.RandomToken(32)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	keyID := uuid.New()
	now := time.Now().UTC()
	if err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).CreateAPIKey(r.Context(), dbsqlc.CreateAPIKeyParams{
			ID:               keyID,
			UserID:           auth.UserID,
			Label:            request.Label,
			SecretHash:       teracrypto.SHA256Hex(secret),
			ScopeType:        request.ScopeType,
			ScopeWorkspaceID: scopeWorkspaceID,
			ExpiresAt:        dbsqlc.NullableTimestamptz(expiresAt),
			CreatedAt:        dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		userID := auth.UserID
		return s.appendEvent(r.Context(), tx, "api_key.created", "api_key", keyID, scopeWorkspaceID, &userID, map[string]any{
			"api_key_id":         keyID.String(),
			"user_id":            auth.UserID.String(),
			"scope_type":         request.ScopeType,
			"scope_workspace_id": uuidPtrToStringPtr(scopeWorkspaceID),
			"expires_at":         timePtrToStringPtr(expiresAt),
		})
	}); err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	writeJSON(w, http.StatusCreated, api.CreateAPIKeyResponse{
		APIKey: api.APIKey{
			ID:               keyID.String(),
			Label:            request.Label,
			ScopeType:        request.ScopeType,
			ScopeWorkspaceID: uuidPtrToStringPtr(scopeWorkspaceID),
			ExpiresAt:        timePtrToStringPtr(expiresAt),
			CreatedAt:        now.Format(time.RFC3339),
		},
		Secret: secret,
	})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if appErr := s.ensureGlobalUserSurfaceAccess(auth); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	keyID, err := parseUUIDPath(r, "key_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		rowsAffected, err := s.queries.WithTx(tx).RevokeAPIKeyByOwner(r.Context(), dbsqlc.RevokeAPIKeyByOwnerParams{
			RevokedAt: dbsqlc.Timestamptz(time.Now().UTC()),
			ID:        keyID,
			UserID:    auth.UserID,
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return notFound("API key not found.")
		}
		userID := auth.UserID
		return s.appendEvent(r.Context(), tx, "api_key.revoked", "api_key", keyID, nil, &userID, map[string]any{
			"api_key_id": keyID.String(),
			"user_id":    auth.UserID.String(),
		})
	})
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, _ domain.AuthContext) {
	s.writeAppError(w, r, notImplemented())
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if auth.APIKeyWorkspaceID != nil {
		if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, *auth.APIKeyWorkspaceID); appErr != nil {
			s.writeAppError(w, r, appErr)
			return
		}
		workspace, err := s.loadWorkspace(r.Context(), *auth.APIKeyWorkspaceID)
		if err != nil {
			s.writeAppError(w, r, internalError(err))
			return
		}
		writeJSON(w, http.StatusOK, api.CollectionResponse[api.Workspace]{Items: []api.Workspace{workspaceToAPI(workspace)}})
		return
	}
	rows, err := s.queries.ListActiveWorkspacesByUser(r.Context(), auth.UserID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	items := make([]api.Workspace, 0)
	for _, item := range rows {
		row := workspaceRow{
			ID:              item.ID,
			Slug:            item.Slug,
			Name:            item.Name,
			CreatedByUserID: item.CreatedByUserID,
			CreatedAt:       dbsqlc.TimeValue(item.CreatedAt),
			UpdatedAt:       dbsqlc.TimeValue(item.UpdatedAt),
		}
		items = append(items, workspaceToAPI(row))
	}
	writeJSON(w, http.StatusOK, api.CollectionResponse[api.Workspace]{Items: items})
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	if auth.APIKeyWorkspaceID != nil {
		s.writeAppError(w, r, forbidden("Workspace-scoped API keys cannot create workspaces."))
		return
	}
	var request api.CreateWorkspaceRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Slug = strings.TrimSpace(strings.ToLower(request.Slug))
	if request.Name == "" {
		s.writeAppError(w, r, validationFailed("name", "required", "Name is required."))
		return
	}
	if request.Slug == "" {
		s.writeAppError(w, r, validationFailed("slug", "required", "Slug is required."))
		return
	}

	now := time.Now().UTC()
	workspaceID := uuid.New()
	err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		if err := txQueries.CreateWorkspace(r.Context(), dbsqlc.CreateWorkspaceParams{
			ID:              workspaceID,
			Slug:            request.Slug,
			Name:            request.Name,
			CreatedByUserID: auth.UserID,
			CreatedAt:       dbsqlc.Timestamptz(now),
			UpdatedAt:       dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		if err := txQueries.CreateWorkspaceMembership(r.Context(), dbsqlc.CreateWorkspaceMembershipParams{
			ID:          uuid.New(),
			WorkspaceID: workspaceID,
			UserID:      auth.UserID,
			Role:        "owner",
			Status:      "active",
			JoinedAt:    dbsqlc.Timestamptz(now),
			CreatedAt:   dbsqlc.Timestamptz(now),
			UpdatedAt:   dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		conversationID := uuid.New()
		general := "general"
		if err := txQueries.CreateConversation(r.Context(), dbsqlc.CreateConversationParams{
			ID:              conversationID,
			WorkspaceID:     &workspaceID,
			AccessPolicy:    "workspace",
			Title:           &general,
			CreatedByUserID: auth.UserID,
			CreatedAt:       dbsqlc.Timestamptz(now),
			UpdatedAt:       dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}

		actor := auth.UserID
		if err := s.appendEvent(r.Context(), tx, "workspace.created", "workspace", workspaceID, &workspaceID, &actor, map[string]any{
			"workspace_id": workspaceID.String(),
			"name":         request.Name,
			"slug":         request.Slug,
		}); err != nil {
			return err
		}
		if err := s.appendEvent(r.Context(), tx, "workspace.membership.added", "workspace_membership", uuid.New(), &workspaceID, &actor, map[string]any{
			"workspace_id": workspaceID.String(),
			"user_id":      auth.UserID.String(),
			"role":         "owner",
		}); err != nil {
			return err
		}
		return s.appendEvent(r.Context(), tx, "conversation.created", "conversation", conversationID, &workspaceID, &actor, map[string]any{
			"conversation_id": conversationID.String(),
			"workspace_id":    workspaceID.String(),
			"access_policy":   "workspace",
			"title":           general,
		})
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	workspace, err := s.loadWorkspace(r.Context(), workspaceID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusCreated, workspaceToAPI(workspace))
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, err := parseUUIDPath(r, "workspace_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, workspaceID); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	workspace, err := s.loadWorkspace(r.Context(), workspaceID)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, notFound("Workspace not found."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, workspaceToAPI(workspace))
}

func (s *Server) handlePatchWorkspace(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, err := parseUUIDPath(r, "workspace_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if appErr := s.ensureWorkspaceAdmin(r.Context(), auth, workspaceID); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.UpdateWorkspaceRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	name := trimOptionalString(request.Name)
	slug := trimOptionalString(request.Slug)
	if err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).UpdateWorkspace(r.Context(), dbsqlc.UpdateWorkspaceParams{
			Name:      name,
			Slug:      slug,
			UpdatedAt: dbsqlc.Timestamptz(time.Now().UTC()),
			ID:        workspaceID,
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "workspace.updated", "workspace", workspaceID, &workspaceID, &actor, map[string]any{
			"workspace_id": workspaceID.String(),
			"name":         name,
			"slug":         slug,
		})
	}); err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	workspace, err := s.loadWorkspace(r.Context(), workspaceID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, workspaceToAPI(workspace))
}

func (s *Server) handleListWorkspaceMembers(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, err := parseUUIDPath(r, "workspace_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if _, appErr := s.ensureWorkspaceActiveMember(r.Context(), auth, workspaceID); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	rows, err := s.queries.ListWorkspaceMembers(r.Context(), workspaceID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	items := make([]api.WorkspaceMember, 0)
	for _, row := range rows {
		item := api.WorkspaceMember{
			WorkspaceID: row.WorkspaceID.String(),
			UserID:      row.UserID.String(),
			Role:        row.Role,
			Status:      row.Status,
			User: userToAPI(userRow{
				ID:            row.ID,
				PrincipalType: row.PrincipalType,
				Status:        row.Status_2,
				Email:         row.Email,
				Handle:        row.Handle,
				DisplayName:   row.DisplayName,
				AvatarURL:     row.AvatarUrl,
				Bio:           row.Bio,
			}),
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.CollectionResponse[api.WorkspaceMember]{Items: items})
}

func (s *Server) handleCreateWorkspaceInvite(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, err := parseUUIDPath(r, "workspace_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if appErr := s.ensureWorkspaceAdmin(r.Context(), auth, workspaceID); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	var request api.CreateWorkspaceInviteRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if request.Email != nil {
		normalized := normalizeEmail(*request.Email)
		request.Email = &normalized
	}
	token, err := teracrypto.RandomToken(24)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	inviteID := uuid.New()
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)
	if err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		if err := s.queries.WithTx(tx).CreateWorkspaceInvite(r.Context(), dbsqlc.CreateWorkspaceInviteParams{
			ID:              inviteID,
			WorkspaceID:     workspaceID,
			Email:           request.Email,
			InvitedByUserID: auth.UserID,
			TokenHash:       teracrypto.SHA256Hex(token),
			ExpiresAt:       dbsqlc.Timestamptz(expiresAt),
			CreatedAt:       dbsqlc.Timestamptz(now),
		}); err != nil {
			return err
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "workspace.invite.created", "workspace_invite", inviteID, &workspaceID, &actor, map[string]any{
			"workspace_invite_id": inviteID.String(),
			"workspace_id":        workspaceID.String(),
			"email":               request.Email,
			"expires_at":          expiresAt.Format(time.RFC3339),
		})
	}); err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	inviteURL := strings.TrimRight(s.cfg.BaseURL, "/") + "/workspace-invites/" + url.PathEscape(token)
	writeJSON(w, http.StatusCreated, api.CreateWorkspaceInviteResponse{
		InviteToken: token,
		InviteURL:   inviteURL,
	})
}

func (s *Server) handleAcceptWorkspaceInvite(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	token := strings.TrimSpace(r.PathValue("token"))
	if token == "" {
		s.writeAppError(w, r, notFound("Workspace invite not found."))
		return
	}
	var member api.WorkspaceMember
	err := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		now := time.Now().UTC()
		invite, err := txQueries.GetWorkspaceInviteByTokenHashForUpdate(r.Context(), dbsqlc.GetWorkspaceInviteByTokenHashForUpdateParams{
			TokenHash: teracrypto.SHA256Hex(token),
			NowAt:     dbsqlc.Timestamptz(now),
		})
		if err != nil {
			if err == pgx.ErrNoRows {
				return notFound("Workspace invite not found.")
			}
			return err
		}
		if invite.Email != nil {
			user, err := s.loadUser(r.Context(), auth.UserID)
			if err != nil {
				return err
			}
			if user.Email == nil || normalizeEmail(*user.Email) != normalizeEmail(*invite.Email) {
				return forbidden("This invite is not valid for the authenticated user.")
			}
		}

		workspaceID := invite.WorkspaceID
		membershipID := uuid.UUID{}
		role := ""
		status := ""
		membershipChanged := false
		membership, err := txQueries.GetWorkspaceMembershipForUpdate(r.Context(), dbsqlc.GetWorkspaceMembershipForUpdateParams{
			WorkspaceID: workspaceID,
			UserID:      auth.UserID,
		})
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == pgx.ErrNoRows {
			membershipID = uuid.New()
			role = "member"
			status = "active"
			if err := txQueries.CreateWorkspaceMembership(r.Context(), dbsqlc.CreateWorkspaceMembershipParams{
				ID:          membershipID,
				WorkspaceID: workspaceID,
				UserID:      auth.UserID,
				Role:        role,
				Status:      "active",
				JoinedAt:    dbsqlc.Timestamptz(now),
				CreatedAt:   dbsqlc.Timestamptz(now),
				UpdatedAt:   dbsqlc.Timestamptz(now),
			}); err != nil {
				return err
			}
			membershipChanged = true
		} else {
			membershipID = membership.ID
			role = membership.Role
			status = membership.Status
		}
		if err == nil && status != "active" {
			if err := txQueries.ActivateWorkspaceMembership(r.Context(), dbsqlc.ActivateWorkspaceMembershipParams{
				UpdatedAt: dbsqlc.Timestamptz(now),
				ID:        membershipID,
				UserID:    auth.UserID,
			}); err != nil {
				return err
			}
			status = "active"
			membershipChanged = true
		}
		if dbsqlc.TimePtr(invite.AcceptedAt) == nil {
			if err := txQueries.AcceptWorkspaceInvite(r.Context(), dbsqlc.AcceptWorkspaceInviteParams{
				AcceptedAt:       dbsqlc.Timestamptz(now),
				AcceptedByUserID: &auth.UserID,
				ID:               invite.ID,
			}); err != nil {
				return err
			}
		}
		workspace, err := s.loadWorkspace(r.Context(), workspaceID)
		if err != nil {
			return err
		}
		user, err := s.loadUser(r.Context(), auth.UserID)
		if err != nil {
			return err
		}
		member = api.WorkspaceMember{
			WorkspaceID: workspaceID.String(),
			UserID:      auth.UserID.String(),
			Role:        role,
			Status:      "active",
			User:        userToAPI(user),
		}
		if !membershipChanged {
			return nil
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "workspace.membership.added", "workspace_membership", membershipID, &workspace.ID, &actor, map[string]any{
			"workspace_id": workspaceID.String(),
			"user_id":      auth.UserID.String(),
			"role":         role,
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
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) handlePatchWorkspaceMember(w http.ResponseWriter, r *http.Request, auth domain.AuthContext) {
	workspaceID, err := parseUUIDPath(r, "workspace_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if appErr := s.ensureWorkspaceAdmin(r.Context(), auth, workspaceID); appErr != nil {
		s.writeAppError(w, r, appErr)
		return
	}
	userID, err := parseUUIDPath(r, "user_id")
	if err != nil {
		s.writeAppError(w, r, err)
		return
	}
	var request api.UpdateWorkspaceMemberRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if request.Role == nil && request.Status == nil {
		s.writeAppError(w, r, validationFailed("body", "required", "At least one field must be provided."))
		return
	}
	if request.Role != nil {
		role := strings.TrimSpace(*request.Role)
		switch role {
		case "owner", "admin", "member":
			request.Role = &role
		default:
			s.writeAppError(w, r, validationFailed("role", "invalid_value", "Must be one of owner, admin, member."))
			return
		}
	}
	if request.Status != nil {
		status := strings.TrimSpace(*request.Status)
		switch status {
		case "invited", "active", "suspended", "removed":
			request.Status = &status
		default:
			s.writeAppError(w, r, validationFailed("status", "invalid_value", "Must be one of invited, active, suspended, removed."))
			return
		}
	}

	errExec := withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		membership, err := txQueries.GetWorkspaceMembershipForUpdate(r.Context(), dbsqlc.GetWorkspaceMembershipForUpdateParams{
			WorkspaceID: workspaceID,
			UserID:      userID,
		})
		if err != nil {
			if err == pgx.ErrNoRows {
				return notFound("Workspace member not found.")
			}
			return err
		}
		nextRole := membership.Role
		nextStatus := membership.Status
		if request.Role != nil {
			nextRole = strings.TrimSpace(*request.Role)
		}
		if request.Status != nil {
			nextStatus = strings.TrimSpace(*request.Status)
		}
		if membership.Role == "owner" && (nextRole != "owner" || nextStatus != "active") {
			owners, err := txQueries.CountActiveWorkspaceOwners(r.Context(), workspaceID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return conflict("Workspace must keep at least one active owner.")
			}
		}
		if err := txQueries.UpdateWorkspaceMembership(r.Context(), dbsqlc.UpdateWorkspaceMembershipParams{
			Role:      nextRole,
			Status:    nextStatus,
			UpdatedAt: dbsqlc.Timestamptz(time.Now().UTC()),
			ID:        membership.ID,
			UserID:    userID,
		}); err != nil {
			return err
		}
		if nextStatus != "active" {
			if err := s.removeWorkspacePrivateConversationParticipantsTx(r.Context(), tx, workspaceID, userID, auth.UserID); err != nil {
				return err
			}
		}
		actor := auth.UserID
		return s.appendEvent(r.Context(), tx, "workspace.membership.updated", "workspace_membership", membership.ID, &workspaceID, &actor, map[string]any{
			"workspace_id": workspaceID.String(),
			"user_id":      userID.String(),
			"role":         nextRole,
			"status":       nextStatus,
		})
	})
	if errExec != nil {
		if appErr, ok := errExec.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(errExec))
		return
	}

	user, err := s.loadUser(r.Context(), userID)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	membership, err := s.queries.GetWorkspaceMembership(r.Context(), dbsqlc.GetWorkspaceMembershipParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	writeJSON(w, http.StatusOK, api.WorkspaceMember{
		WorkspaceID: workspaceID.String(),
		UserID:      userID.String(),
		Role:        membership.Role,
		Status:      membership.Status,
		User:        userToAPI(user),
	})
}

func (s *Server) createEmailLoginChallenge(ctx context.Context, email string) (string, error) {
	code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	now := time.Now().UTC()
	err := s.queries.CreateEmailLoginChallenge(ctx, dbsqlc.CreateEmailLoginChallengeParams{
		ID:        uuid.New(),
		Email:     email,
		CodeHash:  teracrypto.SHA256Hex(code),
		ExpiresAt: dbsqlc.Timestamptz(now.Add(10 * time.Minute)),
		CreatedAt: dbsqlc.Timestamptz(now),
	})
	return code, err
}

func (s *Server) createSessionTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (uuid.UUID, string, api.SessionEnvelope, error) {
	token, err := teracrypto.RandomToken(32)
	if err != nil {
		return uuid.UUID{}, "", api.SessionEnvelope{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(30 * 24 * time.Hour)
	sessionID := uuid.New()
	if err := s.queries.WithTx(tx).CreateAuthSession(ctx, dbsqlc.CreateAuthSessionParams{
		ID:         sessionID,
		UserID:     userID,
		TokenHash:  teracrypto.SHA256Hex(token),
		ExpiresAt:  dbsqlc.Timestamptz(expiresAt),
		LastSeenAt: dbsqlc.Timestamptz(now),
		CreatedAt:  dbsqlc.Timestamptz(now),
	}); err != nil {
		return uuid.UUID{}, "", api.SessionEnvelope{}, err
	}
	return sessionID, token, api.SessionEnvelope{
		Token:     token,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}

func (s *Server) resolveOrCreateUserByEmailTx(ctx context.Context, tx pgx.Tx, email string) (userRow, bool, error) {
	row, err := s.queries.WithTx(tx).GetUserByEmail(ctx, stringPtr(email))
	if err == nil {
		return userRow{
			ID:            row.ID,
			PrincipalType: row.PrincipalType,
			Status:        row.Status,
			Email:         row.Email,
			Handle:        row.Handle,
			DisplayName:   row.DisplayName,
			AvatarURL:     row.AvatarUrl,
			Bio:           row.Bio,
		}, false, nil
	}
	if err != pgx.ErrNoRows {
		return userRow{}, false, err
	}
	var user userRow
	user, err = s.insertUserWithProfile(ctx, tx, email)
	return user, true, err
}

func (s *Server) sendEmailLoginCode(ctx context.Context, email string, code string) error {
	body, _ := json.Marshal(map[string]any{
		"from":    s.cfg.AuthEmailFrom,
		"to":      []string{email},
		"subject": "Your Teraslack login code",
		"text":    fmt.Sprintf("Your verification code is %s", code),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.ResendAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend returned status %d", resp.StatusCode)
	}
	return nil
}

func (s *Server) removeWorkspacePrivateConversationParticipantsTx(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, userID uuid.UUID, actorUserID uuid.UUID) error {
	txQueries := s.queries.WithTx(tx)
	rows, err := txQueries.ListWorkspacePrivateConversationParticipantCountsForUser(ctx, dbsqlc.ListWorkspacePrivateConversationParticipantCountsForUserParams{
		UserID:      userID,
		WorkspaceID: &workspaceID,
	})
	if err != nil {
		return err
	}

	conversationCounts := make(map[uuid.UUID]int)
	for _, row := range rows {
		if row.ParticipantCount <= 1 {
			return conflict("Cannot deactivate this workspace member because they are the last participant in a workspace-private conversation.")
		}
		conversationCounts[row.ID] = int(row.ParticipantCount)
	}

	for conversationID := range conversationCounts {
		rowsAffected, err := txQueries.DeleteConversationParticipant(ctx, dbsqlc.DeleteConversationParticipantParams{
			ConversationID: conversationID,
			UserID:         userID,
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			continue
		}
		if err := s.appendEvent(ctx, tx, "conversation.participant.removed", "conversation", conversationID, &workspaceID, &actorUserID, map[string]any{
			"conversation_id": conversationID.String(),
			"user_id":         userID.String(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(strings.ToLower(s.cfg.BaseURL), "https://"),
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
}

func withTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func notImplemented() *appError {
	return &appError{Status: http.StatusNotImplemented, Code: "not_implemented", Message: "This route is not implemented yet."}
}
