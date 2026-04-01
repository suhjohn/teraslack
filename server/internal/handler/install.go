package handler

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/service"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type InstallHandler struct {
	authSvc    *service.AuthService
	installSvc *service.InstallService
}

func NewInstallHandler(authSvc *service.AuthService, installSvc *service.InstallService) *InstallHandler {
	return &InstallHandler{authSvc: authSvc, installSvc: installSvc}
}

func (h *InstallHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateInstallSessionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	resp, err := h.installSvc.CreateSession(r.Context(), req)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusCreated, resp)
}

func (h *InstallHandler) ApprovalPage(w http.ResponseWriter, r *http.Request) {
	auth, ok := h.sessionAuth(r)
	if !ok {
		renderLoginPage(w, r.URL.String())
		return
	}

	prompt, err := h.installSvc.BuildApprovalPrompt(r.Context(), auth, r.PathValue("id"))
	if err != nil {
		if err == domain.ErrNotFound {
			renderInstallStatusPage(w, http.StatusNotFound, "Install link not found", "This install request does not exist or has expired.")
			return
		}
		httputil.WriteError(w, r, err)
		return
	}

	switch prompt.Session.Status {
	case domain.InstallSessionStatusApproved, domain.InstallSessionStatusConsumed:
		renderInstallStatusPage(w, http.StatusOK, "Install approved", "You can return to your terminal. Teraslack is ready to finish setup there.")
		return
	case domain.InstallSessionStatusExpired:
		renderInstallStatusPage(w, http.StatusGone, "Install expired", "This install request expired. Re-run the installer from your terminal.")
		return
	case domain.InstallSessionStatusCanceled:
		renderInstallStatusPage(w, http.StatusGone, "Install cancelled", "This install request was cancelled. Re-run the installer from your terminal.")
		return
	}

	renderInstallApprovalPage(w, prompt)
}

func (h *InstallHandler) Approve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}

	auth, ok := h.sessionAuth(r)
	if !ok {
		redirectTo := "/cli/install/" + url.PathEscape(r.PathValue("id"))
		renderLoginPage(w, redirectTo)
		return
	}

	session, err := h.installSvc.ApproveSession(r.Context(), auth, r.PathValue("id"), r.FormValue("workspace_id"))
	if err != nil {
		if err == domain.ErrNotFound {
			renderInstallStatusPage(w, http.StatusNotFound, "Install link not found", "This install request does not exist or has expired.")
			return
		}
		httputil.WriteError(w, r, err)
		return
	}

	switch session.Status {
	case domain.InstallSessionStatusExpired:
		renderInstallStatusPage(w, http.StatusGone, "Install expired", "This install request expired. Re-run the installer from your terminal.")
	case domain.InstallSessionStatusCanceled:
		renderInstallStatusPage(w, http.StatusGone, "Install cancelled", "This install request was cancelled. Re-run the installer from your terminal.")
	default:
		renderInstallStatusPage(w, http.StatusOK, "Install approved", "You can return to your terminal. Teraslack is ready to finish setup there.")
	}
}

func (h *InstallHandler) Poll(w http.ResponseWriter, r *http.Request) {
	var req domain.PollInstallSessionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, r, domain.ErrInvalidArgument)
		return
	}
	resp, err := h.installSvc.PollSession(r.Context(), r.PathValue("id"), req.PollToken)
	if err != nil {
		if err == domain.ErrNotFound {
			httputil.WriteErrorResponse(w, r, http.StatusNotFound, "not_found", "The install session was not found.")
			return
		}
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteResource(w, http.StatusOK, resp)
}

func (h *InstallHandler) sessionAuth(r *http.Request) (*domain.AuthContext, bool) {
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

func renderInstallApprovalPage(w http.ResponseWriter, prompt *domain.InstallApprovalPrompt) {
	const tpl = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Install Teraslack</title></head>
<body style="font-family: sans-serif; max-width: 720px; margin: 3rem auto; line-height: 1.5;">
<h1>Install Teraslack</h1>
<p>Signed in as <strong>{{.User.Name}}</strong>{{if .User.Email}} ({{.User.Email}}){{end}}</p>
<p>This will configure a local Teraslack MCP install for one of your workspaces.</p>
{{if .Session.DeviceName}}<p><strong>Device:</strong> {{.Session.DeviceName}}</p>{{end}}
<form method="post" action="{{.ApprovalURL}}">
<label for="workspace_id"><strong>Workspace</strong></label><br>
<select id="workspace_id" name="workspace_id" style="margin-top: 0.5rem; margin-bottom: 1rem; min-width: 24rem;">
{{range .AvailableWorkspaces}}
<option value="{{.ID}}" {{if eq .ID $.SelectedWorkspaceID}}selected{{end}}>{{.Name}} ({{.ID}})</option>
{{end}}
</select><br>
<button type="submit">Approve install</button>
</form>
</body>
</html>`
	t := template.Must(template.New("install-approval").Parse(tpl))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = t.Execute(w, prompt)
}

func renderInstallStatusPage(w http.ResponseWriter, status int, title, message string) {
	const tpl = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body style="font-family: sans-serif; max-width: 720px; margin: 3rem auto; line-height: 1.5;">
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
</body>
</html>`
	t := template.Must(template.New("install-status").Parse(tpl))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = t.Execute(w, map[string]string{
		"Title":   strings.TrimSpace(title),
		"Message": strings.TrimSpace(message),
	})
}
