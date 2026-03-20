package handler

import (
	"log/slog"
	"net/http"

	"github.com/suhjohn/workspace/internal/service"
)

// Router sets up all HTTP routes.
func Router(
	logger *slog.Logger,
	authSvc *service.AuthService,
	userH *UserHandler,
	convH *ConversationHandler,
	msgH *MessageHandler,
	ugH *UsergroupHandler,
	pinH *PinHandler,
	bookmarkH *BookmarkHandler,
	fileH *FileHandler,
	eventH *EventHandler,
	authH *AuthHandler,
	searchH *SearchHandler,
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

	// Usergroups
	mux.HandleFunc("POST /api/usergroups.create", ugH.Create)
	mux.HandleFunc("POST /api/usergroups.update", ugH.Update)
	mux.HandleFunc("GET /api/usergroups.list", ugH.List)
	mux.HandleFunc("POST /api/usergroups.enable", ugH.Enable)
	mux.HandleFunc("POST /api/usergroups.disable", ugH.Disable)
	mux.HandleFunc("GET /api/usergroups.users.list", ugH.ListUsers)
	mux.HandleFunc("POST /api/usergroups.users.update", ugH.SetUsers)

	// Pins
	mux.HandleFunc("POST /api/pins.add", pinH.Add)
	mux.HandleFunc("POST /api/pins.remove", pinH.Remove)
	mux.HandleFunc("GET /api/pins.list", pinH.List)

	// Bookmarks
	mux.HandleFunc("POST /api/bookmarks.add", bookmarkH.Create)
	mux.HandleFunc("POST /api/bookmarks.edit", bookmarkH.Edit)
	mux.HandleFunc("POST /api/bookmarks.remove", bookmarkH.Remove)
	mux.HandleFunc("GET /api/bookmarks.list", bookmarkH.List)

	// Files
	mux.HandleFunc("POST /api/files.getUploadURLExternal", fileH.GetUploadURL)
	mux.HandleFunc("POST /api/files.completeUploadExternal", fileH.CompleteUpload)
	mux.HandleFunc("GET /api/files.info", fileH.Info)
	mux.HandleFunc("POST /api/files.delete", fileH.Delete)
	mux.HandleFunc("GET /api/files.list", fileH.List)
	mux.HandleFunc("POST /api/files.remote.add", fileH.AddRemoteFile)
	mux.HandleFunc("POST /api/files.remote.share", fileH.ShareRemoteFile)

	// Events
	mux.HandleFunc("POST /api/events.subscriptions.create", eventH.CreateSubscription)
	mux.HandleFunc("GET /api/events.subscriptions.info", eventH.GetSubscription)
	mux.HandleFunc("POST /api/events.subscriptions.update", eventH.UpdateSubscription)
	mux.HandleFunc("POST /api/events.subscriptions.delete", eventH.DeleteSubscription)
	mux.HandleFunc("GET /api/events.subscriptions.list", eventH.ListSubscriptions)

	// Auth
	mux.HandleFunc("POST /api/auth.createToken", authH.CreateToken)
	mux.HandleFunc("POST /api/auth.test", authH.Test)
	mux.HandleFunc("POST /api/auth.revoke", authH.Revoke)

	// Search
	mux.HandleFunc("GET /api/search.messages", searchH.SearchMessages)
	mux.HandleFunc("GET /api/search.files", searchH.SearchFiles)
	mux.HandleFunc("POST /api/search.semantic", searchH.SemanticSearch)

	// Apply middleware
	var h http.Handler = mux
	h = AuthMiddleware(authSvc)(h)
	h = Logger(logger)(h)
	h = Recovery(logger)(h)

	return h
}
