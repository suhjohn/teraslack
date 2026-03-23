package handler

import (
	"encoding/json"
	"net/http"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// EventHandler handles HTTP requests for event subscription operations.
type EventHandler struct {
	svc *service.EventService
}

// NewEventHandler creates a new EventHandler.
func NewEventHandler(svc *service.EventService) *EventHandler {
	return &EventHandler{svc: svc}
}

// CreateSubscription handles POST /event-subscriptions.
func (h *EventHandler) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateEventSubscriptionParams
	if err := decodeStrictJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if teamID := ctxutil.GetTeamID(r.Context()); teamID != "" {
		params.TeamID = teamID
	}

	sub, err := h.svc.CreateSubscription(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/event-subscriptions/"+sub.ID, eventSubscriptionResponseFromDomain(sub))
}

// GetSubscription handles GET /event-subscriptions/{id}.
func (h *EventHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sub, err := h.svc.GetSubscription(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, eventSubscriptionResponseFromDomain(sub))
}

// UpdateSubscription handles PATCH /event-subscriptions/{id}.
func (h *EventHandler) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req EventSubscriptionUpdateRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	sub, err := h.svc.UpdateSubscription(r.Context(), id, domain.UpdateEventSubscriptionParams{
		URL:          req.URL,
		Type:         req.Type,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		Enabled:      req.Enabled,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, eventSubscriptionResponseFromDomain(sub))
}

// DeleteSubscription handles DELETE /event-subscriptions/{id}.
func (h *EventHandler) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.DeleteSubscription(r.Context(), id); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// ListSubscriptions handles GET /event-subscriptions.
func (h *EventHandler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	teamID := r.URL.Query().Get("team_id")
	if ctxTeam := ctxutil.GetTeamID(r.Context()); ctxTeam != "" {
		teamID = ctxTeam
	}

	subs, err := h.svc.ListSubscriptions(r.Context(), domain.ListEventSubscriptionsParams{
		TeamID: teamID,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCollection(w, http.StatusOK, eventSubscriptionResponsesFromDomain(subs), "")
}

func decodeStrictJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func eventSubscriptionResponseFromDomain(sub *domain.EventSubscription) EventSubscriptionResponse {
	if sub == nil {
		return EventSubscriptionResponse{}
	}
	return EventSubscriptionResponse{
		ID:           sub.ID,
		TeamID:       sub.TeamID,
		URL:          sub.URL,
		Type:         sub.Type,
		ResourceType: sub.ResourceType,
		ResourceID:   sub.ResourceID,
		Enabled:      sub.Enabled,
		CreatedAt:    sub.CreatedAt,
		UpdatedAt:    sub.UpdatedAt,
	}
}

func eventSubscriptionResponsesFromDomain(subs []domain.EventSubscription) []EventSubscriptionResponse {
	resp := make([]EventSubscriptionResponse, 0, len(subs))
	for i := range subs {
		resp = append(resp, eventSubscriptionResponseFromDomain(&subs[i]))
	}
	return resp
}
