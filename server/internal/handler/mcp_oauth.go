package handler

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type MCPOAuthHandler struct {
	authSvc     *service.AuthService
	mcpOAuthSvc *service.MCPOAuthService
	logger      *slog.Logger
}

func NewMCPOAuthHandler(authSvc *service.AuthService, mcpOAuthSvc *service.MCPOAuthService, logger *slog.Logger) *MCPOAuthHandler {
	return &MCPOAuthHandler{authSvc: authSvc, mcpOAuthSvc: mcpOAuthSvc, logger: logger}
}

func (h *MCPOAuthHandler) AuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	metadata, err := h.mcpOAuthSvc.AuthorizationServerMetadata()
	if err != nil {
		h.logInternalError(r, "oauth authorization metadata failed", err)
		http.Error(w, "failed to build metadata", http.StatusInternalServerError)
		return
	}
	writeOAuthJSON(w, http.StatusOK, metadata)
}

func (h *MCPOAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.authorizePrompt(w, r)
	case http.MethodPost:
		h.authorizeSubmit(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *MCPOAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, r, &domain.OAuthProtocolError{
			StatusCode:  http.StatusBadRequest,
			Code:        "invalid_request",
			Description: "Form body is invalid.",
		})
		return
	}

	response, err := h.mcpOAuthSvc.ExchangeToken(r.Context(), domain.MCPOAuthTokenExchangeParams{
		GrantType:    strings.TrimSpace(r.PostFormValue("grant_type")),
		Code:         strings.TrimSpace(r.PostFormValue("code")),
		RedirectURI:  strings.TrimSpace(r.PostFormValue("redirect_uri")),
		ClientID:     strings.TrimSpace(r.PostFormValue("client_id")),
		CodeVerifier: strings.TrimSpace(r.PostFormValue("code_verifier")),
		RefreshToken: strings.TrimSpace(r.PostFormValue("refresh_token")),
		Scope:        strings.TrimSpace(r.PostFormValue("scope")),
		Resource:     strings.TrimSpace(r.PostFormValue("resource")),
	})
	if err != nil {
		if oauthErr, ok := err.(*domain.OAuthProtocolError); ok {
			writeOAuthError(w, r, oauthErr)
			return
		}
		h.logInternalError(r, "oauth token exchange failed", err,
			"grant_type", strings.TrimSpace(r.PostFormValue("grant_type")),
			"client_id", strings.TrimSpace(r.PostFormValue("client_id")),
			"redirect_uri", strings.TrimSpace(r.PostFormValue("redirect_uri")),
			"resource", strings.TrimSpace(r.PostFormValue("resource")),
		)
		writeOAuthError(w, r, &domain.OAuthProtocolError{
			StatusCode:  http.StatusInternalServerError,
			Code:        "server_error",
			Description: "Token exchange failed.",
		})
		return
	}
	writeOAuthJSON(w, http.StatusOK, response)
}

func (h *MCPOAuthHandler) authorizePrompt(w http.ResponseWriter, r *http.Request) {
	req := authorizeRequestFromValues(r.URL.Query())
	auth, ok := h.sessionAuth(r)
	if !ok {
		renderLoginPage(w, r.URL.String())
		return
	}

	prompt, err := h.mcpOAuthSvc.BuildAuthorizePrompt(r.Context(), auth, req)
	if err != nil {
		if oauthErr, ok := err.(*domain.OAuthProtocolError); ok {
			writeOAuthError(w, r, oauthErr)
			return
		}
		h.logInternalError(r, "oauth authorization prompt failed", err,
			"client_id", req.ClientID,
			"redirect_uri", req.RedirectURI,
			"resource", req.Resource,
		)
		writeOAuthError(w, r, &domain.OAuthProtocolError{
			StatusCode:  http.StatusInternalServerError,
			Code:        "server_error",
			Description: "Authorization prompt failed.",
		})
		return
	}

	renderConsentPage(w, prompt)
}

func (h *MCPOAuthHandler) authorizeSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, r, &domain.OAuthProtocolError{
			StatusCode:  http.StatusBadRequest,
			Code:        "invalid_request",
			Description: "Form body is invalid.",
		})
		return
	}
	auth, ok := h.sessionAuth(r)
	if !ok {
		renderLoginPage(w, "/oauth/authorize?"+r.Form.Encode())
		return
	}

	redirectURL, err := h.mcpOAuthSvc.CompleteAuthorize(r.Context(), auth, domain.MCPOAuthApproveRequest{
		MCPOAuthAuthorizeRequest: authorizeRequestFromValues(r.Form),
		Approved:                 strings.TrimSpace(r.FormValue("decision")) == "approve",
	})
	if err != nil {
		if oauthErr, ok := err.(*domain.OAuthProtocolError); ok {
			writeOAuthError(w, r, oauthErr)
			return
		}
		submitReq := authorizeRequestFromValues(r.Form)
		h.logInternalError(r, "oauth authorization submit failed", err,
			"client_id", submitReq.ClientID,
			"redirect_uri", submitReq.RedirectURI,
			"resource", submitReq.Resource,
			"decision", strings.TrimSpace(r.FormValue("decision")),
		)
		writeOAuthError(w, r, &domain.OAuthProtocolError{
			StatusCode:  http.StatusInternalServerError,
			Code:        "server_error",
			Description: "Authorization failed.",
		})
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h *MCPOAuthHandler) logInternalError(r *http.Request, msg string, err error, attrs ...any) {
	if h == nil || h.logger == nil || err == nil {
		return
	}

	baseAttrs := []any{
		"status", http.StatusInternalServerError,
		"code", "server_error",
		"message", msg,
		"error", err,
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", httputil.GetRequestID(r.Context()),
		"stack", httputil.Stacktrace(),
	}
	h.logger.Error(msg, append(baseAttrs, attrs...)...)
}

