package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/mcpoauth"
	"github.com/suhjohn/teraslack/internal/teraslackmcp"
	"golang.org/x/oauth2"
)

type headerInjectTransport struct {
	Base          http.RoundTripper
	Authorization string
	Origin        string
}

func (t *headerInjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	req2 := req.Clone(req.Context())
	if t.Authorization != "" && req2.Header.Get("Authorization") == "" {
		req2.Header.Set("Authorization", t.Authorization)
	}
	if t.Origin != "" && req2.Header.Get("Origin") == "" {
		req2.Header.Set("Origin", t.Origin)
	}
	return base.RoundTrip(req2)
}

func newTestToken(t *testing.T, issuer, mcpAudience, apiAudience, signingKey string) string {
	t.Helper()
	token, _, err := mcpoauth.IssueAccessToken(mcpoauth.TokenConfig{
		Issuer:      issuer,
		MCPAudience: mcpAudience,
		APIAudience: apiAudience,
		SigningKey:  signingKey,
	}, time.Now(), mcpoauth.AccessTokenClaims{
		WorkspaceID:   "T_TEST",
		UserID:        "U_TEST",
		PrincipalType: "human",
		ClientID:      "client-test",
		Scope:         domain.MCPOAuthScopeTools,
		Permissions:   []string{"*"},
	})
	if err != nil {
		t.Fatalf("IssueAccessToken error: %v", err)
	}
	return token
}

func newTestMCPMux(t *testing.T, cfg teraslackmcp.Config, logger *slog.Logger) http.Handler {
	t.Helper()
	sessions := &sessionManager{
		cfg:    cfg,
		logger: logger,
	}
	resourceMetadataURL := protectedResourceMetadataURL(cfg)

	mux := http.NewServeMux()
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validateOrigin(r, cfg.MCPBaseURL); err != nil {
			writeOAuthChallenge(w, http.StatusForbidden, "invalid_request", err.Error(), "", resourceMetadataURL)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			writeOAuthChallenge(w, http.StatusUnauthorized, "", "Authorization is required.", domain.MCPOAuthScopeTools, resourceMetadataURL)
			return
		}

		claims, err := mcpoauth.ValidateAccessToken(mcpoauth.TokenConfig{
			Issuer:      cfg.OAuthIssuer,
			MCPAudience: cfg.MCPBaseURL,
			APIAudience: cfg.BaseURL,
			SigningKey:  cfg.OAuthSigningKey,
		}, token, cfg.MCPBaseURL)
		if err != nil {
			writeOAuthChallenge(w, http.StatusUnauthorized, "invalid_token", "Access token is invalid or expired.", domain.MCPOAuthScopeTools, resourceMetadataURL)
			return
		}
		scopes := mcpoauth.NormalizeScopes(claims.Scope)
		if !contains(scopes, domain.MCPOAuthScopeTools) {
			writeOAuthChallenge(w, http.StatusForbidden, "insufficient_scope", "MCP access requires the mcp:tools scope.", domain.MCPOAuthScopeTools, resourceMetadataURL)
			return
		}

		srv, err := sessions.get(token, claims, scopes)
		if err != nil {
			http.Error(w, `{"error":"failed to create session"}`, http.StatusInternalServerError)
			return
		}
		srv.HTTPHandler().ServeHTTP(w, r)
	}))
	return mux
}

func TestMCPServer_RejectsMissingAuthorization(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := teraslackmcp.Config{
		BaseURL:         "http://api.example",
		MCPBaseURL:      "http://mcp.example/mcp",
		OAuthIssuer:     "http://api.example",
		OAuthSigningKey: "test-signing-key",
	}

	ts := httptest.NewServer(newTestMCPMux(t, cfg, logger))
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`)))
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got == "" {
		t.Fatalf("missing WWW-Authenticate header")
	}
}

func TestMCPServer_StreamableHTTPConnectsWithBearerToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := teraslackmcp.Config{
		BaseURL:         "http://api.example",
		MCPBaseURL:      "http://mcp.example/mcp",
		OAuthIssuer:     "http://api.example",
		OAuthSigningKey: "test-signing-key",
	}

	token := newTestToken(t, cfg.OAuthIssuer, cfg.MCPBaseURL, cfg.BaseURL, cfg.OAuthSigningKey)

	ts := httptest.NewServer(newTestMCPMux(t, cfg, logger))
	t.Cleanup(ts.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "teraslack-mcp-test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp",
		HTTPClient: &http.Client{
			Transport: &headerInjectTransport{
				Authorization: "Bearer " + token,
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatalf("ListTools returned no tools")
	}
}

func TestMCPServer_AllowsTrustedOriginEvenWhenHostDiffers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := teraslackmcp.Config{
		BaseURL:         "http://api.example",
		MCPBaseURL:      "http://mcp.example/mcp",
		OAuthIssuer:     "http://api.example",
		OAuthSigningKey: "test-signing-key",
	}
	token := newTestToken(t, cfg.OAuthIssuer, cfg.MCPBaseURL, cfg.BaseURL, cfg.OAuthSigningKey)

	ts := httptest.NewServer(newTestMCPMux(t, cfg, logger))
	t.Cleanup(ts.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "origin-test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp",
		HTTPClient: &http.Client{
			Transport: &headerInjectTransport{
				Authorization: "Bearer " + token,
				Origin:        "http://mcp.example",
			},
		},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()
}

func TestMCPServer_RejectsInvalidOrigin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := teraslackmcp.Config{
		BaseURL:         "http://api.example",
		MCPBaseURL:      "http://mcp.example/mcp",
		OAuthIssuer:     "http://api.example",
		OAuthSigningKey: "test-signing-key",
	}
	token := newTestToken(t, cfg.OAuthIssuer, cfg.MCPBaseURL, cfg.BaseURL, cfg.OAuthSigningKey)

	ts := httptest.NewServer(newTestMCPMux(t, cfg, logger))
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`)))
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "http://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusForbidden, string(body))
	}
}

// Ensure we don't accidentally regress into requiring interactive OAuth flows in tests.
var _ = oauth2.StaticTokenSource

