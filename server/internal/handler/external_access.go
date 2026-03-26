package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type ExternalPrincipalAccessHandler struct {
	svc *service.ExternalPrincipalAccessService
}

func NewExternalPrincipalAccessHandler(svc *service.ExternalPrincipalAccessService) *ExternalPrincipalAccessHandler {
	return &ExternalPrincipalAccessHandler{svc: svc}
}

func (h *ExternalPrincipalAccessHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context(), domain.ListExternalPrincipalAccessParams{
		HostWorkspaceID: r.URL.Query().Get("host_workspace_id"),
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, items, "")
}

func (h *ExternalPrincipalAccessHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateExternalPrincipalAccessParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	item, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCreated(w, "/external-principal-access/"+item.ID, item)
}

func (h *ExternalPrincipalAccessHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	item, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, item)
}

func (h *ExternalPrincipalAccessHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	var params domain.UpdateExternalPrincipalAccessParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	item, err := h.svc.Update(r.Context(), id, params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, item)
}

func (h *ExternalPrincipalAccessHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if err := h.svc.Revoke(r.Context(), id); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteNoContent(w)
}
