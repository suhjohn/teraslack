package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// UsergroupHandler handles HTTP requests for usergroup operations.
type UsergroupHandler struct {
	svc *service.UsergroupService
}

// NewUsergroupHandler creates a new UsergroupHandler.
func NewUsergroupHandler(svc *service.UsergroupService) *UsergroupHandler {
	return &UsergroupHandler{svc: svc}
}

// Create handles POST /usergroups.
func (h *UsergroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateUsergroupParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if teamID := ctxutil.GetTeamID(r.Context()); teamID != "" {
		params.TeamID = teamID
	}
	if actorID := ctxutil.GetActingUserID(r.Context()); actorID != "" {
		params.CreatedBy = actorID
	}

	ug, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/usergroups/"+ug.ID, ug)
}

// Update handles PATCH /usergroups/{id}.
func (h *UsergroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req UsergroupUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if actorID := ctxutil.GetActingUserID(r.Context()); actorID != "" {
		req.UpdatedBy = actorID
	}

	if req.Name != nil || req.Handle != nil || req.Description != nil {
		if _, err := h.svc.Update(r.Context(), ugID, domain.UpdateUsergroupParams{
			Name:        req.Name,
			Handle:      req.Handle,
			Description: req.Description,
			UpdatedBy:   req.UpdatedBy,
		}); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	if req.Enabled != nil {
		var err error
		if *req.Enabled {
			err = h.svc.Enable(r.Context(), ugID)
		} else {
			err = h.svc.Disable(r.Context(), ugID)
		}
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	ug, err := h.svc.Get(r.Context(), ugID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, ug)
}

// List handles GET /usergroups.
func (h *UsergroupHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	includeDisabled, _ := strconv.ParseBool(q.Get("include_disabled"))

	params := domain.ListUsergroupsParams{
		TeamID:          q.Get("team_id"),
		IncludeDisabled: includeDisabled,
	}
	if teamID := ctxutil.GetTeamID(r.Context()); teamID != "" {
		params.TeamID = teamID
	}
	groups, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCollection(w, http.StatusOK, groups, "")
}

// ListUsers handles GET /usergroups/{id}/members.
func (h *UsergroupHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	users, err := h.svc.ListUsers(r.Context(), ugID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCollection(w, http.StatusOK, users, "")
}

// Info handles GET /usergroups/{id}.
func (h *UsergroupHandler) Info(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	ug, err := h.svc.Get(r.Context(), ugID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, ug)
}

// SetUsers handles PUT /usergroups/{id}/members.
func (h *UsergroupHandler) SetUsers(w http.ResponseWriter, r *http.Request) {
	ugID := r.PathValue("id")
	if ugID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req UsergroupMembersUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.SetUsers(r.Context(), ugID, req.Users); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}
