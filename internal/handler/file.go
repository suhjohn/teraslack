package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// FileHandler handles HTTP requests for file operations.
type FileHandler struct {
	svc *service.FileService
}

// NewFileHandler creates a new FileHandler.
func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
}

// GetUploadURL handles POST /api/files.getUploadURLExternal
func (h *FileHandler) GetUploadURL(w http.ResponseWriter, r *http.Request) {
	var params domain.GetUploadURLParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	resp, err := h.svc.GetUploadURL(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"upload_url": resp.UploadURL,
		"file_id":    resp.FileID,
	})
}

// CompleteUpload handles POST /api/files.completeUploadExternal
func (h *FileHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	var params domain.CompleteUploadParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	file, err := h.svc.CompleteUpload(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"file": file})
}

// Info handles GET /api/files.info?file=F123
func (h *FileHandler) Info(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("file")
	if fileID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	file, err := h.svc.Get(r.Context(), fileID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"file": file})
}

// Delete handles POST /api/files.delete
func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string `json:"file"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Delete(r.Context(), req.File); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// List handles GET /api/files.list?channel=C123&user=U123&cursor=...&limit=100
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := h.svc.List(r.Context(), domain.ListFilesParams{
		ChannelID: q.Get("channel"),
		UserID:    q.Get("user"),
		Cursor:    q.Get("cursor"),
		Limit:     limit,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"files": page.Items,
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}

// AddRemoteFile handles POST /api/files.remote.add
func (h *FileHandler) AddRemoteFile(w http.ResponseWriter, r *http.Request) {
	var params domain.AddRemoteFileParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	file, err := h.svc.AddRemoteFile(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"file": file})
}

// ShareRemoteFile handles POST /api/files.remote.share
func (h *FileHandler) ShareRemoteFile(w http.ResponseWriter, r *http.Request) {
	var params domain.ShareRemoteFileParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.ShareRemoteFile(r.Context(), params); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}
