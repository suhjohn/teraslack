package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	openapi "github.com/suhjohn/teraslack/internal/api"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
	"gopkg.in/yaml.v3"
)

// Router sets up all HTTP routes.
func Router(
	logger *slog.Logger,
	frontendURL string,
	authSvc *service.AuthService,
	apiKeySvc *service.APIKeyService,
	workspaceH *WorkspaceHandler,
	workspaceInviteH *WorkspaceInviteHandler,
	userH *UserHandler,
	convH *ConversationHandler,
	msgH *MessageHandler,
	ugH *UsergroupHandler,
	pinH *PinHandler,
	bookmarkH *BookmarkHandler,
	fileH *FileHandler,
	externalEventH *ExternalEventHandler,
	externalAccessH *ExternalPrincipalAccessHandler,
	eventH *EventHandler,
	authH *AuthHandler,
	searchH *SearchHandler,
	apiKeyH *APIKeyHandler,
	conversationReadH *ConversationReadHandler,
) http.Handler {
	spec, err := openapi.GetSwagger()
	if err != nil {
		panic("load embedded openapi spec: " + err.Error())
	}

	apiMux := http.NewServeMux()
	apiServer := newOpenAPIServer(
		workspaceH,
		userH,
		convH,
		msgH,
		ugH,
		pinH,
		bookmarkH,
		fileH,
		externalEventH,
		externalAccessH,
		eventH,
		authH,
		searchH,
		apiKeyH,
		conversationReadH,
	)

	apiHandler := openapi.HandlerWithOptions(apiServer, openapi.StdHTTPServerOptions{
		BaseRouter: apiMux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			message := strings.TrimSpace(err.Error())
			if message == "" {
				message = "The request is invalid."
			}
			httputil.WriteErrorResponse(w, r, http.StatusBadRequest, "invalid_request", message)
		},
	})

	validator := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		DoNotValidateServers: true,
		Options: openapi3filter.Options{
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error {
				return nil
			},
		},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			status := opts.StatusCode
			code := "invalid_request"
			message := strings.TrimSpace(err.Error())
			if message == "" {
				message = "The request is invalid."
			}
			switch status {
			case http.StatusNotFound:
				code = "not_found"
				message = "The requested resource was not found."
			case http.StatusMethodNotAllowed:
				code = "method_not_allowed"
				message = "The request method is not allowed for this resource."
			case http.StatusUnauthorized:
				code = "authentication_required"
				message = "Authentication is required."
			case http.StatusInternalServerError:
				code = "internal_error"
				message = "An unexpected error occurred."
			}
			httputil.WriteErrorResponse(w, r, status, code, message)
		},
	})

	root := http.NewServeMux()
	root.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		doc, err := openapi.GetSwagger()
		if err != nil {
			httputil.WriteInternalError(w, r)
			return
		}
		data, err := doc.MarshalJSON()
		if err != nil {
			httputil.WriteInternalError(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
	root.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		doc, err := openapi.GetSwagger()
		if err != nil {
			httputil.WriteInternalError(w, r)
			return
		}
		data, err := yaml.Marshal(doc)
		if err != nil {
			httputil.WriteInternalError(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
	root.HandleFunc("POST /teams/{id}/invites", func(w http.ResponseWriter, r *http.Request) {
		workspaceInviteH.Create(w, r)
	})
	root.Handle("/", validator(apiHandler))

	var h http.Handler = root
	h = CORS(frontendURL)(h)
	h = AuthMiddleware(authSvc, apiKeySvc)(h)
	h = Logger(logger)(h)
	h = Recovery(logger)(h)
	h = RequestID()(h)

	return h
}
