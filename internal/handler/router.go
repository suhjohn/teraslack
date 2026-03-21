package handler

import (
	"log/slog"
	"net/http"

	"github.com/suhjohn/teraslack/internal/service"
)

// Router sets up all HTTP routes.
func Router(
	logger *slog.Logger,
	authSvc *service.AuthService,
	apiKeySvc *service.APIKeyService,
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
	apiKeyH *APIKeyHandler,
) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	// Users
	mux.HandleFunc("POST /users", userH.Create)
	mux.HandleFunc("GET /users/search", userH.LookupByEmail)
	mux.HandleFunc("GET /users/{id}", userH.Info)
	mux.HandleFunc("POST /users/{id}", userH.Update)
	mux.HandleFunc("GET /users", userH.List)

	// Conversations
	mux.HandleFunc("POST /conversations", convH.Create)
	mux.HandleFunc("GET /conversations/{id}/members", convH.Members)
	mux.HandleFunc("POST /conversations/{id}/members", convH.Invite)
	mux.HandleFunc("DELETE /conversations/{id}/members/{user_id}", convH.Kick)
	mux.HandleFunc("POST /conversations/{id}/archive", convH.Archive)
	mux.HandleFunc("POST /conversations/{id}/unarchive", convH.Unarchive)
	mux.HandleFunc("POST /conversations/{id}/topic", convH.SetTopic)
	mux.HandleFunc("POST /conversations/{id}/purpose", convH.SetPurpose)
	mux.HandleFunc("GET /conversations/{id}", convH.Info)
	mux.HandleFunc("POST /conversations/{id}", convH.Update)
	mux.HandleFunc("GET /conversations", convH.List)

	// Messages
	mux.HandleFunc("POST /messages", msgH.PostMessage)
	mux.HandleFunc("POST /messages/{channel_id}/{ts}", msgH.UpdateMessage)
	mux.HandleFunc("DELETE /messages/{channel_id}/{ts}", msgH.DeleteMessage)
	mux.HandleFunc("GET /messages", msgH.History)

	// Reactions
	mux.HandleFunc("POST /reactions", msgH.AddReaction)
	mux.HandleFunc("DELETE /reactions", msgH.RemoveReaction)
	mux.HandleFunc("GET /reactions", msgH.GetReactions)

	// Usergroups
	mux.HandleFunc("POST /usergroups", ugH.Create)
	mux.HandleFunc("POST /usergroups/{id}/enable", ugH.Enable)
	mux.HandleFunc("POST /usergroups/{id}/disable", ugH.Disable)
	mux.HandleFunc("GET /usergroups/{id}/users", ugH.ListUsers)
	mux.HandleFunc("POST /usergroups/{id}/users", ugH.SetUsers)
	mux.HandleFunc("GET /usergroups/{id}", ugH.Info)
	mux.HandleFunc("POST /usergroups/{id}", ugH.Update)
	mux.HandleFunc("GET /usergroups", ugH.List)

	// Pins
	mux.HandleFunc("POST /pins", pinH.Add)
	mux.HandleFunc("DELETE /pins", pinH.Remove)
	mux.HandleFunc("GET /pins", pinH.List)

	// Bookmarks
	mux.HandleFunc("POST /bookmarks", bookmarkH.Create)
	mux.HandleFunc("POST /bookmarks/{id}", bookmarkH.Edit)
	mux.HandleFunc("DELETE /bookmarks/{id}", bookmarkH.Remove)
	mux.HandleFunc("GET /bookmarks", bookmarkH.List)

	// Files
	mux.HandleFunc("POST /files/upload_url", fileH.GetUploadURL)
	mux.HandleFunc("POST /files/remote", fileH.AddRemoteFile)
	mux.HandleFunc("POST /files/{id}/complete", fileH.CompleteUpload)
	mux.HandleFunc("POST /files/{id}/share", fileH.ShareRemoteFile)
	mux.HandleFunc("GET /files/{id}", fileH.Info)
	mux.HandleFunc("DELETE /files/{id}", fileH.Delete)
	mux.HandleFunc("GET /files", fileH.List)

	// Event subscriptions
	mux.HandleFunc("POST /event_subscriptions", eventH.CreateSubscription)
	mux.HandleFunc("GET /event_subscriptions/{id}", eventH.GetSubscription)
	mux.HandleFunc("POST /event_subscriptions/{id}", eventH.UpdateSubscription)
	mux.HandleFunc("DELETE /event_subscriptions/{id}", eventH.DeleteSubscription)
	mux.HandleFunc("GET /event_subscriptions", eventH.ListSubscriptions)

	// Auth / Tokens
	mux.HandleFunc("POST /tokens", authH.CreateToken)
	mux.HandleFunc("DELETE /tokens", authH.Revoke)
	mux.HandleFunc("GET /auth/test", authH.Test)

	// Search (unified — Turbopuffer-backed)
	mux.HandleFunc("POST /search", searchH.Search)

	// API Keys
	mux.HandleFunc("POST /api_keys", apiKeyH.Create)
	mux.HandleFunc("GET /api_keys", apiKeyH.List)
	mux.HandleFunc("GET /api_keys/{id}", apiKeyH.Get)
	mux.HandleFunc("PATCH /api_keys/{id}", apiKeyH.Update)
	mux.HandleFunc("DELETE /api_keys/{id}", apiKeyH.Delete)
	mux.HandleFunc("POST /api_keys/{id}/rotate", apiKeyH.Rotate)

	// Apply middleware
	var h http.Handler = mux
	h = AuthMiddleware(authSvc, apiKeySvc)(h)
	h = Logger(logger)(h)
	h = Recovery(logger)(h)

	return h
}
