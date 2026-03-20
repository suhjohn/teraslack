package handler

import (
	"net/http"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// SearchHandler handles HTTP requests for unified search.
type SearchHandler struct {
	svc *service.SearchService
}

// NewSearchHandler creates a new SearchHandler.
func NewSearchHandler(svc *service.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// Search handles POST /search
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	var params domain.SearchParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	results, err := h.svc.Search(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"results":  results,
		"has_more": false,
	})
}
