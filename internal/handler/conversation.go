package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// ConversationHandler handles HTTP requests for conversation operations.
type ConversationHandler struct {
	svc *service.ConversationService
}

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(svc *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// Create handles POST /conversations
func (h *ConversationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var params domain.CreateConversationParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.Create(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// Info handles GET /conversations/{id}
func (h *ConversationHandler) Info(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.Get(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// Update handles POST /conversations/{id}
func (h *ConversationHandler) Update(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateConversationParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.Update(r.Context(), channelID, params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// Archive handles POST /conversations/{id}/archive
func (h *ConversationHandler) Archive(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Archive(r.Context(), channelID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Unarchive handles POST /conversations/{id}/unarchive
func (h *ConversationHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Unarchive(r.Context(), channelID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// SetTopic handles POST /conversations/{id}/topic
func (h *ConversationHandler) SetTopic(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Topic  string `json:"topic"`
		UserID string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.SetTopic(r.Context(), channelID, domain.SetTopicParams{
		Topic:   req.Topic,
		SetByID: req.UserID,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// SetPurpose handles POST /conversations/{id}/purpose
func (h *ConversationHandler) SetPurpose(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Purpose string `json:"purpose"`
		UserID  string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.SetPurpose(r.Context(), channelID, domain.SetPurposeParams{
		Purpose: req.Purpose,
		SetByID: req.UserID,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// List handles GET /conversations?team_id=T123&types=public_channel,private_channel&...
func (h *ConversationHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	var types []domain.ConversationType
	if t := q.Get("types"); t != "" {
		for _, s := range strings.Split(t, ",") {
			types = append(types, domain.ConversationType(strings.TrimSpace(s)))
		}
	}

	excludeArchived, _ := strconv.ParseBool(q.Get("exclude_archived"))

	params := domain.ListConversationsParams{
		TeamID:          q.Get("team_id"),
		Types:           types,
		ExcludeArchived: excludeArchived,
		Cursor:          q.Get("cursor"),
		Limit:           limit,
	}

	page, err := h.svc.List(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"channels": page.Items,
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}

// Invite handles POST /conversations/{id}/members
func (h *ConversationHandler) Invite(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Users string `json:"users"` // comma-separated user IDs
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	for _, uid := range strings.Split(req.Users, ",") {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if err := h.svc.Invite(r.Context(), channelID, uid); err != nil {
			httputil.WriteError(w, err)
			return
		}
	}

	conv, err := h.svc.Get(r.Context(), channelID)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// Kick handles DELETE /conversations/{id}/members/{user_id}
func (h *ConversationHandler) Kick(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	userID := r.PathValue("user_id")
	if channelID == "" || userID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Kick(r.Context(), channelID, userID); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Members handles GET /conversations/{id}/members?cursor=...&limit=100
func (h *ConversationHandler) Members(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("id")
	if channelID == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := h.svc.ListMembers(r.Context(), channelID, q.Get("cursor"), limit)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	userIDs := make([]string, len(page.Items))
	for i, m := range page.Items {
		userIDs[i] = m.UserID
	}

	resp := map[string]any{
		"members": userIDs,
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}
