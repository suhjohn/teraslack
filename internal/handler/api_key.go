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

// Create handles POST /api_keys
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateAPIKeyParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	// Default team_id from auth context if not provided
	if params.TeamID == "" {
		params.TeamID = ctxutil.GetTeamID(r.Context())
	}
	// Default principal_id from auth context if not provided
	if params.PrincipalID == "" {
		params.PrincipalID = ctxutil.GetUserID(r.Context())
	}
	// Default created_by from auth context
	if params.CreatedBy == "" {
		params.CreatedBy = ctxutil.GetUserID(r.Context())
	}

	key, rawKey, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":      true,
		"api_key": key.Redacted(),
		"key":     rawKey,
	})
}

// Get handles GET /api_keys/{id}
func (h *APIKeyHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	key, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"api_key": key,
	})
}

// List handles GET /api_keys
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	params := domain.ListAPIKeysParams{
		TeamID:      ctxutil.GetTeamID(r.Context()),
		PrincipalID: q.Get("principal_id"),
		Cursor:      q.Get("cursor"),
		Limit:       limit,
	}
	if q.Get("include_revoked") == "true" {
		params.IncludeRevoked = true
	}
	if params.TeamID == "" {
		params.TeamID = q.Get("team_id")
	}

	page, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"api_keys": page.Items,
		"has_more": page.HasMore,
	}
	if page.NextCursor != "" {
		resp["next_cursor"] = page.NextCursor
	}
	httputil.WriteOK(w, resp)
}

// Delete handles DELETE /api_keys/{id}
func (h *APIKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Revoke(r.Context(), id); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Rotate handles POST /api_keys/{id}/rotate
func (h *APIKeyHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var params domain.RotateAPIKeyParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		// Rotation can be called with an empty body (uses default 24h grace period)
		params = domain.RotateAPIKeyParams{}
	}

	newKey, rawKey, err := h.svc.Rotate(r.Context(), id, params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]any{
		"ok":      true,
		"api_key": newKey.Redacted(),
		"key":     rawKey,
	})
}

// Update handles PATCH /api_keys/{id}
func (h *APIKeyHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateAPIKeyParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	key, err := h.svc.Update(r.Context(), id, params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"api_key": key,
	})
}
