package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// UserHandler handles HTTP requests for user operations.
type UserHandler struct {
	svc     *service.UserService
	roleSvc *service.RoleService
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(svc *service.UserService, roleSvc *service.RoleService) *UserHandler {
	return &UserHandler{svc: svc, roleSvc: roleSvc}
}

func requestWorkspaceID(r *http.Request) string {
	if workspaceID := strings.TrimSpace(r.PathValue("workspace_id")); workspaceID != "" {
		return workspaceID
	}
	if workspaceID := strings.TrimSpace(r.PathValue("id")); workspaceID != "" && strings.Contains(r.URL.Path, "/workspaces/") && !strings.Contains(r.URL.Path, "/users/") {
		return workspaceID
	}
	return strings.TrimSpace(ctxutil.GetWorkspaceID(r.Context()))
}

func requestUserID(r *http.Request) string {
	if userID := strings.TrimSpace(r.PathValue("user_id")); userID != "" {
		return userID
	}
	return strings.TrimSpace(r.PathValue("id"))
}

// Create handles POST /workspaces/{workspace_id}/users.
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateUserParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if workspaceID := requestWorkspaceID(r); workspaceID != "" {
		params.WorkspaceID = workspaceID
	}

	user, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	location := ""
	if workspaceID := strings.TrimSpace(user.WorkspaceID); workspaceID != "" {
		location = "/workspaces/" + workspaceID + "/users/" + user.ID
	}
	httputil.WriteCreated(w, location, user)
}

// Info handles GET /workspaces/{workspace_id}/users/{user_id}.
func (h *UserHandler) Info(w http.ResponseWriter, r *http.Request) {
	userID := requestUserID(r)
	if userID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	user, err := h.svc.Get(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, user)
}

// Update handles PATCH /workspaces/{workspace_id}/users/{user_id}.
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := requestUserID(r)
	if userID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateUserParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	user, err := h.svc.Update(r.Context(), userID, params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, user)
}

// List handles GET /workspaces/{workspace_id}/users.
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if email := q.Get("email"); email != "" {
		user, err := h.svc.GetByEmail(r.Context(), email)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		httputil.WriteCollection(w, http.StatusOK, []domain.User{*user}, "")
		return
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	params := domain.ListUsersParams{
		WorkspaceID: q.Get("workspace_id"),
		Cursor:      q.Get("cursor"),
		Limit:       limit,
	}
	if workspaceID := requestWorkspaceID(r); workspaceID != "" {
		params.WorkspaceID = workspaceID
	}

	page, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	nextCursor := ""
	if page.HasMore {
		nextCursor = page.NextCursor
	}
	httputil.WriteCollection(w, http.StatusOK, page.Items, nextCursor)
}

func (h *UserHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	if h.roleSvc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	userID := requestUserID(r)
	if userID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	roles, err := h.roleSvc.ListUserRoles(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, UserRolesResponse{
		UserID:         userID,
		DelegatedRoles: roles,
	})
}

func (h *UserHandler) SetRoles(w http.ResponseWriter, r *http.Request) {
	if h.roleSvc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	userID := requestUserID(r)
	if userID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	var req UserRolesUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	roles, err := h.roleSvc.SetUserRoles(r.Context(), userID, req.DelegatedRoles)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, UserRolesResponse{
		UserID:         userID,
		DelegatedRoles: roles,
	})
}
