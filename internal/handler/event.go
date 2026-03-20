package handler

import (
	"net/http"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// EventHandler handles HTTP requests for event subscription operations.
type EventHandler struct {
	svc *service.EventService
}

// NewEventHandler creates a new EventHandler.
func NewEventHandler(svc *service.EventService) *EventHandler {
	return &EventHandler{svc: svc}
}

// CreateSubscription handles POST /api/events.subscriptions.create
func (h *EventHandler) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateEventSubscriptionParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	sub, err := h.svc.CreateSubscription(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"subscription": sub})
}

// GetSubscription handles GET /api/events.subscriptions.info?id=ES123
func (h *EventHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	sub, err := h.svc.GetSubscription(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"subscription": sub})
}

// UpdateSubscription handles POST /api/events.subscriptions.update
func (h *EventHandler) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string   `json:"id"`
		URL        *string  `json:"url,omitempty"`
		EventTypes []string `json:"event_types,omitempty"`
		Enabled    *bool    `json:"enabled,omitempty"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	sub, err := h.svc.UpdateSubscription(r.Context(), req.ID, domain.UpdateEventSubscriptionParams{
		URL:        req.URL,
		EventTypes: req.EventTypes,
		Enabled:    req.Enabled,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"subscription": sub})
}

// DeleteSubscription handles POST /api/events.subscriptions.delete
func (h *EventHandler) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.DeleteSubscription(r.Context(), req.ID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// ListSubscriptions handles GET /api/events.subscriptions.list?team_id=T123
func (h *EventHandler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	teamID := r.URL.Query().Get("team_id")

	subs, err := h.svc.ListSubscriptions(r.Context(), domain.ListEventSubscriptionsParams{
		TeamID: teamID,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"subscriptions": subs})
}
