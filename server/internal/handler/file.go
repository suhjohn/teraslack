package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// FileHandler handles HTTP requests for file operations.
type FileHandler struct {
	svc *service.FileService
}

// NewFileHandler creates a new FileHandler.
func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
}

// GetUploadURL handles POST /file-uploads.
func (h *FileHandler) GetUploadURL(w http.ResponseWriter, r *http.Request) {
	var params domain.GetUploadURLParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	resp, err := h.svc.GetUploadURL(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/file-uploads/"+resp.FileID, resp)
}

// CompleteUpload handles POST /file-uploads/{id}/complete.
func (h *FileHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	var params domain.CompleteUploadParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	params.FileID = r.PathValue("id")

	file, err := h.svc.CompleteUpload(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, file)
}

// Info handles GET /files/{id}.
func (h *FileHandler) Info(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	file, err := h.svc.Get(r.Context(), fileID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, file)
}

// Delete handles DELETE /files/{id}.
func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Delete(r.Context(), fileID); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// List handles GET /files.
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := h.svc.List(r.Context(), domain.ListFilesParams{
		ChannelID: q.Get("conversation_id"),
		UserID:    q.Get("user_id"),
		Cursor:    q.Get("cursor"),
		Limit:     limit,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	nextCursor := ""
	if page.HasMore {
		nextCursor = page.NextCursor
	}
	httputil.WriteCollection(w, http.StatusOK, page.Items, nextCursor)
}

// AddRemoteFile handles POST /files.
func (h *FileHandler) AddRemoteFile(w http.ResponseWriter, r *http.Request) {
	var params domain.AddRemoteFileParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	params.UserID = service.ActorUserID(r.Context())

	file, err := h.svc.AddRemoteFile(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/files/"+file.ID, file)
}

// ShareRemoteFile handles POST /files/{id}/shares.
func (h *FileHandler) ShareRemoteFile(w http.ResponseWriter, r *http.Request) {
	var params domain.ShareRemoteFileParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	params.FileID = r.PathValue("id")

	if err := h.svc.ShareRemoteFile(r.Context(), params); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}
