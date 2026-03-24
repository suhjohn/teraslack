package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type ConversationReadHandler struct {
	svc *service.ConversationReadService
}

func NewConversationReadHandler(svc *service.ConversationReadService) *ConversationReadHandler {
	return &ConversationReadHandler{svc: svc}
}

func (h *ConversationReadHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req ConversationReadUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.MarkRead(r.Context(), domain.MarkConversationReadParams{
		ConversationID: channelID,
		LastReadTS:     req.LastReadTS,
	}); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteNoContent(w)
}
