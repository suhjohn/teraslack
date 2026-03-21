package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// UserHandler handles HTTP requests for user operations.
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Create handles POST /users
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateUserParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	user, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"user": user})
}

// Info handles GET /users/{id}
func (h *UserHandler) Info(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	user, err := h.svc.Get(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"user": user})
}

// LookupByEmail handles GET /users/search?email=...
func (h *UserHandler) LookupByEmail(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	user, err := h.svc.GetByEmail(r.Context(), email)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"user": user})
}

// Update handles POST /users/{id}
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateUserParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	user, err := h.svc.Update(r.Context(), userID, params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"user": user})
}

// List handles GET /users?team_id=T123&cursor=...&limit=100
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	params := domain.ListUsersParams{
		TeamID: q.Get("team_id"),
		Cursor: q.Get("cursor"),
		Limit:  limit,
	}

	page, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"members": page.Items,
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}
