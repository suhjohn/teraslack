package openapicli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/johnsuh/teraslack/server/internal/api"
)

func (c *CLI) runSignIn(ctx context.Context, args []string, baseURL string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		c.printSigninHelp(nil, stdout)
		return 0
	}
	if args[0] == "help" {
		c.printSigninHelp(args[1:], stdout)
		return 0
	}

	switch args[0] {
	case "email":
		return c.runSignInEmail(ctx, args[1:], baseURL, stdout, stderr)
	case "google", "github":
		fmt.Fprintf(stderr, "CLI %s sign-in is not supported by the current backend callback flow; use `teraslack signin email`.\n", args[0])
		return 1
	default:
		fmt.Fprintf(stderr, "unknown signin method %q\n\n", args[0])
		c.printSigninHelp(nil, stderr)
		return 2
	}
}

func (c *CLI) printSigninHelp(args []string, w io.Writer) {
	if len(args) == 0 {
		fmt.Fprintln(w, "Usage:\n  teraslack signin email --email <email> [--code <code>]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Supported sign-in methods:")
		fmt.Fprintln(w, "  email              Send a sign-in code and exchange it for a saved session")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "OAuth browser sign-in is not currently available from the CLI for this backend.")
		return
	}

	switch args[0] {
	case "email":
		fmt.Fprintln(w, "Usage:\n  teraslack signin email --email <email> [--code <code>] [--name <ignored>]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Send a verification code by email, prompt for the code if you do not pass one, then save the resulting session locally.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "The legacy `--name` flag is accepted for compatibility and ignored.")
	default:
		fmt.Fprintln(w, "Usage:\n  teraslack signin email --email <email> [--code <code>]")
	}
}

func (c *CLI) runSignInEmail(ctx context.Context, args []string, baseURL string, stdout io.Writer, stderr io.Writer) int {
	var email string
	var code string
	var ignoredName string

	fs := flag.NewFlagSet("signin email", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&email, "email", "", "Email address to sign in with.")
	fs.StringVar(&code, "code", "", "Verification code from the email. If omitted, the CLI prompts for it.")
	fs.StringVar(&ignoredName, "name", "", "Ignored compatibility flag from the older CLI flow.")
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

	baseURL = canonicalBaseURL(baseURL)
	if err := doJSONRequest(ctx, http.MethodPost, baseURL+"/auth/email/start", api.StartEmailLoginRequest{
		Email: strings.TrimSpace(email),
	}, "", &api.GenericStatusResponse{}); err != nil {
		fmt.Fprintf(stderr, "start email sign-in: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Verification code sent to %s\n", strings.TrimSpace(email))

	code = strings.TrimSpace(code)
	if code == "" {
		prompted, err := promptVerificationCode(stderr)
		if err != nil {
			fmt.Fprintf(stderr, "read verification code: %v\n", err)
			return 1
		}
		code = prompted
	}

	var auth api.AuthResponse
	if err := doJSONRequest(ctx, http.MethodPost, baseURL+"/auth/email/verify", api.VerifyEmailLoginRequest{
		Email: strings.TrimSpace(email),
		Code:  code,
	}, "", &auth); err != nil {
		fmt.Fprintf(stderr, "complete email sign-in: %v\n", err)
		return 1
	}

	if err := writeSessionToFileConfig(baseURL, auth.Session.Token, auth.User.ID); err != nil {
		fmt.Fprintf(stderr, "save session: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Signed in as %s (%s)\n", auth.User.Profile.DisplayName, auth.User.ID)
	return 0
}

func promptVerificationCode(stderr io.Writer) (string, error) {
	fmt.Fprint(stderr, "Verification code: ")
	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(code), nil
}
