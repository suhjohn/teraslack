package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type ExternalMemberHandler struct {
	svc *service.ExternalMemberService
}

func NewExternalMemberHandler(svc *service.ExternalMemberService) *ExternalMemberHandler {
	return &ExternalMemberHandler{svc: svc}
}

func (h *ExternalMemberHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListByConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCollection(w, http.StatusOK, items, "")
}

func (h *ExternalMemberHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateExternalMemberParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	item, err := h.svc.Create(r.Context(), r.PathValue("id"), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteCreated(w, "/conversations/"+item.ConversationID+"/external-members/"+item.ID, item)
}

func (h *ExternalMemberHandler) Update(w http.ResponseWriter, r *http.Request) {
	var params domain.UpdateExternalMemberParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	item, err := h.svc.Update(r.Context(), r.PathValue("id"), r.PathValue("external_member_id"), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, item)
}

func (h *ExternalMemberHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Revoke(r.Context(), r.PathValue("id"), r.PathValue("external_member_id")); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteNoContent(w)
}
