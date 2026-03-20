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
	mux.HandleFunc("POST /v1/users", userH.Create)
	mux.HandleFunc("GET /v1/users/search", userH.LookupByEmail)
	mux.HandleFunc("GET /v1/users/{id}", userH.Info)
	mux.HandleFunc("POST /v1/users/{id}", userH.Update)
	mux.HandleFunc("GET /v1/users", userH.List)

	// Conversations
	mux.HandleFunc("POST /v1/conversations", convH.Create)
	mux.HandleFunc("GET /v1/conversations/{id}/members", convH.Members)
	mux.HandleFunc("POST /v1/conversations/{id}/members", convH.Invite)
	mux.HandleFunc("DELETE /v1/conversations/{id}/members/{user_id}", convH.Kick)
	mux.HandleFunc("POST /v1/conversations/{id}/archive", convH.Archive)
	mux.HandleFunc("POST /v1/conversations/{id}/unarchive", convH.Unarchive)
	mux.HandleFunc("POST /v1/conversations/{id}/topic", convH.SetTopic)
	mux.HandleFunc("POST /v1/conversations/{id}/purpose", convH.SetPurpose)
	mux.HandleFunc("GET /v1/conversations/{id}", convH.Info)
	mux.HandleFunc("POST /v1/conversations/{id}", convH.Update)
	mux.HandleFunc("GET /v1/conversations", convH.List)

	// Messages
	mux.HandleFunc("POST /v1/messages", msgH.PostMessage)
	mux.HandleFunc("POST /v1/messages/{channel_id}/{ts}", msgH.UpdateMessage)
	mux.HandleFunc("DELETE /v1/messages/{channel_id}/{ts}", msgH.DeleteMessage)
	mux.HandleFunc("GET /v1/messages", msgH.History)

	// Reactions
	mux.HandleFunc("POST /v1/reactions", msgH.AddReaction)
	mux.HandleFunc("DELETE /v1/reactions", msgH.RemoveReaction)
	mux.HandleFunc("GET /v1/reactions", msgH.GetReactions)

	// Usergroups
	mux.HandleFunc("POST /v1/usergroups", ugH.Create)
	mux.HandleFunc("POST /v1/usergroups/{id}/enable", ugH.Enable)
	mux.HandleFunc("POST /v1/usergroups/{id}/disable", ugH.Disable)
	mux.HandleFunc("GET /v1/usergroups/{id}/users", ugH.ListUsers)
	mux.HandleFunc("POST /v1/usergroups/{id}/users", ugH.SetUsers)
	mux.HandleFunc("GET /v1/usergroups/{id}", ugH.Info)
	mux.HandleFunc("POST /v1/usergroups/{id}", ugH.Update)
	mux.HandleFunc("GET /v1/usergroups", ugH.List)

	// Pins
	mux.HandleFunc("POST /v1/pins", pinH.Add)
	mux.HandleFunc("DELETE /v1/pins", pinH.Remove)
	mux.HandleFunc("GET /v1/pins", pinH.List)

	// Bookmarks
	mux.HandleFunc("POST /v1/bookmarks", bookmarkH.Create)
	mux.HandleFunc("POST /v1/bookmarks/{id}", bookmarkH.Edit)
	mux.HandleFunc("DELETE /v1/bookmarks/{id}", bookmarkH.Remove)
	mux.HandleFunc("GET /v1/bookmarks", bookmarkH.List)

	// Files
	mux.HandleFunc("POST /v1/files/upload_url", fileH.GetUploadURL)
	mux.HandleFunc("POST /v1/files/remote", fileH.AddRemoteFile)
	mux.HandleFunc("POST /v1/files/{id}/complete", fileH.CompleteUpload)
	mux.HandleFunc("POST /v1/files/{id}/share", fileH.ShareRemoteFile)
	mux.HandleFunc("GET /v1/files/{id}", fileH.Info)
	mux.HandleFunc("DELETE /v1/files/{id}", fileH.Delete)
	mux.HandleFunc("GET /v1/files", fileH.List)

	// Event subscriptions
	mux.HandleFunc("POST /v1/event_subscriptions", eventH.CreateSubscription)
	mux.HandleFunc("GET /v1/event_subscriptions/{id}", eventH.GetSubscription)
	mux.HandleFunc("POST /v1/event_subscriptions/{id}", eventH.UpdateSubscription)
	mux.HandleFunc("DELETE /v1/event_subscriptions/{id}", eventH.DeleteSubscription)
	mux.HandleFunc("GET /v1/event_subscriptions", eventH.ListSubscriptions)

	// Auth / Tokens
	mux.HandleFunc("POST /v1/tokens", authH.CreateToken)
	mux.HandleFunc("DELETE /v1/tokens", authH.Revoke)
	mux.HandleFunc("GET /v1/auth/test", authH.Test)

	// Search
	mux.HandleFunc("GET /v1/search/messages", searchH.SearchMessages)
	mux.HandleFunc("GET /v1/search/files", searchH.SearchFiles)
	mux.HandleFunc("POST /v1/search/semantic", searchH.SemanticSearch)

	// Apply middleware
	var h http.Handler = mux
	h = AuthMiddleware(authSvc)(h)
	h = Logger(logger)(h)
	h = Recovery(logger)(h)

	return h
}
