package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/mcpoauth"
	"github.com/suhjohn/teraslack/internal/teraslackmcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := teraslackmcp.LoadConfigFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8090"
	}
	if strings.TrimSpace(cfg.MCPBaseURL) == "" {
		cfg.MCPBaseURL = "http://localhost:" + port + "/mcp"
	}
	if strings.TrimSpace(cfg.OAuthIssuer) == "" {
		cfg.OAuthIssuer = cfg.BaseURL
	}
	if strings.TrimSpace(cfg.OAuthSigningKey) == "" {
		return fmt.Errorf("MCP_OAUTH_SIGNING_KEY or ENCRYPTION_KEY is required")
	}

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
			logger.Error("create mcp session", "error", err)
			http.Error(w, `{"error":"failed to create session"}`, http.StatusInternalServerError)
			return
		}

		srv.HTTPHandler().ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeProtectedResourceMetadata(w, cfg)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeProtectedResourceMetadata(w, cfg)
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // allow long-lived SSE streams on /mcp
		IdleTimeout:  60 * time.Second,
	}

	sessionCtx, cancelSessions := context.WithCancel(context.Background())
	defer cancelSessions()
	sessions.startJanitor(sessionCtx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("mcp http server starting", "port", port)
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", "signal", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	cancelSessions()
	sessions.closeAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// sessionManager maintains one MCP Server instance per bearer token.
type sessionManager struct {
	cfg    teraslackmcp.Config
	logger *slog.Logger

	mu       sync.RWMutex
	sessions map[string]*managedSession
}

type managedSession struct {
	srv        *teraslackmcp.Server
	expiresAt  time.Time
	lastAccess time.Time
}

func (m *sessionManager) get(token string, claims *mcpoauth.AccessTokenClaims, scopes []string) (*teraslackmcp.Server, error) {
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessions == nil {
		m.sessions = make(map[string]*managedSession)
	} else {
		m.cleanupLocked(now)
	}

	if entry, ok := m.sessions[token]; ok && entry != nil && entry.srv != nil {
		entry.lastAccess = now
		return entry.srv, nil
	}

	// Create a per-client config with the provided token as the API key.
	clientCfg := m.cfg
	clientCfg.APIKey = token
	clientCfg.WorkspaceID = claims.WorkspaceID
	clientCfg.UserID = claims.UserID
	clientCfg.Permissions = append([]string(nil), claims.Permissions...)
	clientCfg.OAuthScopes = append([]string(nil), scopes...)

	srv, err := teraslackmcp.NewServer(clientCfg, m.logger)
	if err != nil {
		return nil, err
	}

	var expiresAt time.Time
	if claims != nil && claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}
	m.sessions[token] = &managedSession{
		srv:        srv,
		expiresAt:  expiresAt,
		lastAccess: now,
	}
	return srv, nil
}

func (m *sessionManager) startJanitor(ctx context.Context) {
	if m == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanup(time.Now())
			}
		}
	}()
}

func (m *sessionManager) cleanup(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
}

func (m *sessionManager) cleanupLocked(now time.Time) {
	if m.sessions == nil {
		return
	}
	// Access tokens are short lived, and the underlying MCP sessions have their own
	// timeout. Prune old bearer-token keyed servers to avoid unbounded growth.
	const idleTTL = 2 * time.Hour
	const grace = 5 * time.Minute

	for token, entry := range m.sessions {
		if entry == nil || entry.srv == nil {
			delete(m.sessions, token)
			continue
		}

		expired := !entry.expiresAt.IsZero() && now.After(entry.expiresAt.Add(grace))
		idle := now.Sub(entry.lastAccess) > idleTTL
		if (expired || idle) && !entry.srv.HasActiveSessions() {
			_ = entry.srv.Close()
			delete(m.sessions, token)
		}
	}
}

func (m *sessionManager) closeAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for token, entry := range m.sessions {
		if entry != nil && entry.srv != nil {
			_ = entry.srv.Close()
		}
		delete(m.sessions, token)
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func writeProtectedResourceMetadata(w http.ResponseWriter, cfg teraslackmcp.Config) {
	resource, err := mcpoauth.CanonicalURL(cfg.MCPBaseURL)
	if err != nil {
		http.Error(w, "invalid MCP_BASE_URL", http.StatusInternalServerError)
		return
	}
	issuer, err := mcpoauth.CanonicalURL(cfg.OAuthIssuer)
	if err != nil {
		http.Error(w, "invalid OAuth issuer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(domain.MCPOAuthProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{issuer},
		ScopesSupported:        domain.MCPOAuthSupportedScopes,
		BearerMethodsSupported: []string{"header"},
	})
}

func protectedResourceMetadataURL(cfg teraslackmcp.Config) string {
	base, err := mcpoauth.CanonicalURL(cfg.MCPBaseURL)
	if err != nil {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/.well-known/oauth-protected-resource" + parsed.Path
}

func writeOAuthChallenge(w http.ResponseWriter, status int, code, description, scope, resourceMetadata string) {
	var params []string
	if code != "" {
		params = append(params, fmt.Sprintf(`error="%s"`, code))
	}
	if scope != "" {
		params = append(params, fmt.Sprintf(`scope="%s"`, scope))
	}
	if resourceMetadata != "" {
		params = append(params, fmt.Sprintf(`resource_metadata="%s"`, resourceMetadata))
	}
	if description != "" {
		params = append(params, fmt.Sprintf(`error_description="%s"`, description))
	}
	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(params, ", "))
	http.Error(w, description, status)
}

func validateOrigin(r *http.Request, rawBaseURL string) error {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	base, err := url.Parse(rawBaseURL)
	if err != nil {
		return err
	}
	allowed := base.Scheme + "://" + base.Host
	if origin != allowed {
		return fmt.Errorf("origin is not allowed")
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
