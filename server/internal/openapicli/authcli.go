package openapicli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAuthBaseURL  = "https://api.teraslack.ai"
	cliOAuthTimeout     = 5 * time.Minute
	cliOAuthCallbackURL = "/callback"
)

type signupResult struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

type authSessionResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	Provider    string    `json:"provider"`
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type authMeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
}

type startOAuthResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	Nonce            string `json:"nonce"`
}

type oauthCallbackResult struct {
	Code  string
	State string
	Err   string
}

func (c *CLI) runSignIn(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" {
		c.printSigninHelp(args[1:], stdout)
		return 0
	}

	switch args[0] {
	case "email":
		return c.runSignInEmail(ctx, args[1:], stdout, stderr)
	case "google":
		return c.runSignInOAuth(ctx, "google", args[1:], stdout, stderr)
	case "github":
		return c.runSignInOAuth(ctx, "github", args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown signin method %q\n\n", args[0])
		c.printSigninHelp(nil, stderr)
		return 2
	}
}

func (c *CLI) printSigninHelp(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "Usage:\n  teraslack signin <email|google|github> [command flags]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Sign in commands:")
		fmt.Fprintln(w, "  email              Send an email verification code and sign in with it")
		fmt.Fprintln(w, "  google             Sign in via Google in your browser")
		fmt.Fprintln(w, "  github             Sign in via GitHub in your browser")
		return
	}

	switch args[0] {
	case "email":
		fmt.Fprintln(w, "Usage:\n  teraslack signin email --email <email> [--name <name>] [--code <code>]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Send a verification code by email, prompt for the code if omitted, then save the session token locally.")
	case "google":
		fmt.Fprintln(w, "Usage:\n  teraslack signin google")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Open your browser, complete Google OAuth, then save the session token locally.")
	case "github":
		fmt.Fprintln(w, "Usage:\n  teraslack signin github")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Open your browser, complete GitHub OAuth, then save the session token locally.")
	default:
		fmt.Fprintln(w, "Usage:\n  teraslack signin <email|google|github> [command flags]")
	}
}