func (h *MCPOAuthHandler) sessionAuth(r *http.Request) (*domain.AuthContext, bool) {
	token, isAPIKey := authCredentialFromRequest(r)
	if token == "" || isAPIKey {
		return nil, false
	}
	auth, err := h.authSvc.ValidateSession(r.Context(), token)
	if err != nil {
		return nil, false
	}
	return auth, true
}

func authorizeRequestFromValues(values url.Values) domain.MCPOAuthAuthorizeRequest {
	return domain.MCPOAuthAuthorizeRequest{
		ResponseType:        strings.TrimSpace(values.Get("response_type")),
		ClientID:            strings.TrimSpace(values.Get("client_id")),
		RedirectURI:         strings.TrimSpace(values.Get("redirect_uri")),
		Scope:               strings.TrimSpace(values.Get("scope")),
		State:               strings.TrimSpace(values.Get("state")),
		CodeChallenge:       strings.TrimSpace(values.Get("code_challenge")),
		CodeChallengeMethod: strings.TrimSpace(values.Get("code_challenge_method")),
		Resource:            strings.TrimSpace(values.Get("resource")),
	}
}

func writeOAuthJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeOAuthError(w http.ResponseWriter, r *http.Request, err *domain.OAuthProtocolError) {
	status := http.StatusBadRequest
	if err != nil && err.StatusCode > 0 {
		status = err.StatusCode
	}
	body := map[string]string{
		"error": "server_error",
	}
	if err != nil {
		body["error"] = err.Code
		if err.Description != "" {
			body["error_description"] = err.Description
		}
	}
	writeOAuthJSON(w, status, body)
}

func renderLoginPage(w http.ResponseWriter, redirectTo string) {
	tpl := template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Authorize Teraslack MCP</title></head>
<body style="font-family: sans-serif; max-width: 720px; margin: 3rem auto; line-height: 1.5;">
<h1>Sign in to Teraslack</h1>
<p>This client needs you to sign in before you can authorize MCP access.</p>
<p><a href="/auth/oauth/github/start?redirect_to={{.RedirectToQuery}}">Continue with GitHub</a></p>
<p><a href="/auth/oauth/google/start?redirect_to={{.RedirectToQuery}}">Continue with Google</a></p>
</body>
</html>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = tpl.Execute(w, map[string]string{"RedirectToQuery": url.QueryEscape(redirectTo)})
}

func renderConsentPage(w http.ResponseWriter, prompt *domain.MCPOAuthAuthorizePrompt) {
	tpl := template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Authorize Teraslack MCP</title></head>
<body style="font-family: sans-serif; max-width: 720px; margin: 3rem auto; line-height: 1.5;">
<h1>Authorize {{.Client.ClientName}}</h1>
<p>Signed in as <strong>{{.UserName}}</strong>{{if .UserEmail}} ({{.UserEmail}}){{end}}</p>
<p>This client is requesting access to Teraslack MCP at <code>{{.Request.Resource}}</code>.</p>
<p>Approving this request authorizes you as the owner of future MCP sessions from this client. Each new MCP session can start with its own Teraslack agent identity owned by you, and <code>whoami</code> will return the active session identity.</p>
<p>Requested scopes:</p>
<ul>{{range .RequestedScopes}}<li><code>{{.}}</code></li>{{end}}</ul>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="response_type" value="{{.Request.ResponseType}}">
<input type="hidden" name="client_id" value="{{.Request.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.Request.RedirectURI}}">
<input type="hidden" name="scope" value="{{.Request.Scope}}">
<input type="hidden" name="state" value="{{.Request.State}}">
<input type="hidden" name="code_challenge" value="{{.Request.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="{{.Request.CodeChallengeMethod}}">
<input type="hidden" name="resource" value="{{.Request.Resource}}">
<button type="submit" name="decision" value="approve">Allow</button>
<button type="submit" name="decision" value="deny">Deny</button>
</form>
</body>
</html>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = tpl.Execute(w, prompt)
}
