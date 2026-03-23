package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// WorkspaceHandler handles workspace and team HTTP routes.
type WorkspaceHandler struct {
	svc *service.WorkspaceService
}

// NewWorkspaceHandler creates a new WorkspaceHandler.
func NewWorkspaceHandler(svc *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{svc: svc}
}

// Get handles GET /teams/{id}.
func (h *WorkspaceHandler) Get(w http.ResponseWriter, r *http.Request) {
	team, err := h.svc.TeamInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, team)
}

// List handles GET /teams.
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	teams, err := h.svc.AdminList(r.Context())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, teams, "")
}

// Create handles POST /teams.
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateWorkspaceParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	team, err := h.svc.AdminCreate(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCreated(w, "/teams/"+team.ID, team)
}

// Update handles PATCH /teams/{id}.
func (h *WorkspaceHandler) Update(w http.ResponseWriter, r *http.Request) {
	var params domain.UpdateWorkspaceParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	team, err := h.svc.Update(r.Context(), r.PathValue("id"), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, team)
}

// ListAdmins handles GET /teams/{id}/admins.
func (h *WorkspaceHandler) ListAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := h.svc.AdminListAdmins(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, admins, "")
}

// ListOwners handles GET /teams/{id}/owners.
func (h *WorkspaceHandler) ListOwners(w http.ResponseWriter, r *http.Request) {
	owners, err := h.svc.AdminListOwners(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, owners, "")
}

// AccessLogs handles GET /teams/{id}/access-logs.
func (h *WorkspaceHandler) AccessLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := h.svc.TeamAccessLogs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, logs, "")
}

// BillableInfo handles GET /teams/{id}/billable-info.
func (h *WorkspaceHandler) BillableInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.TeamBillableInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, info)
}

// Billing handles GET /teams/{id}/billing.
func (h *WorkspaceHandler) Billing(w http.ResponseWriter, r *http.Request) {
	billing, err := h.svc.TeamBillingInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, billing)
}

// ListExternalTeams handles GET /teams/{id}/external-teams.
func (h *WorkspaceHandler) ListExternalTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := h.svc.TeamExternalTeams(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, teams, "")
}

// DisconnectExternalTeam handles DELETE /teams/{id}/external-teams/{external_team_id}.
func (h *WorkspaceHandler) DisconnectExternalTeam(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DisconnectExternalTeam(r.Context(), r.PathValue("id"), r.PathValue("external_team_id")); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteNoContent(w)
}

// IntegrationLogs handles GET /teams/{id}/integration-logs.
func (h *WorkspaceHandler) IntegrationLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := h.svc.TeamIntegrationLogs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, logs, "")
}

func (h *WorkspaceHandler) AuthorizationAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := h.svc.TeamAuthorizationAuditLogs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, logs, "")
}

func (h *WorkspaceHandler) TransferPrimaryAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	user, err := h.svc.TransferPrimaryAdmin(r.Context(), r.PathValue("id"), req.UserID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, user)
}

// Preferences handles GET /teams/{id}/preferences.
func (h *WorkspaceHandler) Preferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := h.svc.TeamPreferences(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, prefs)
}

// ProfileFields handles GET /teams/{id}/profile-fields.
func (h *WorkspaceHandler) ProfileFields(w http.ResponseWriter, r *http.Request) {
	fields, err := h.svc.TeamProfile(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, fields, "")
}
