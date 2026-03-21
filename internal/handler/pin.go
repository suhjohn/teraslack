package handler

import (
	"net/http"

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

// Add handles POST /pins
func (h *PinHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
		UserID    string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	pin, err := h.svc.Add(r.Context(), domain.PinParams{
		ChannelID: req.Channel,
		MessageTS: req.Timestamp,
		UserID:    req.UserID,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"pin": pin})
}

// Remove handles DELETE /pins
func (h *PinHandler) Remove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Remove(r.Context(), domain.PinParams{
		ChannelID: req.Channel,
		MessageTS: req.Timestamp,
	}); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// List handles GET /pins?channel=C123
func (h *PinHandler) List(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel")

	pins, err := h.svc.List(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"items": pins})
}