func (c *CLI) runSignInEmail(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var email string
	var code string
	var name string

	fs := flag.NewFlagSet("signin email", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&email, "email", "", "Email address to sign in with.")
	fs.StringVar(&name, "name", "", "Name to use when creating a new account on first sign-in.")
	fs.StringVar(&code, "code", "", "Verification code from the email. If omitted, the CLI prompts for it.")
	fs.Usage = func() {
		c.printSigninHelp([]string{"email"}, stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 || strings.TrimSpace(email) == "" {
		fs.Usage()
		return 2
	}

	baseURL, err := currentCLIBaseURL()
	if err != nil {
		fmt.Fprintf(stderr, "resolve base URL: %v\n", err)
		return 1
	}

	signup, err := requestEmailSignup(ctx, baseURL, strings.TrimSpace(email))
	if err != nil {
		fmt.Fprintf(stderr, "start email sign-in: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Verification code sent to %s", signup.Email)
	if !signup.ExpiresAt.IsZero() {
		fmt.Fprintf(stdout, " (expires %s)", signup.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintln(stdout)

	code = strings.TrimSpace(code)
	if code == "" {
		prompted, err := promptVerificationCode(stderr)
		if err != nil {
			fmt.Fprintf(stderr, "read verification code: %v\n", err)
			return 1
		}
		code = prompted
	}

	session, err := requestEmailVerify(ctx, baseURL, strings.TrimSpace(email), code, strings.TrimSpace(name))
	if err != nil {
		fmt.Fprintf(stderr, "complete email sign-in: %v\n", err)
		return 1
	}
	return finalizeSessionSignIn(ctx, baseURL, session, stdout, stderr)
}

func (c *CLI) runSignInOAuth(ctx context.Context, provider string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("signin "+provider, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		c.printSigninHelp([]string{provider}, stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	baseURL, err := currentCLIBaseURL()
	if err != nil {
		fmt.Fprintf(stderr, "resolve base URL: %v\n", err)
		return 1
	}

	listener, callbackURL, callbackCh, serverClose, err := startLocalCallbackServer()
	if err != nil {
		fmt.Fprintf(stderr, "start callback server: %v\n", err)
		return 1
	}
	defer serverClose(context.Background())

	startResp, err := requestOAuthStart(ctx, baseURL, provider, callbackURL)
	if err != nil {
		fmt.Fprintf(stderr, "start %s sign-in: %v\n", provider, err)
		return 1
	}

	if err := openBrowser(startResp.AuthorizationURL); err != nil {
		fmt.Fprintf(stderr, "Open this URL in your browser to continue:\n  %s\n", startResp.AuthorizationURL)
	}
	fmt.Fprintf(stdout, "Waiting for %s sign-in callback on %s (timeout %s)\n", provider, listener.Addr().String(), cliOAuthTimeout)

	waitCtx, cancel := context.WithTimeout(ctx, cliOAuthTimeout)
	defer cancel()

	var callback oauthCallbackResult
	select {
	case callback = <-callbackCh:
	case <-waitCtx.Done():
		fmt.Fprintf(stderr, "timed out waiting for %s sign-in callback\n", provider)
		return 1
	}
	if callback.Err != "" {
		fmt.Fprintf(stderr, "%s sign-in failed: %s\n", provider, callback.Err)
		return 1
	}

	session, err := requestOAuthComplete(waitCtx, baseURL, provider, callback.Code, callback.State, startResp.Nonce)
	if err != nil {
		fmt.Fprintf(stderr, "complete %s sign-in: %v\n", provider, err)
		return 1
	}
	return finalizeSessionSignIn(waitCtx, baseURL, session, stdout, stderr)
}

func promptVerificationCode(stderr io.Writer) (string, error) {
	fmt.Fprint(stderr, "Verification code: ")
	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(code), nil
}

func currentCLIBaseURL() (string, error) {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_BASE_URL")); value != "" {
		return value, nil
	}
	cfg, err := loadFileConfig()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return strings.TrimSpace(cfg.BaseURL), nil
	}
	return defaultAuthBaseURL, nil
}

func requestEmailSignup(ctx context.Context, baseURL, email string) (*signupResult, error) {
	var resp signupResult
	err := doJSONRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/auth/signup", map[string]any{
		"email": email,
	}, "", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func requestEmailVerify(ctx context.Context, baseURL, email, code, name string) (*authSessionResponse, error) {
	body := map[string]any{
		"email": email,
		"code":  code,
	}
	if name != "" {
		body["name"] = name
	}
	var resp authSessionResponse
	err := doJSONRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/auth/verify", body, "", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func requestOAuthStart(ctx context.Context, baseURL, provider, callbackURL string) (*startOAuthResponse, error) {
	var resp startOAuthResponse
	err := doJSONRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/auth/cli/oauth/"+url.PathEscape(provider)+"/start", map[string]any{
		"callback_url": callbackURL,
	}, "", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func requestOAuthComplete(ctx context.Context, baseURL, provider, code, state, nonce string) (*authSessionResponse, error) {
	var resp authSessionResponse
	err := doJSONRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/auth/cli/oauth/"+url.PathEscape(provider)+"/complete", map[string]any{
		"code":  code,
		"state": state,
		"nonce": nonce,
	}, "", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func requestAuthMe(ctx context.Context, baseURL, sessionToken string) (*authMeResponse, error) {
	var resp authMeResponse
	err := doJSONRequest(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/auth/me", nil, sessionToken, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func finalizeSessionSignIn(ctx context.Context, baseURL string, session *authSessionResponse, stdout, stderr io.Writer) int {
	me, err := requestAuthMe(ctx, baseURL, session.Token)
	if err != nil {
		fmt.Fprintf(stderr, "fetch auth context: %v\n", err)
		return 1
	}
	if err := writeSessionToFileConfig(baseURL, session.Token, me); err != nil {
		fmt.Fprintf(stderr, "write config: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Signed in to workspace %s as user %s\n", me.WorkspaceID, me.UserID)
	return 0
}

func doJSONRequest(ctx context.Context, method, targetURL string, body any, bearerToken string, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("%s %s returned %s: %s", method, targetURL, resp.Status, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func startLocalCallbackServer() (net.Listener, string, chan oauthCallbackResult, func(context.Context) error, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", nil, nil, err
	}

	callbackCh := make(chan oauthCallbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc(cliOAuthCallbackURL, func(w http.ResponseWriter, r *http.Request) {
		result := oauthCallbackResult{
			Code:  strings.TrimSpace(r.URL.Query().Get("code")),
			State: strings.TrimSpace(r.URL.Query().Get("state")),
			Err:   strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("error_description"), r.URL.Query().Get("error"))),
		}
		if result.Code == "" || result.State == "" {
			if result.Err == "" {
				result.Err = "missing code or state in OAuth callback"
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "Teraslack sign-in failed. You can return to the terminal.")
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "Teraslack sign-in complete. You can return to the terminal.")
		}
		select {
		case callbackCh <- result:
		default:
		}
	})

	go func() {
		_ = server.Serve(listener)
	}()

	callbackURL := "http://" + listener.Addr().String() + cliOAuthCallbackURL
	return listener, callbackURL, callbackCh, server.Shutdown, nil
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
