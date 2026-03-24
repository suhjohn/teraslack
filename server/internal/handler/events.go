package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type ExternalEventHandler struct {
	svc *service.ExternalEventService
}

func NewExternalEventHandler(svc *service.ExternalEventService) *ExternalEventHandler {
	return &ExternalEventHandler{svc: svc}
}

func (h *ExternalEventHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := h.svc.List(r.Context(), domain.ListExternalEventsParams{
		Cursor:       q.Get("after"),
		Limit:        limit,
		Type:         q.Get("type"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, struct {
		Items      []domain.ExternalEvent `json:"items"`
		NextCursor string                 `json:"next_cursor,omitempty"`
		HasMore    bool                   `json:"has_more"`
	}{
		Items:      page.Items,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	})
}
