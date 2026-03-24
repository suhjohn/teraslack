package handler

import (
	"net/http"

	"github.com/suhjohn/teraslack/internal/ctxutil"
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

// Create handles POST /conversations/{id}/bookmarks.
func (h *BookmarkHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateBookmarkParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	params.ChannelID = r.PathValue("id")
	if actorID := ctxutil.GetActingUserID(r.Context()); actorID != "" {
		params.CreatedBy = actorID
	}

	bookmark, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/conversations/"+bookmark.ChannelID+"/bookmarks/"+bookmark.ID, bookmark)
}

// Edit handles PATCH /conversations/{conversation_id}/bookmarks/{bookmark_id}.
func (h *BookmarkHandler) Edit(w http.ResponseWriter, r *http.Request) {
	bookmarkID := r.PathValue("bookmark_id")
	if bookmarkID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var req BookmarkUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if actorID := ctxutil.GetActingUserID(r.Context()); actorID != "" {
		req.UpdatedBy = actorID
	}

	bookmark, err := h.svc.Update(r.Context(), bookmarkID, domain.UpdateBookmarkParams{
		Title:     req.Title,
		Link:      req.Link,
		Emoji:     req.Emoji,
		UpdatedBy: req.UpdatedBy,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, bookmark)
}

// Remove handles DELETE /conversations/{conversation_id}/bookmarks/{bookmark_id}.
func (h *BookmarkHandler) Remove(w http.ResponseWriter, r *http.Request) {
	bookmarkID := r.PathValue("bookmark_id")
	if bookmarkID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Delete(r.Context(), bookmarkID); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// List handles GET /conversations/{id}/bookmarks.
func (h *BookmarkHandler) List(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")

	bookmarks, err := h.svc.List(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCollection(w, http.StatusOK, bookmarks, "")
}
