package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// PinHandler handles HTTP requests for pin operations.
type PinHandler struct {
	svc *service.PinService
}

// NewPinHandler creates a new PinHandler.
func NewPinHandler(svc *service.PinService) *PinHandler {
	return &PinHandler{svc: svc}
}

// Add handles POST /conversations/{id}/pins.
func (h *PinHandler) Add(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	var req PinCreateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if actorID := ctxutil.GetActingUserID(r.Context()); actorID != "" {
		req.UserID = actorID
	}

	pin, err := h.svc.Add(r.Context(), domain.PinParams{
		ChannelID: channelID,
		MessageTS: req.MessageTS,
		UserID:    req.UserID,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/conversations/"+channelID+"/pins/"+pin.MessageTS, pin)
}

// Remove handles DELETE /conversations/{id}/pins/{message_ts}.
func (h *PinHandler) Remove(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	messageTS := r.PathValue("message_ts")

	if err := h.svc.Remove(r.Context(), domain.PinParams{
		ChannelID: channelID,
		MessageTS: messageTS,
	}); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// List handles GET /conversations/{id}/pins.
func (h *PinHandler) List(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")

	pins, err := h.svc.List(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCollection(w, http.StatusOK, pins, "")
}
