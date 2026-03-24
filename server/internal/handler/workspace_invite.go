package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type WorkspaceInviteHandler struct {
	svc *service.WorkspaceInviteService
}

func NewWorkspaceInviteHandler(svc *service.WorkspaceInviteService) *WorkspaceInviteHandler {
	return &WorkspaceInviteHandler{svc: svc}
}

func (h *WorkspaceInviteHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		httputil.WriteInternalError(w, r)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	result, err := h.svc.Create(r.Context(), r.PathValue("id"), req.Email)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCreated(w, "/teams/"+r.PathValue("id")+"/invites/"+result.Invite.ID, result)
}
