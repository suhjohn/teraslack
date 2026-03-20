package handler

import (
	"log/slog"
	"net/http"
)

// Router sets up all HTTP routes.
func Router(
	logger *slog.Logger,
	userH *UserHandler,
	convH *ConversationHandler,
	msgH *MessageHandler,
) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	// Users
	mux.HandleFunc("POST /api/users.create", userH.Create)
	mux.HandleFunc("GET /api/users.info", userH.Info)
	mux.HandleFunc("GET /api/users.lookupByEmail", userH.LookupByEmail)
	mux.HandleFunc("POST /api/users.profile.set", userH.Update)
	mux.HandleFunc("GET /api/users.list", userH.List)

	// Conversations
	mux.HandleFunc("POST /api/conversations.create", convH.Create)
	mux.HandleFunc("GET /api/conversations.info", convH.Info)
	mux.HandleFunc("POST /api/conversations.rename", convH.Rename)
	mux.HandleFunc("POST /api/conversations.archive", convH.Archive)
	mux.HandleFunc("POST /api/conversations.unarchive", convH.Unarchive)
	mux.HandleFunc("POST /api/conversations.setTopic", convH.SetTopic)
	mux.HandleFunc("POST /api/conversations.setPurpose", convH.SetPurpose)
	mux.HandleFunc("GET /api/conversations.list", convH.List)
	mux.HandleFunc("POST /api/conversations.invite", convH.Invite)
	mux.HandleFunc("POST /api/conversations.kick", convH.Kick)
	mux.HandleFunc("GET /api/conversations.members", convH.Members)

	// Messages
	mux.HandleFunc("POST /api/chat.postMessage", msgH.PostMessage)
	mux.HandleFunc("POST /api/chat.update", msgH.UpdateMessage)
	mux.HandleFunc("POST /api/chat.delete", msgH.DeleteMessage)
	mux.HandleFunc("GET /api/conversations.history", msgH.History)
	mux.HandleFunc("GET /api/conversations.replies", msgH.Replies)

	// Reactions
	mux.HandleFunc("POST /api/reactions.add", msgH.AddReaction)
	mux.HandleFunc("POST /api/reactions.remove", msgH.RemoveReaction)
	mux.HandleFunc("GET /api/reactions.get", msgH.GetReactions)

	// Apply middleware
	var handler http.Handler = mux
	handler = Logger(logger)(handler)
	handler = Recovery(logger)(handler)

	return handler
}
