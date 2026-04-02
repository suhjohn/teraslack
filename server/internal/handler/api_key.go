package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// APIKeyHandler handles HTTP requests for API key operations.
type APIKeyHandler struct {
	svc *service.APIKeyService
}

// NewAPIKeyHandler creates a new APIKeyHandler.
func NewAPIKeyHandler(svc *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{svc: svc}
}

// Create handles POST /api-keys.
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateAPIKeyParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	// Default workspace_id from auth context if not provided
	if params.WorkspaceID == "" {
		params.WorkspaceID = ctxutil.GetWorkspaceID(r.Context())
	}
	// Always track the authenticated actor who created the key.
	if actorID := service.ActorUserID(r.Context()); actorID != "" {
		params.CreatedBy = actorID
	}

	key, rawKey, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/api-keys/"+key.ID, APIKeySecretResponse{
		APIKey: key.Redacted(),
		Secret: rawKey,
	})
}

// Get handles GET /api-keys/{id}.
func (h *APIKeyHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	key, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, key)
}

// List handles GET /api-keys.
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	params := domain.ListAPIKeysParams{
		WorkspaceID: ctxutil.GetWorkspaceID(r.Context()),
		AccountID:   q.Get("account_id"),
		Cursor:      q.Get("cursor"),
		Limit:       limit,
	}
	if q.Get("include_revoked") == "true" {
		params.IncludeRevoked = true
	}
	if params.WorkspaceID == "" {
		params.WorkspaceID = q.Get("workspace_id")
	}

	page, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	nextCursor := ""
	if page.NextCursor != "" {
		nextCursor = page.NextCursor
	}
	httputil.WriteCollection(w, http.StatusOK, page.Items, nextCursor)
}

// Delete handles DELETE /api-keys/{id}.
func (h *APIKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Revoke(r.Context(), id); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// Rotate handles POST /api-keys/{id}/rotations.
func (h *APIKeyHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var params domain.RotateAPIKeyParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		// Rotation can be called with an empty body (uses default 24h grace period)
		params = domain.RotateAPIKeyParams{}
	}

	newKey, rawKey, err := h.svc.Rotate(r.Context(), id, params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/api-keys/"+newKey.ID, APIKeySecretResponse{
		APIKey: newKey.Redacted(),
		Secret: rawKey,
	})
}

// Update handles PATCH /api-keys/{id}.
func (h *APIKeyHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateAPIKeyParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	key, err := h.svc.Update(r.Context(), id, params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, key)
}
