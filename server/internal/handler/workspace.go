package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// WorkspaceHandler handles workspace and workspace HTTP routes.
type WorkspaceHandler struct {
	svc *service.WorkspaceService
}

// NewWorkspaceHandler creates a new WorkspaceHandler.
func NewWorkspaceHandler(svc *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{svc: svc}
}

// Get handles GET /workspaces/{id}.
func (h *WorkspaceHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.svc.WorkspaceInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, workspace)
}

// List handles GET /workspaces.
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.svc.AdminList(r.Context())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, workspaces, "")
}

// Create handles POST /workspaces.
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateWorkspaceParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	workspace, err := h.svc.AdminCreate(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCreated(w, "/workspaces/"+workspace.ID, workspace)
}

// Update handles PATCH /workspaces/{id}.
func (h *WorkspaceHandler) Update(w http.ResponseWriter, r *http.Request) {
	var params domain.UpdateWorkspaceParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	workspace, err := h.svc.Update(r.Context(), r.PathValue("id"), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, workspace)
}

// ListAdmins handles GET /workspaces/{id}/admins.
func (h *WorkspaceHandler) ListAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := h.svc.AdminListAdmins(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, admins, "")
}

// ListOwners handles GET /workspaces/{id}/owners.
func (h *WorkspaceHandler) ListOwners(w http.ResponseWriter, r *http.Request) {
	owners, err := h.svc.AdminListOwners(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, owners, "")
}

// AccessLogs handles GET /workspaces/{id}/access-logs.
func (h *WorkspaceHandler) AccessLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := h.svc.WorkspaceAccessLogs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, logs, "")
}

// BillableInfo handles GET /workspaces/{id}/billable-info.
func (h *WorkspaceHandler) BillableInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.WorkspaceBillableInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, info)
}

// Billing handles GET /workspaces/{id}/billing.
func (h *WorkspaceHandler) Billing(w http.ResponseWriter, r *http.Request) {
	billing, err := h.svc.WorkspaceBillingInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, billing)
}

// ListExternalWorkspaces handles GET /workspaces/{id}/external-workspaces.
func (h *WorkspaceHandler) ListExternalWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.svc.TeamExternalWorkspaces(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, workspaces, "")
}

// CreateExternalWorkspace handles POST /workspaces/{id}/external-workspaces.
func (h *WorkspaceHandler) CreateExternalWorkspace(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateExternalWorkspaceParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	item, err := h.svc.CreateExternalWorkspace(r.Context(), r.PathValue("id"), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCreated(w, "/workspaces/"+r.PathValue("id")+"/external-workspaces/"+item.ExternalWorkspaceID, item)
}

// DisconnectExternalWorkspace handles DELETE /workspaces/{id}/external-workspaces/{external_workspace_id}.
func (h *WorkspaceHandler) DisconnectExternalWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DisconnectExternalWorkspace(r.Context(), r.PathValue("id"), r.PathValue("external_workspace_id")); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteNoContent(w)
}

// IntegrationLogs handles GET /workspaces/{id}/integration-logs.
func (h *WorkspaceHandler) IntegrationLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := h.svc.WorkspaceIntegrationLogs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, logs, "")
}

func (h *WorkspaceHandler) AuthorizationAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	logs, err := h.svc.WorkspaceAuthorizationAuditLogs(r.Context(), r.PathValue("id"), limit)
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

// Preferences handles GET /workspaces/{id}/preferences.
func (h *WorkspaceHandler) Preferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := h.svc.WorkspacePreferences(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, prefs)
}
