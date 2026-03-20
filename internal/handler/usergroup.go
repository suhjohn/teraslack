package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// UsergroupHandler handles HTTP requests for usergroup operations.
type UsergroupHandler struct {
	svc *service.UsergroupService
}

// NewUsergroupHandler creates a new UsergroupHandler.
func NewUsergroupHandler(svc *service.UsergroupService) *UsergroupHandler {
	return &UsergroupHandler{svc: svc}
}

// Create handles POST /api/usergroups.create
func (h *UsergroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateUsergroupParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	ug, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"usergroup": ug})
}

// Update handles POST /api/usergroups.update
func (h *UsergroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Usergroup   string  `json:"usergroup"`
		Name        *string `json:"name,omitempty"`
		Handle      *string `json:"handle,omitempty"`
		Description *string `json:"description,omitempty"`
		UpdatedBy   string  `json:"updated_by"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	ug, err := h.svc.Update(r.Context(), req.Usergroup, domain.UpdateUsergroupParams{
		Name:        req.Name,
		Handle:      req.Handle,
		Description: req.Description,
		UpdatedBy:   req.UpdatedBy,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"usergroup": ug})
}

// List handles GET /api/usergroups.list?team_id=T123&include_disabled=true
func (h *UsergroupHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	includeDisabled, _ := strconv.ParseBool(q.Get("include_disabled"))

	groups, err := h.svc.List(r.Context(), domain.ListUsergroupsParams{
		TeamID:          q.Get("team_id"),
		IncludeDisabled: includeDisabled,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"usergroups": groups})
}

// Enable handles POST /api/usergroups.enable
func (h *UsergroupHandler) Enable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Usergroup string `json:"usergroup"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Enable(r.Context(), req.Usergroup); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Disable handles POST /api/usergroups.disable
func (h *UsergroupHandler) Disable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Usergroup string `json:"usergroup"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Disable(r.Context(), req.Usergroup); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// ListUsers handles GET /api/usergroups.users.list?usergroup=S123
func (h *UsergroupHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	usergroupID := r.URL.Query().Get("usergroup")

	users, err := h.svc.ListUsers(r.Context(), usergroupID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"users": users})
}

// SetUsers handles POST /api/usergroups.users.update
func (h *UsergroupHandler) SetUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Usergroup string   `json:"usergroup"`
		Users     []string `json:"users"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.SetUsers(r.Context(), req.Usergroup, req.Users); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"usergroup_id": req.Usergroup, "users": req.Users})
}
