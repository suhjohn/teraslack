package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// SearchHandler handles HTTP requests for search operations.
type SearchHandler struct {
	svc *service.SearchService
}

// NewSearchHandler creates a new SearchHandler.
func NewSearchHandler(svc *service.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// SearchMessages handles GET /search/messages?team_id=T123&query=hello&limit=20
func (h *SearchHandler) SearchMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	params := domain.SearchMessagesParams{
		TeamID: q.Get("team_id"),
		Query:  q.Get("query"),
		Cursor: q.Get("cursor"),
		Limit:  limit,
	}

	page, err := h.svc.SearchMessages(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"messages": map[string]any{
			"matches": page.Items,
		},
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}

// SearchFiles handles GET /search/files?team_id=T123&query=hello&limit=20
func (h *SearchHandler) SearchFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	params := domain.SearchFilesParams{
		TeamID: q.Get("team_id"),
		Query:  q.Get("query"),
		Cursor: q.Get("cursor"),
		Limit:  limit,
	}

	page, err := h.svc.SearchFiles(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"files": map[string]any{
			"matches": page.Items,
		},
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}

// SemanticSearch handles POST /search/semantic
func (h *SearchHandler) SemanticSearch(w http.ResponseWriter, r *http.Request) {
	var params domain.SemanticSearchParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	results, err := h.svc.SemanticSearch(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"results": results})
}
