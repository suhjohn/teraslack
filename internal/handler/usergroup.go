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

// Create handles POST /usergroups
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

// Update handles POST /usergroups/{id}
func (h *UsergroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Name        *string `json:"name,omitempty"`
		Handle      *string `json:"handle,omitempty"`
		Description *string `json:"description,omitempty"`
		UpdatedBy   string  `json:"updated_by"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	ug, err := h.svc.Update(r.Context(), ugID, domain.UpdateUsergroupParams{
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

// List handles GET /usergroups?team_id=T123&include_disabled=true
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

// Enable handles POST /usergroups/{id}/enable
func (h *UsergroupHandler) Enable(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Enable(r.Context(), ugID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Disable handles POST /usergroups/{id}/disable
func (h *UsergroupHandler) Disable(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Disable(r.Context(), ugID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// ListUsers handles GET /usergroups/{id}/users
func (h *UsergroupHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	users, err := h.svc.ListUsers(r.Context(), ugID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"users": users})
}

// Info handles GET /usergroups/{id}
func (h *UsergroupHandler) Info(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	ug, err := h.svc.Get(r.Context(), ugID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"usergroup": ug})
}

// SetUsers handles POST /usergroups/{id}/users
func (h *UsergroupHandler) SetUsers(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Users []string `json:"users"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.SetUsers(r.Context(), ugID, req.Users); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"usergroup_id": ugID, "users": req.Users})
}
