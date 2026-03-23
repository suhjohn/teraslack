package handler

import (
	"net/http"
	"strconv"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

// MessageHandler handles HTTP requests for message operations.
type MessageHandler struct {
	svc *service.MessageService
}

// NewMessageHandler creates a new MessageHandler.
func NewMessageHandler(svc *service.MessageService) *MessageHandler {
	return &MessageHandler{svc: svc}
}

// PostMessage handles POST /messages.
func (h *MessageHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	var params domain.PostMessageParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	msg, err := h.svc.PostMessage(r.Context(), params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteCreated(w, "/messages/"+msg.ChannelID+"/"+msg.TS, msg)
}

// UpdateMessage handles PATCH /messages/{conversation_id}/{message_ts}.
func (h *MessageHandler) UpdateMessage(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("conversation_id")
	ts := r.PathValue("message_ts")
	if channelID == "" || ts == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	var params domain.UpdateMessageParams
	if err := httputil.DecodeJSON(r, &params); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	msg, err := h.svc.UpdateMessage(r.Context(), channelID, ts, params)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, msg)
}

// DeleteMessage handles DELETE /messages/{conversation_id}/{message_ts}.
func (h *MessageHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("conversation_id")
	ts := r.PathValue("message_ts")
	if channelID == "" || ts == "" {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.svc.DeleteMessage(r.Context(), channelID, ts); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// History handles GET /messages.
func (h *MessageHandler) History(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	conversationID := q.Get("conversation_id")

	// If thread_ts is provided, return thread replies
	if threadTS := q.Get("thread_ts"); threadTS != "" {
		params := domain.ListRepliesParams{
			ChannelID: conversationID,
			ThreadTS:  threadTS,
			Cursor:    q.Get("cursor"),
			Limit:     limit,
		}

		page, err := h.svc.Replies(r.Context(), params)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}

		nextCursor := ""
		if page.HasMore {
			nextCursor = page.NextCursor
		}
		httputil.WriteCollection(w, http.StatusOK, page.Items, nextCursor)
		return
	}

	inclusive, _ := strconv.ParseBool(q.Get("inclusive"))
	includeAllMetadata, _ := strconv.ParseBool(q.Get("include_all_metadata"))

	params := domain.ListMessagesParams{
		ChannelID:          conversationID,
		Latest:             q.Get("latest"),
		Oldest:             q.Get("oldest"),
		Inclusive:          inclusive,
		IncludeAllMetadata: includeAllMetadata,
		Cursor:             q.Get("cursor"),
		Limit:              limit,
	}

	page, err := h.svc.History(r.Context(), params)
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

// AddReaction handles POST /messages/{conversation_id}/{message_ts}/reactions.
func (h *MessageHandler) AddReaction(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("conversation_id")
	messageTS := r.PathValue("message_ts")
	var req MessageReactionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	if actorID := ctxutil.GetActingUserID(r.Context()); actorID != "" {
		req.UserID = actorID
	}

	err := h.svc.AddReaction(r.Context(), domain.AddReactionParams{
		ChannelID: channelID,
		MessageTS: messageTS,
		UserID:    req.UserID,
		Emoji:     req.Name,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// RemoveReaction handles DELETE /messages/{conversation_id}/{message_ts}/reactions/{reaction_name}.
func (h *MessageHandler) RemoveReaction(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("conversation_id")
	messageTS := r.PathValue("message_ts")
	name := r.PathValue("reaction_name")
	userID := ctxutil.GetActingUserID(r.Context())

	err := h.svc.RemoveReaction(r.Context(), domain.RemoveReactionParams{
		ChannelID: channelID,
		MessageTS: messageTS,
		UserID:    userID,
		Emoji:     name,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteNoContent(w)
}

// GetReactions handles GET /messages/{conversation_id}/{message_ts}/reactions.
func (h *MessageHandler) GetReactions(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("conversation_id")
	timestamp := r.PathValue("message_ts")

	reactions, err := h.svc.GetReactions(r.Context(), channelID, timestamp)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteResource(w, http.StatusOK, reactions)
}
