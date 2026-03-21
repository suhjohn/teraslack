package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// BookmarkHandler handles HTTP requests for bookmark operations.
type BookmarkHandler struct {
	svc *service.BookmarkService
}

// NewBookmarkHandler creates a new BookmarkHandler.
func NewBookmarkHandler(svc *service.BookmarkService) *BookmarkHandler {
	return &BookmarkHandler{svc: svc}
}

// Create handles POST /bookmarks
func (h *BookmarkHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateBookmarkParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	bookmark, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"bookmark": bookmark})
}

// Edit handles POST /bookmarks/{id}
func (h *BookmarkHandler) Edit(w http.ResponseWriter, r *http.Request) {
	bookmarkID := r.PathValue("id")
	if bookmarkID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Title     *string `json:"title,omitempty"`
		Link      *string `json:"link,omitempty"`
		Emoji     *string `json:"emoji,omitempty"`
		UpdatedBy string  `json:"updated_by"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	bookmark, err := h.svc.Update(r.Context(), bookmarkID, domain.UpdateBookmarkParams{
		Title:     req.Title,
		Link:      req.Link,
		Emoji:     req.Emoji,
		UpdatedBy: req.UpdatedBy,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"bookmark": bookmark})
}

// Remove handles DELETE /bookmarks/{id}
func (h *BookmarkHandler) Remove(w http.ResponseWriter, r *http.Request) {
	bookmarkID := r.PathValue("id")
	if bookmarkID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Delete(r.Context(), bookmarkID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// List handles GET /bookmarks?channel_id=C123
func (h *BookmarkHandler) List(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")

	bookmarks, err := h.svc.List(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"bookmarks": bookmarks})
}
