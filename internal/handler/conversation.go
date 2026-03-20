package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/suhjohn/slackbackend/internal/domain"
	"github.com/suhjohn/slackbackend/internal/service"
	"github.com/suhjohn/slackbackend/pkg/httputil"
)

// ConversationHandler handles HTTP requests for conversation operations.
type ConversationHandler struct {
	svc *service.ConversationService
}

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(svc *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// Create handles POST /api/conversations.create
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

// Info handles GET /api/conversations.info?channel=C123
func (h *ConversationHandler) Info(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel")
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

// Rename handles POST /api/conversations.rename
func (h *ConversationHandler) Rename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Name    string `json:"name"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.Update(r.Context(), req.Channel, domain.UpdateConversationParams{Name: &req.Name})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// Archive handles POST /api/conversations.archive
func (h *ConversationHandler) Archive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Archive(r.Context(), req.Channel); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Unarchive handles POST /api/conversations.unarchive
func (h *ConversationHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Unarchive(r.Context(), req.Channel); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// SetTopic handles POST /api/conversations.setTopic
func (h *ConversationHandler) SetTopic(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Topic   string `json:"topic"`
		UserID  string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.SetTopic(r.Context(), req.Channel, domain.SetTopicParams{
		Topic:   req.Topic,
		SetByID: req.UserID,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// SetPurpose handles POST /api/conversations.setPurpose
func (h *ConversationHandler) SetPurpose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Purpose string `json:"purpose"`
		UserID  string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	conv, err := h.svc.SetPurpose(r.Context(), req.Channel, domain.SetPurposeParams{
		Purpose: req.Purpose,
		SetByID: req.UserID,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// List handles GET /api/conversations.list?team_id=T123&types=public_channel,private_channel&...
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

// Invite handles POST /api/conversations.invite
func (h *ConversationHandler) Invite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Users   string `json:"users"` // comma-separated user IDs
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
		if err := h.svc.Invite(r.Context(), req.Channel, uid); err != nil {
			httputil.WriteError(w, err)
			return
		}
	}

	conv, err := h.svc.Get(r.Context(), req.Channel)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{"channel": conv})
}

// Kick handles POST /api/conversations.kick
func (h *ConversationHandler) Kick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		User    string `json:"user"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.Kick(r.Context(), req.Channel, req.User); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// Members handles GET /api/conversations.members?channel=C123&cursor=...&limit=100
func (h *ConversationHandler) Members(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := h.svc.ListMembers(r.Context(), q.Get("channel"), q.Get("cursor"), limit)
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
