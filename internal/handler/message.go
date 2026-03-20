package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/service"
	"github.com/suhjohn/workspace/pkg/httputil"
)

// MessageHandler handles HTTP requests for message operations.
type MessageHandler struct {
	svc *service.MessageService
}

// NewMessageHandler creates a new MessageHandler.
func NewMessageHandler(svc *service.MessageService) *MessageHandler {
	return &MessageHandler{svc: svc}
}

// PostMessage handles POST /messages
func (h *MessageHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	var params domain.PostMessageParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	msg, err := h.svc.PostMessage(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"channel": msg.ChannelID,
		"ts":      msg.TS,
		"message": msg,
	})
}

// UpdateMessage handles POST /messages/{channel_id}/{ts}
func (h *MessageHandler) UpdateMessage(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channel_id")
	ts := r.PathValue("ts")
	if channelID == "" || ts == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateMessageParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	msg, err := h.svc.UpdateMessage(r.Context(), channelID, ts, params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"channel": msg.ChannelID,
		"ts":      msg.TS,
		"message": msg,
	})
}

// DeleteMessage handles DELETE /messages/{channel_id}/{ts}
func (h *MessageHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channel_id")
	ts := r.PathValue("ts")
	if channelID == "" || ts == "" {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.DeleteMessage(r.Context(), channelID, ts); err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"channel": channelID,
		"ts":      ts,
	})
}

// History handles GET /messages?channel=C123&latest=...&oldest=...
// When thread_ts is provided, returns thread replies instead of channel history.
func (h *MessageHandler) History(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	// If thread_ts is provided, return thread replies
	if threadTS := q.Get("thread_ts"); threadTS != "" {
		params := domain.ListRepliesParams{
			ChannelID: q.Get("channel"),
			ThreadTS:  threadTS,
			Cursor:    q.Get("cursor"),
			Limit:     limit,
		}

		page, err := h.svc.Replies(r.Context(), params)
		if err != nil {
			httputil.WriteError(w, err)
			return
		}

		resp := map[string]any{
			"messages": page.Items,
			"has_more": page.HasMore,
		}
		if page.HasMore {
			resp["response_metadata"] = map[string]any{
				"next_cursor": page.NextCursor,
			}
		}
		httputil.WriteOK(w, resp)
		return
	}

	inclusive, _ := strconv.ParseBool(q.Get("inclusive"))
	includeAllMetadata, _ := strconv.ParseBool(q.Get("include_all_metadata"))

	params := domain.ListMessagesParams{
		ChannelID:          q.Get("channel"),
		Latest:             q.Get("latest"),
		Oldest:             q.Get("oldest"),
		Inclusive:          inclusive,
		IncludeAllMetadata: includeAllMetadata,
		Cursor:             q.Get("cursor"),
		Limit:              limit,
	}

	page, err := h.svc.History(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	resp := map[string]any{
		"messages": page.Items,
		"has_more": page.HasMore,
	}
	if page.HasMore {
		resp["response_metadata"] = map[string]any{
			"next_cursor": page.NextCursor,
		}
	}
	httputil.WriteOK(w, resp)
}


// AddReaction handles POST /reactions
func (h *MessageHandler) AddReaction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
		Name      string `json:"name"`
		UserID    string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	err := h.svc.AddReaction(r.Context(), domain.AddReactionParams{
		ChannelID: req.Channel,
		MessageTS: req.Timestamp,
		UserID:    req.UserID,
		Emoji:     req.Name,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// RemoveReaction handles DELETE /reactions
func (h *MessageHandler) RemoveReaction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
		Name      string `json:"name"`
		UserID    string `json:"user_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, domain.ErrInvalidArgument)
		return
	}

	err := h.svc.RemoveReaction(r.Context(), domain.RemoveReactionParams{
		ChannelID: req.Channel,
		MessageTS: req.Timestamp,
		UserID:    req.UserID,
		Emoji:     req.Name,
	})
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, nil)
}

// GetReactions handles GET /reactions?channel=C123&timestamp=...
func (h *MessageHandler) GetReactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	channelID := q.Get("channel")
	timestamp := q.Get("timestamp")

	reactions, err := h.svc.GetReactions(r.Context(), channelID, timestamp)
	if err != nil {
		httputil.WriteError(w, err)
		return
	}

	httputil.WriteOK(w, map[string]any{
		"message": map[string]any{
			"reactions": reactions,
		},
	})
}
