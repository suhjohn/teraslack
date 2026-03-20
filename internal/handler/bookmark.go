package handler

import (
	"net/http"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// BookmarkHandler handles HTTP requests for bookmark operations.
type BookmarkHandler struct {
	svc *service.BookmarkService
}

// NewBookmarkHandler creates a new BookmarkHandler.
func NewBookmarkHandler(svc *service.BookmarkService) *BookmarkHandler {
	return &BookmarkHandler{svc: svc}
}

// Create handles POST /api/bookmarks.add
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

// Edit handles POST /api/bookmarks.edit
func (h *BookmarkHandler) Edit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookmarkID string  `json:"bookmark_id"`
		Title      *string `json:"title,omitempty"`
		Link       *string `json:"link,omitempty"`
		Emoji      *string `json:"emoji,omitempty"`
		UpdatedBy  string  `json:"updated_by"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	bookmark, err := h.svc.Update(r.Context(), req.BookmarkID, domain.UpdateBookmarkParams{
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

// Remove handles POST /api/bookmarks.remove
func (h *BookmarkHandler) Remove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookmarkID string `json:"bookmark_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Delete(r.Context(), req.BookmarkID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// List handles GET /api/bookmarks.list?channel_id=C123
func (h *BookmarkHandler) List(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")

	bookmarks, err := h.svc.List(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"bookmarks": bookmarks})
}
